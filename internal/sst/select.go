package sst

// SelectStatementNode represents the root node of a SELECT statement.
type SelectStatementNode interface {
	StatementNode

	// Columns returns the projected expressions in this SELECT statement.
	Columns() *ExpressionList

	// Source returns the primary FROM source.
	Source() FromSourceNode
}
