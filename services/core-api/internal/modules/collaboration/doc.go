// Package collaboration owns TutorHub whiteboard control-plane behavior.
//
// Canonical Yjs document state, operation ordering, awareness and undo history
// belong to the separate collaboration data plane. This package must keep the
// PostgreSQL boundary limited to tenant policy, lifecycle, generations,
// immutable snapshot metadata and bounded command idempotency receipts.
package collaboration
