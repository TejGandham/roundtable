// Package stdiomcp contains helpers for running roundtable over MCP stdio.
//
// The single most important rule of stdio MCP servers: NOTHING may ever
// write to os.Stdout except the MCP framing layer. A stray fmt.Println, a
// panic stack trace on stdout, or a dep that logs to stdout on import will
// corrupt JSON-RPC frames and wedge the session.
package stdiomcp

import (
	"io"
	"log"
	"log/slog"
	"os"
)

// InitStdioDiscipline redirects the standard `log` package and the default
// slog logger to stderr, and returns a logger the caller can use.
//
// Call this as the VERY FIRST line of main() in any binary that may run in
// stdio mode. It is safe to call in HTTP mode too — stderr logging is fine
// for both.
func InitStdioDiscipline() *slog.Logger {
	// Force the legacy `log` package to stderr. Deps that call log.Printf
	// (there are many) will then not pollute stdout.
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Structured logger to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	return logger
}

// GuardStdout wraps os.Stdout so accidental writes during startup panic
// loudly on stderr. Used by tests, not main(). Returns a restore func and
// any pipe-setup error. The returned goroutine exits when restore is
// called; callers that never call restore will leak it (test helper only).
func GuardStdout() (restore func(), err error) {
	orig := os.Stdout
	r, w, perr := os.Pipe()
	if perr != nil {
		return func() {}, perr
	}
	os.Stdout = w
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				_, _ = os.Stderr.Write([]byte("STDOUT LEAK: "))
				_, _ = os.Stderr.Write(buf[:n])
			}
			if rerr == io.EOF || rerr != nil {
				return
			}
		}
	}()
	return func() {
		_ = w.Close()
		os.Stdout = orig
	}, nil
}
