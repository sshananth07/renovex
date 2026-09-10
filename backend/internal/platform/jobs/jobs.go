// Package jobs defines the JobQueue abstraction used for asynchronous
// background work (e.g. PDF generation, AI processing requests). Phase 1
// uses a non-durable in-process implementation; a future SQS-backed queue
// can satisfy the same interface without changes to calling domain modules.
package jobs

import "context"

// Job is a unit of asynchronous work.
type Job struct {
	Name    string
	Payload map[string]any
}

// Handler processes a Job.
type Handler func(ctx context.Context, job Job) error

// JobQueue enqueues Jobs for asynchronous processing.
type JobQueue interface {
	// Enqueue submits job for processing. Enqueue does not guarantee
	// delivery across process restarts (non-durable in the Phase 1
	// in-process implementation).
	Enqueue(ctx context.Context, job Job) error

	// RegisterHandler associates a Handler with jobs of the given name.
	RegisterHandler(name string, handler Handler)
}
