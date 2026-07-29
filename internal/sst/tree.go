package sst

// Node represents any node in the SQL semantic tree.
type Node interface {
	// Accept dispatches the node to the provided visitor.
	Accept(Visitor) error
}

// StatementNode represents a SQL statement root that can report a
// construction error before compilation or execution.
type StatementNode interface {
	Node

	// Err returns the first construction error recorded by the statement.
	Err() error
}

// ColumnRefNode represents a reference to a SQL column.
type ColumnRefNode interface {
	Node

	// Name returns the referenced column name.
	Name() string

	// Schema returns the optional schema qualifier.
	Schema() string

	// Table returns the table qualifier.
	Table() string
}

// TableRefNode represents a reference to a SQL table or relation source.
type TableRefNode interface {
	Node

	// Name returns the referenced table name.
	Name() string

	// Schema returns the optional schema qualifier.
	Schema() string
}

// Visitor defines operations that can be applied to SQL semantic tree nodes.
type Visitor interface {
	// VisitColumnRef visits a SQL column reference node.
	VisitColumnRef(ColumnRefNode) error

	VisitFromSource(FromSourceNode) error

	VisitJoin(JoinNode) error

	// VisitSelect visits a SELECT statement root node.
	VisitSelect(SelectStatementNode) error

	// VisitTableRef visits a SQL table reference node.
	VisitTableRef(TableRefNode) error

	VisitBinaryExpression(BinaryExpressionNode) error

	VisitLiteral(LiteralNode) error
}
