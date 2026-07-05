package sst

type Node interface {
	Accept(Visitor) error
}

type SelectNode interface {
	Node
	Columns() []SelectColumnNode
}

type SelectColumnNode interface {
	Node
	Expr() Node
}

type Visitor interface {
	VisitSelect(SelectNode) error
	VisitSelectColumn(SelectColumnNode) error
}
