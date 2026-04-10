package roundtable

import (
	"context"
	"sync"
	"time"
)

const (
	ProbeTimeout = 5 * time.Second
	RunGrace     = 30 * time.Second
)

type Dispatcher struct {
	backends []Backend
}

func NewDispatcher(backends ...Backend) *Dispatcher {
	return &Dispatcher{backends: backends}
}

func (d *Dispatcher) Dispatch(ctx context.Context, req Request, roles map[string]string) *DispatchResult {
	start := time.Now()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	type probeOutcome struct {
		backend Backend
		healthy bool
		err     error
	}

	probeResults := make([]probeOutcome, len(d.backends))
	var probeWg sync.WaitGroup
	for i, b := range d.backends {
		probeWg.Add(1)
		go func(idx int, backend Backend) {
			defer probeWg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
			defer cancel()
			err := backend.Healthy(probeCtx)
			probeResults[idx] = probeOutcome{backend: backend, healthy: err == nil, err: err}
		}(i, b)
	}
	probeWg.Wait()

	var runMu sync.Mutex
	runResults := make(map[string]*Result, len(d.backends))

	for _, po := range probeResults {
		if !po.healthy {
			reason := "unknown"
			if po.err != nil {
				reason = po.err.Error()
			}
			runResults[po.backend.Name()] = ProbeFailedResult(po.backend.Name(), req.Model, reason, nil)
		}
	}

	runDeadline := time.Duration(timeout)*time.Second + RunGrace
	runCtx, runCancel := context.WithTimeout(ctx, runDeadline)
	defer runCancel()

	var runWg sync.WaitGroup
	for _, po := range probeResults {
		if !po.healthy {
			continue
		}
		runWg.Add(1)
		go func(backend Backend) {
			defer runWg.Done()
			result, err := backend.Run(runCtx, req)
			if err != nil && result == nil {
				result = &Result{
					Model:  req.Model,
					Status: "error",
					Stderr: err.Error(),
				}
			}
			runMu.Lock()
			runResults[backend.Name()] = result
			runMu.Unlock()
		}(po.backend)
	}
	runWg.Wait()

	var maxElapsed int64
	for _, r := range runResults {
		if r.ElapsedMs > maxElapsed {
			maxElapsed = r.ElapsedMs
		}
	}

	totalElapsed := time.Since(start).Milliseconds()
	if maxElapsed > totalElapsed {
		totalElapsed = maxElapsed
	}

	meta := Meta{
		TotalElapsedMs:  totalElapsed,
		FilesReferenced: req.Files,
		DynamicFields:   roles,
	}
	if meta.FilesReferenced == nil {
		meta.FilesReferenced = []string{}
	}

	return &DispatchResult{
		Results: runResults,
		Meta:    meta,
	}
}
