package sst

// JoinType identifies the SQL join operation.
type JoinType string

const (
	InnerJoin JoinType = "INNER JOIN"
	CrossJoin JoinType = "CROSS JOIN"
	LeftJoin  JoinType = "LEFT JOIN"
	RightJoin JoinType = "RIGHT JOIN"
)

// FromSourceNode represents a SELECT FROM source and its optional join link.
//
// Traversal advances from the source table to its Join and then through the
// Join's Right source. The source's join link must not be traversed through
// Join.Left, which is a back-reference used for context and validation.
type FromSourceNode interface {
	Node

	// Attach adds the next join to this source.
	Attach(JoinNode) error

	// Table returns the table source, when present.
	Table() TableRefNode

	// Join returns the next join attached to this source, when present.
	Join() JoinNode
}

// JoinNode represents a join relationship between SELECT source references.
//
// Left is a back-reference to the source that was joined; traversal advances
// through Right instead, preventing a cycle when the left source owns this
// join link.
type JoinNode interface {
	Node

	// Left returns the existing source on the left side of the join.
	// Left is contextual and must not be followed during forward traversal.
	Left() FromSourceNode

	// On returns the expression used by the join condition.
	On() Node

	// SetOn sets the condition while the statement builder completes this join.
	SetOn(condition Node)

	// Right returns the source introduced by the join and the next traversal
	// point in the source chain.
	Right() FromSourceNode

	// Type returns the SQL join type.
	Type() JoinType
}
