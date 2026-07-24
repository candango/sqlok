package sst

// SelectNode represents the root node of a SELECT statement.
type SelectNode interface {
	Node

	// Columns returns the projected columns in this SELECT statement.
	Columns() []SelectColumnNode

	// Source returns the primary FROM source.
	Source() FromSourceNode
}

// SelectColumnNode represents one projected item in a SELECT columns clause.
type SelectColumnNode interface {
	Node

	// Expr returns the expression projected by this SELECT column.
	Expr() Node
}
