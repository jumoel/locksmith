package ecosystem

import (
	"context"
	"sync"
	"time"
)

// prefetcher warms a Registry's packument cache by speculatively fetching
// names the resolver is about to need. The actual dependency walk stays
// strictly serial - this only changes when the I/O happens, not what gets
// added to the graph or in what order.
//
// Submissions are best-effort: errors are swallowed because the actual
// resolver call will surface them, and a saturated queue drops new names
// rather than backpressure the resolver. Singleflight in the registry
// client ensures a prefetch in flight when the resolver arrives at the
// same name shares a single HTTP request.
type prefetcher struct {
	ctx      context.Context
	registry Registry
	cutoff   *time.Time
	ch       chan string
	wg       sync.WaitGroup

	mu      sync.Mutex
	started map[string]bool // names ever submitted; per-prefetcher dedup
}

// newPrefetcher constructs a prefetcher with `workers` concurrent slots
// and a buffered submission queue of `queueDepth`. Pass workers <= 0 to
// disable; the returned *prefetcher is nil and all methods become no-ops.
func newPrefetcher(ctx context.Context, registry Registry, cutoff *time.Time, workers, queueDepth int) *prefetcher {
	if workers <= 0 {
		return nil
	}
	if queueDepth <= 0 {
		queueDepth = workers * 32
	}
	p := &prefetcher{
		ctx:      ctx,
		registry: registry,
		cutoff:   cutoff,
		ch:       make(chan string, queueDepth),
		started:  make(map[string]bool),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.run()
	}
	return p
}

func (p *prefetcher) run() {
	defer p.wg.Done()
	for name := range p.ch {
		// Honor context cancellation between items but don't abort an
		// already-issued request; let the singleflight in-flight call
		// finish so other waiters aren't left dangling.
		if p.ctx.Err() != nil {
			// Drain the rest of the queue without doing work.
			continue
		}
		// We don't care about errors - the resolver will issue the same
		// fetch and surface them properly. We just want the cache warm.
		_, _ = p.registry.FetchVersions(p.ctx, name, p.cutoff)
	}
}

// Submit warms the packument cache for `name`. Safe to call from any
// goroutine. No-op when the prefetcher is nil, name is empty, or `name`
// has already been submitted. When the queue is full the call returns
// immediately without queueing - the resolver will fetch synchronously
// when it reaches this name.
func (p *prefetcher) Submit(name string) {
	if p == nil || name == "" {
		return
	}
	p.mu.Lock()
	if p.started[name] {
		p.mu.Unlock()
		return
	}
	p.started[name] = true
	p.mu.Unlock()

	select {
	case p.ch <- name:
	case <-p.ctx.Done():
	default:
		// Queue saturated. The resolver will fetch this name itself when
		// it arrives; we just won't have prewarmed it.
	}
}

// SubmitMany prefetches a batch of names.
func (p *prefetcher) SubmitMany(names []string) {
	if p == nil {
		return
	}
	for _, n := range names {
		p.Submit(n)
	}
}

// Wait stops accepting new submissions and waits for in-flight prefetches
// to drain. The caller must guarantee no further calls to Submit happen
// after Wait is invoked.
func (p *prefetcher) Wait() {
	if p == nil {
		return
	}
	close(p.ch)
	p.wg.Wait()
}
