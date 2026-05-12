package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubRegistry counts calls and lets each one optionally block on a gate so
// tests can assert that several requests are genuinely in flight at once.
type stubRegistry struct {
	calls         atomic.Int64
	gate          chan struct{}   // closed by test to release pending calls
	concurrentMax atomic.Int64    // peak observed concurrency
	concurrentNow atomic.Int64    // live in-flight count
	delay         time.Duration   // sleep before returning if no gate
	failNames     map[string]bool // names that should return an error
	err           error
}

func (s *stubRegistry) FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]VersionInfo, error) {
	cur := s.concurrentNow.Add(1)
	for {
		prev := s.concurrentMax.Load()
		if cur <= prev || s.concurrentMax.CompareAndSwap(prev, cur) {
			break
		}
	}
	defer s.concurrentNow.Add(-1)

	s.calls.Add(1)
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.failNames[name] {
		if s.err != nil {
			return nil, s.err
		}
		return nil, errors.New("stub failure")
	}
	return []VersionInfo{{Version: "1.0.0"}}, nil
}

func (s *stubRegistry) FetchMetadata(_ context.Context, name, version string) (*VersionMetadata, error) {
	return &VersionMetadata{Name: name, Version: version}, nil
}

func (s *stubRegistry) FetchDistTags(_ context.Context, _ string) (map[string]string, error) {
	return nil, nil
}

// TestPrefetcher_WarmsCacheBeforeResolverArrives confirms that a name
// submitted to the prefetcher actually triggers a registry call.
func TestPrefetcher_WarmsCache(t *testing.T) {
	reg := &stubRegistry{}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, 4, 16)
	if p == nil {
		t.Fatal("prefetcher unexpectedly nil")
	}

	p.Submit("alpha")
	p.Submit("beta")
	p.Wait()

	if got := reg.calls.Load(); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

// TestPrefetcher_DedupesNames asserts the same name submitted multiple times
// only triggers one registry call. Singleflight in the real registry would
// catch this anyway, but local dedup prevents queue saturation from a single
// noisy name and is cheaper.
func TestPrefetcher_DedupesNames(t *testing.T) {
	reg := &stubRegistry{}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, 4, 16)

	const N = 100
	for i := 0; i < N; i++ {
		p.Submit("alpha")
	}
	p.Wait()

	if got := reg.calls.Load(); got != 1 {
		t.Errorf("expected 1 call after %d submits of same name, got %d", N, got)
	}
}

// TestPrefetcher_ConcurrentExecution proves the worker pool actually runs
// requests in parallel. We gate all responses so every running request must
// be live simultaneously before any returns.
func TestPrefetcher_ConcurrentExecution(t *testing.T) {
	const workers = 8
	gate := make(chan struct{})
	reg := &stubRegistry{gate: gate}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, workers, 64)

	for i := 0; i < workers*2; i++ {
		p.Submit(fmt.Sprintf("pkg-%d", i))
	}

	// Wait until peak concurrency reaches the worker cap. If singleflight
	// or queue logic accidentally serialised the requests, this would never
	// reach `workers`.
	deadline := time.After(5 * time.Second)
	for reg.concurrentMax.Load() < int64(workers) {
		select {
		case <-deadline:
			t.Fatalf("peak concurrency reached %d, want >= %d (workers blocked or serialized)",
				reg.concurrentMax.Load(), workers)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	close(gate)
	p.Wait()

	if got := reg.concurrentMax.Load(); got < int64(workers) {
		t.Errorf("peak concurrency = %d, want >= %d", got, workers)
	}
	if got := reg.concurrentMax.Load(); got > int64(workers) {
		t.Errorf("peak concurrency = %d, exceeded worker cap %d", got, workers)
	}
}

// TestPrefetcher_RespectsContextCancel asserts that cancelling the context
// causes in-flight work to abort and Wait to return promptly.
func TestPrefetcher_RespectsContextCancel(t *testing.T) {
	gate := make(chan struct{}) // never closed
	reg := &stubRegistry{gate: gate}
	ctx, cancel := context.WithCancel(context.Background())
	p := newPrefetcher(ctx, reg, nil, 4, 16)

	for i := 0; i < 20; i++ {
		p.Submit(fmt.Sprintf("pkg-%d", i))
	}

	// Give submissions time to enter the queue and start running.
	time.Sleep(50 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return within 2s of ctx cancel")
	}
}

// TestPrefetcher_ErrorsAreSwallowed asserts that a prefetch error does not
// surface anywhere. The real resolver fetch will surface errors when it
// arrives; prefetch failures must not abort other prefetches or leak.
func TestPrefetcher_ErrorsAreSwallowed(t *testing.T) {
	reg := &stubRegistry{
		failNames: map[string]bool{"bad-1": true, "bad-2": true},
		err:       errors.New("simulated registry failure"),
	}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, 4, 16)

	for _, n := range []string{"good-1", "bad-1", "good-2", "bad-2", "good-3"} {
		p.Submit(n)
	}
	p.Wait()

	if got := reg.calls.Load(); got != 5 {
		t.Errorf("expected 5 attempts (errors must not skip work), got %d", got)
	}
}

// TestPrefetcher_QueueSaturationDoesNotBlockSubmit asserts that filling the
// queue does not block Submit. This is important because Submit is called
// from the serial resolver - blocking the resolver to enqueue prefetches
// would defeat the entire purpose.
func TestPrefetcher_SubmitDoesNotBlockOnFullQueue(t *testing.T) {
	gate := make(chan struct{}) // never closes -> all workers stuck
	reg := &stubRegistry{gate: gate}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, 2, 4)

	// Fill workers + queue.
	for i := 0; i < 6; i++ {
		p.Submit(fmt.Sprintf("pkg-%d", i))
	}

	// This submit would block if Submit didn't drop on full queue.
	done := make(chan struct{})
	go func() {
		p.Submit("overflow")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Submit blocked on full queue (expected drop-on-full)")
	}

	close(gate)
	p.Wait()
}

// TestPrefetcher_NilIsNoOp asserts disabled prefetcher behaves like a sink.
func TestPrefetcher_NilIsNoOp(t *testing.T) {
	var p *prefetcher
	// Must not panic.
	p.Submit("x")
	p.SubmitMany([]string{"a", "b"})
	p.Wait()

	q := newPrefetcher(context.Background(), &stubRegistry{}, nil, 0, 0)
	if q != nil {
		t.Error("newPrefetcher with workers=0 should return nil")
	}
}

// TestPrefetcher_SubmitManyConcurrently exercises the dedup map under
// concurrent submitters. With go test -race this would catch a missing
// mutex on `started`.
func TestPrefetcher_SubmitManyConcurrently(t *testing.T) {
	reg := &stubRegistry{delay: 10 * time.Millisecond}
	ctx := context.Background()
	p := newPrefetcher(ctx, reg, nil, 8, 256)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				p.Submit(fmt.Sprintf("pkg-%d", (off*32+j)%50))
			}
		}(i)
	}
	wg.Wait()
	p.Wait()

	// 50 unique names across 256 submissions.
	if got := reg.calls.Load(); got != 50 {
		t.Errorf("expected exactly 50 unique fetches, got %d", got)
	}
}
