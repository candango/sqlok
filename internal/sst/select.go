package sst

// SelectStatementNode represents the structural contract of a SELECT
// statement after it has been built. Clause traversal is performed through
// Accept; this interface does not expose fluent construction methods.
type SelectStatementNode interface {
	StatementNode

	// Columns returns the projected expressions in this SELECT statement.
	Columns() *ExpressionList

	// Source returns the primary FROM source.
	Source() FromSourceNode
}

// SelectBuilder represents the fluent construction API for a SELECT
// statement. It embeds SelectStatementNode so a built statement can be passed
// directly to semantic-tree visitors and compilers.
type SelectBuilder interface {
	SelectStatementNode

	// From sets the primary FROM source.
	From(TableRefNode) SelectBuilder

	// Join adds a source with the JOIN type.
	Join(TableRefNode) SelectBuilder

	// InnerJoin adds a source with the INNER JOIN type.
	InnerJoin(TableRefNode) SelectBuilder

	// CrossJoin adds a source with the CROSS JOIN type.
	CrossJoin(TableRefNode) SelectBuilder

	// LeftJoin adds a source with the LEFT JOIN type.
	LeftJoin(TableRefNode) SelectBuilder

	// RightJoin adds a source with the RIGHT JOIN type.
	RightJoin(TableRefNode) SelectBuilder

	// On completes the most recently created JOIN.
	On(Node) SelectBuilder

	// Where adds or combines a WHERE condition.
	Where(ExpressionNode) SelectBuilder
}
