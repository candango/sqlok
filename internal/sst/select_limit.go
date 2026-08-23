package sst

// LimitNode represents a SELECT row limit.
type LimitNode interface {
	ClauseNode

	// Value returns the maximum number of rows.
	Value() int
}
