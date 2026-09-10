package jobs

import (
	"context"
	"fmt"
	"sync"
)

// InProcessQueue is a non-durable JobQueue that dispatches jobs to
// registered handlers on goroutines within the current process. Jobs are
// lost on process restart — acceptable for Phase 1 local development;
// a durable, distributed queue is out of scope.
type InProcessQueue struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	wg       sync.WaitGroup
}

// NewInProcessQueue constructs a ready-to-use InProcessQueue.
func NewInProcessQueue() *InProcessQueue {
	return &InProcessQueue{
		handlers: make(map[string]Handler),
	}
}

func (q *InProcessQueue) RegisterHandler(name string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[name] = handler
}

func (q *InProcessQueue) Enqueue(ctx context.Context, job Job) error {
	q.mu.RLock()
	handler, ok := q.handlers[job.Name]
	q.mu.RUnlock()

	if !ok {
		return fmt.Errorf("jobs: no handler registered for job %q", job.Name)
	}

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		_ = handler(ctx, job)
	}()

	return nil
}

// Close blocks until all in-flight jobs have completed.
func (q *InProcessQueue) Close() {
	q.wg.Wait()
}
