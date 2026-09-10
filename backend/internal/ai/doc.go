// Package ai owns domain-level AI Suggestion records (ai_suggestions
// collection: pending/accepted/modified/rejected) and the orchestration
// that turns authorized Project context into platform/ai.AIGateway calls.
// This package composes platform/ai; platform/ai holds no domain
// knowledge. See phase1.md §9, §18, §56, §61. Implementation begins in
// Milestone 8.
package ai
