package stdiomcp

import (
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestInitStdioDiscipline_RedirectsLog(t *testing.T) {
	// Capture original slog default so we can restore it.
	origSlog := slog.Default()
	defer slog.SetDefault(origSlog)

	// Capture stderr.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	_ = InitStdioDiscipline()
	log.Printf("hello from log")
	slog.Info("hello from slog")

	_ = w.Close()
	data, _ := io.ReadAll(r)
	got := string(data)

	if !strings.Contains(got, "hello from log") {
		t.Errorf("log.Printf output did not go to stderr: %q", got)
	}
	if !strings.Contains(got, "hello from slog") {
		t.Errorf("slog output did not go to stderr: %q", got)
	}
}
