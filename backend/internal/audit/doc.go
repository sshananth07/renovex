// Package audit owns Audit Events: an append-only record of critical
// business events (Quotation Sent, Client Viewed Quotation, Client Accepted
// Quotation, etc.) layered on top of current-state domain records, not full
// event sourcing. See phase1.md §50.
//
// Implemented in Milestone 6, its first consumer. The AuditRecorder
// capability is defined in this package (rather than in the consumer) because
// every method takes and returns only primitives — audit.Service satisfies it
// directly with no composition-root adapter.
package audit
