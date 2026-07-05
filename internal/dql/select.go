package dql

import (
	"github.com/candango/sqlok/internal/sst"
)

type Select struct {
	columns []sst.SelectColumnNode
}

func NewSelect(columns ...sst.SelectColumnNode) *Select {
	s := &Select{
		columns: columns,
	}
	return s
}

func (s *Select) Accept(v sst.Visitor) error {
	return v.VisitSelect(s)
}

func (s *Select) Columns() []sst.SelectColumnNode {
	return s.columns
}
