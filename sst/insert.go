package sst

// InsertStatementNode represents the structural contract of an INSERT
// statement after it has been built.
type InsertStatementNode interface {
	StatementNode

	// Target returns the table receiving inserted rows.
	Target() TableRefNode

	// Columns returns the target columns in declaration order.
	Columns() *CommaSeparatedList[ColumnRefNode]
}

// InsertBuilder represents the fluent construction API for an INSERT
// statement.
type InsertBuilder interface {
	InsertStatementNode

	// Values appends one or more INSERT rows.
	Values(...[]ExpressionNode) InsertBuilder
}
