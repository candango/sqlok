package sst

// JoinType identifies the SQL join operation.
type JoinType string

const (
	InnerJoin JoinType = "INNER JOIN"
	CrossJoin JoinType = "CROSS JOIN"
	LeftJoin  JoinType = "LEFT JOIN"
	RightJoin JoinType = "RIGHT JOIN"
)

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
