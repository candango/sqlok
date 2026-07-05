package dql

import "github.com/candango/sqlok/internal/sst"

type SelectColumn struct {
	expr sst.Node
}

func NewSelectColumn(expr sst.Node) *SelectColumn {
	c := &SelectColumn{
		expr: expr,
	}
	return c
}

func (c *SelectColumn) Accept(v sst.Visitor) error {
	return v.VisitSelectColumn(c)
}

func (c *SelectColumn) Expr() sst.Node {
	return c.expr
}
