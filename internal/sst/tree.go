package sst

// Node represents any node in the SQL semantic tree.
type Node interface {
	// Accept dispatches the node to the provided visitor.
	Accept(Visitor) error
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

// SelectNode represents the root node of a SELECT statement.
type SelectNode interface {
	Node

	// Columns returns the projected columns in this SELECT statement.
	Columns() []SelectColumnNode
}

// SelectColumnNode represents one projected item in a SELECT columns clause.
type SelectColumnNode interface {
	Node

	// Expr returns the expression projected by this SELECT column.
	Expr() Node
}

// Visitor defines operations that can be applied to SQL semantic tree nodes.
type Visitor interface {
	// VisitSelect visits a SELECT statement root node.
	VisitSelect(SelectNode) error

	// VisitSelectColumn visits one projected item in a SELECT columns clause.
	VisitSelectColumn(SelectColumnNode) error

	// VisitColumnRef visits a SQL column reference node.
	VisitColumnRef(ColumnRefNode) error
}
