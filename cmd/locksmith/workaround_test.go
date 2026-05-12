package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAtomicWriteFile_HappyPath asserts that a normal write produces the
// expected file contents and no leftover .tmp files.
func TestAtomicWriteFile_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "lockfile.json")
	payload := []byte(`{"name":"test"}`)

	if err := atomicWriteFile(target, payload, 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".locksmith-") {
			t.Errorf("leftover temp file in dir: %s", e.Name())
		}
	}
}

// TestAtomicWriteFile_FailedRenameCleansUp ensures a failure leaves no
// orphan temp file behind. We can't easily fail the rename itself, but
// we can fail the write step by passing a directory we don't have write
// permission to.
func TestAtomicWriteFile_FailureOnUnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks; skip")
	}
	tmp := t.TempDir()
	readonly := filepath.Join(tmp, "ro")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}

	err := atomicWriteFile(filepath.Join(readonly, "out.json"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error writing to read-only dir, got nil")
	}

	// Confirm no temp file in the readonly dir (creation should have
	// failed in the first place; this just locks in the behavior).
	entries, _ := os.ReadDir(readonly)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".locksmith-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// TestAtomicWriteFile_OverwritesExisting verifies that an existing target
// file is replaced atomically (the rename clobbers the old file).
func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "lockfile.json")
	if err := os.WriteFile(target, []byte("old payload"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	newPayload := []byte("new payload that completely replaces the old one")
	if err := atomicWriteFile(target, newPayload, 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(got, newPayload) {
		t.Errorf("got %q, want %q", got, newPayload)
	}
}

// TestStartHeartbeat_EmitsLines confirms the heartbeat fires while it's
// running and stops when the stop channel is closed.
func TestStartHeartbeat_EmitsLines(t *testing.T) {
	if testing.Short() {
		t.Skip("heartbeat test relies on real time; -short skip")
	}

	buf := &syncBuffer{}
	ctx := context.Background()
	// Use a short ticker by temporarily overriding the constant via a
	// sleep: we just wait one ticker-interval-plus-buffer and assert.
	// The 5s interval is too slow for a test - we'll patch via a fake.
	stop := startHeartbeatEvery(buf, ctx, time.Now(), 50*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	close(stop)
	time.Sleep(20 * time.Millisecond) // let the goroutine fully exit

	out := buf.String()
	if !strings.Contains(out, "still working") {
		t.Errorf("heartbeat did not write a 'still working' line; got %q", out)
	}
	// At least 2 ticks should have fired in 150ms with a 50ms interval.
	if got := strings.Count(out, "still working"); got < 2 {
		t.Errorf("expected >= 2 heartbeat lines, got %d", got)
	}
}

// TestStartHeartbeat_StopsOnContextCancel ensures cancelling the context
// terminates the goroutine even without close(stop).
func TestStartHeartbeat_StopsOnContextCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time test; -short skip")
	}

	buf := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	_ = startHeartbeatEvery(buf, ctx, time.Now(), 30*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	cancel()
	// Give it a moment, snapshot, then ensure no new lines after.
	time.Sleep(50 * time.Millisecond)
	before := buf.String()
	time.Sleep(120 * time.Millisecond)
	if after := buf.String(); after != before {
		t.Errorf("heartbeat kept writing after context cancel:\n before=%q\n after=%q", before, after)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer for capturing heartbeat output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Ensure unused imports stay used if a test gets removed.
var _ = errors.New
