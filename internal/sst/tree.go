package sst

type Node interface {
	Accept(Visitor) error
}

type SelectNode interface {
	Node
	Columns() []Node
}

type SelectColumnNode interface {
	Node
	Name() string
}

type Visitor interface {
	VisitSelect(SelectNode) error
	VisitColumn(SelectColumnNode) error
}
