package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/jobs"
)

func TestInProcessQueueDispatchesToRegisteredHandler(t *testing.T) {
	q := jobs.NewInProcessQueue()
	defer q.Close()

	var mu sync.Mutex
	var received jobs.Job
	done := make(chan struct{})

	q.RegisterHandler("send_quotation_email", func(_ context.Context, job jobs.Job) error {
		mu.Lock()
		received = job
		mu.Unlock()
		close(done)
		return nil
	})

	err := q.Enqueue(context.Background(), jobs.Job{
		Name:    "send_quotation_email",
		Payload: map[string]any{"quotationId": "QT-0001"},
	})
	if err != nil {
		t.Fatalf("unexpected error enqueueing: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to run")
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Payload["quotationId"] != "QT-0001" {
		t.Fatalf("expected payload to be delivered to handler, got %+v", received)
	}
}

func TestInProcessQueueEnqueueWithoutHandlerFails(t *testing.T) {
	q := jobs.NewInProcessQueue()
	defer q.Close()

	err := q.Enqueue(context.Background(), jobs.Job{Name: "unregistered_job"})
	if err == nil {
		t.Fatal("expected error enqueueing a job with no registered handler, got nil")
	}
}
