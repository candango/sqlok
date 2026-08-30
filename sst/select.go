package sst

// SelectStatementNode represents the structural contract of a SELECT
// statement after it has been built. Clause traversal is performed through
// Accept; this interface does not expose fluent construction methods.
type SelectStatementNode interface {
	StatementNode

	// Columns returns the projected expressions in this SELECT statement.
	Columns() *CommaSeparatedList[ExpressionNode]

	// Source returns the primary FROM source.
	Source() FromSourceNode
}
