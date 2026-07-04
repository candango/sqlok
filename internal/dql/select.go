package dql

import (
	"github.com/candango/sqlok/internal/sst"
)

type Select struct {
	columns []sst.Node
}

func NewSelect(columns ...sst.Node) *Select {
	s := &Select{
		columns: columns,
	}
	return s
}

func (s *Select) Accept(v sst.Visitor) error {
	return v.VisitSelect(s)
}

func (s *Select) Columns() []sst.Node {
	return s.columns
}
