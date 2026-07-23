package sst

type JoinType string

const (
	InnerJoin JoinType = "INNER JOIN"
	CrossJoin JoinType = "CROSS JOIN"
	LeftJoin  JoinType = "LEFT JOIN"
	RightJoin JoinType = "RIGHT JOIN"
)

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

// FromSourceNode represents a SELECT source reference.
type FromSourceNode interface {
	Node

	// Table returns the table source, when this is a base source reference.
	Table() TableRefNode

	// Join returns the composed join, when this is a joined source reference.
	Join() JoinNode
}

// JoinNode represents a join relationship between source references.
type JoinNode interface {
	Node

	// Left returns the existing source on the left side of the join.
	Left() FromSourceNode

	// On returns the expression used by the join condition.
	On() Node

	// Right returns the source introduced by the join.
	Right() FromSourceNode

	// Type returns the SQL join type.
	Type() JoinType
}

// SelectNode represents the root node of a SELECT statement.
type SelectNode interface {
	Node

	// Columns returns the projected columns in this SELECT statement.
	Columns() []SelectColumnNode

	// Source returns the primary FROM source.
	Source() TableRefNode
}

// SelectColumnNode represents one projected item in a SELECT columns clause.
type SelectColumnNode interface {
	Node

	// Expr returns the expression projected by this SELECT column.
	Expr() Node
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

	// VisitSelect visits a SELECT statement root node.
	VisitSelect(SelectNode) error

	// VisitSelectColumn visits one projected item in a SELECT columns clause.
	VisitSelectColumn(SelectColumnNode) error

	// VisitTableRef visits a SQL table reference node.
	VisitTableRef(TableRefNode) error
}
