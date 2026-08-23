package sst

// OffsetNode represents a SELECT row offset.
type OffsetNode interface {
	ClauseNode

	// Value returns the number of rows to skip.
	Value() int
}
