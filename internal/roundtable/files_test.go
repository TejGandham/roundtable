package roundtable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlineFileContents_Empty(t *testing.T) {
	if got := inlineFileContents(nil); got != "" {
		t.Errorf("nil paths: got %q, want empty", got)
	}
	if got := inlineFileContents([]string{}); got != "" {
		t.Errorf("empty paths: got %q, want empty", got)
	}
}

func TestInlineFileContents_SingleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(p, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := inlineFileContents([]string{p})
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected content in output: %q", got)
	}
	if !strings.Contains(got, `<file path="`+p+`">`) {
		t.Errorf("expected opening <file> tag: %q", got)
	}
	if !strings.Contains(got, "</file>") {
		t.Errorf("expected closing </file> tag: %q", got)
	}
}

func TestInlineFileContents_UnreadableFile(t *testing.T) {
	got := inlineFileContents([]string{"/nonexistent/definitely-not-here.txt"})
	if !strings.Contains(got, "error=") {
		t.Errorf("expected error attr: %q", got)
	}
	if !strings.Contains(got, "/nonexistent/definitely-not-here.txt") {
		t.Errorf("expected path in error output: %q", got)
	}
}

func TestInlineFileContents_TruncatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	big := bytes.Repeat([]byte("a"), defaultMaxFileBytes+1024)
	if err := os.WriteFile(p, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got := inlineFileContents([]string{p})
	if !strings.Contains(got, "<truncated />") {
		t.Errorf("expected <truncated /> marker: output len %d", len(got))
	}
	if len(got) > defaultMaxFileBytes+1024 {
		t.Errorf("output exceeds per-file cap plus overhead: %d > %d", len(got), defaultMaxFileBytes+1024)
	}
}

func TestInlineFileContents_SkipsOverTotalBudget(t *testing.T) {
	dir := t.TempDir()
	block := bytes.Repeat([]byte("x"), defaultMaxFileBytes)
	var paths []string
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.bin", i))
		if err := os.WriteFile(p, block, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	got := inlineFileContents(paths)
	if !strings.Contains(got, "<skipped-files>") {
		t.Errorf("expected <skipped-files> block: output len %d", len(got))
	}
	if !strings.Contains(got, paths[4]) {
		t.Errorf("expected last file (%s) to be named in skipped block", paths[4])
	}
	if !strings.Contains(got, `<file path="`+paths[0]+`">`) {
		t.Error("expected first file to still be inlined")
	}
}
