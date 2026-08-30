package sst

// DeleteStatementNode represents the structural contract of a DELETE
// statement after it has been built.
type DeleteStatementNode interface {
	StatementNode

	// Target returns the table from which rows are deleted.
	Target() TableRefNode
}

// DeleteBuilder represents the fluent construction API for a DELETE
// statement.
type DeleteBuilder interface {
	DeleteStatementNode

	// Where adds or combines a WHERE condition.
	Where(ExpressionNode) DeleteBuilder
}
