package sst

// AssignmentNode represents one UPDATE column assignment.
type AssignmentNode interface {
	Node

	// Column returns the column being assigned.
	Column() ColumnRefNode

	// Value returns the expression assigned to the column.
	Value() ExpressionNode
}

// UpdateStatementNode represents the structural contract of an UPDATE
// statement after it has been built.
type UpdateStatementNode interface {
	StatementNode

	// Target returns the table being updated.
	Target() TableRefNode

	// Assignments returns SET assignments in declaration order.
	Assignments() *List[AssignmentNode]
}

// UpdateBuilder represents the fluent construction API for an UPDATE
// statement.
type UpdateBuilder interface {
	UpdateStatementNode

	// Set appends an assignment to the UPDATE statement.
	Set(ColumnRefNode, ExpressionNode) UpdateBuilder

	// Where adds or combines a WHERE condition.
	Where(ExpressionNode) UpdateBuilder
}
