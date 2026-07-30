package sst

// Node represents any node in the SQL semantic tree.
type Node interface {
	// Accept dispatches the node to the provided visitor.
	Accept(Visitor) error
}

// DeclarationNode represents a node that declares a SQL keyword or clause.
type DeclarationNode interface {
	Node

	// Declaration returns the SQL declaration represented by the node.
	Declaration() string
}

// ClauseNode represents a SQL clause with a declaration.
type ClauseNode interface {
	DeclarationNode
}

// StatementNode represents a SQL statement root that can report a
// construction error before compilation or execution.
type StatementNode interface {
	DeclarationNode

	// Err returns the first construction error recorded by the statement.
	Err() error
}

// ListNode represents an ordered list of semantic-tree nodes.
type ListNode[T Node] interface {
	Node

	// Items returns the list elements in traversal order.
	Items() []T
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

	// VisitClause visits a SQL clause declaration.
	VisitClause(ClauseNode) error

	// VisitExpression visits an expression node for SQL rendering.
	VisitExpression(ExpressionNode) error

	// VisitFromSource visits a SELECT source and its attached joins.
	VisitFromSource(FromSourceNode) error

	// VisitJoin visits a join relationship between SELECT sources.
	VisitJoin(JoinNode) error

	// VisitListSeparator visits the separator position before a list item.
	VisitListSeparator(index int) error

	// VisitStatement visits a SQL statement declaration.
	VisitStatement(StatementNode) error

	// VisitTableRef visits a SQL table reference node.
	VisitTableRef(TableRefNode) error
}
