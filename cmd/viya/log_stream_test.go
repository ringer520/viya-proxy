package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The SSE stream uses each line's own end offset as its event id. A client that
// drops mid-batch must resume exactly where it stopped instead of skipping the
// rest of the batch, so the offsets have to be per line rather than per batch.
func TestReadLogLinesReportsPerLineOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	content := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	lines, next, err := readLogLines(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines=%d want 3", len(lines))
	}
	wantEnd := []int64{6, 13, 19}
	for i, want := range wantEnd {
		if lines[i].end != want {
			t.Fatalf("line %d end=%d want %d", i, lines[i].end, want)
		}
	}
	if next != int64(len(content)) {
		t.Fatalf("next=%d want %d", next, len(content))
	}

	// Resuming from the first line's offset must replay the two lines that a
	// disconnected client never received.
	rest, _, err := readLogLines(path, lines[0].end)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 2 || string(rest[0].data) != "second\n" || string(rest[1].data) != "third\n" {
		t.Fatalf("resume returned %d lines", len(rest))
	}
}

// A cursor past EOF (truncated or rotated file) restarts at zero and the
// offsets must be absolute from there, otherwise the client would re-request a
// position that no longer exists.
func TestReadLogLinesRestartsAfterTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(path, []byte("only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lines, next, err := readLogLines(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].end != 5 || next != 5 {
		t.Fatalf("lines=%d end=%d next=%d", len(lines), lines[0].end, next)
	}
}
