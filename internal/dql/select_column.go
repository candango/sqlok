package dql

import "github.com/candango/sqlok/internal/sst"

// SelectColumn represents one projected item in a SELECT columns clause.
type SelectColumn struct {
	expr sst.Node
}

// NewSelectColumn creates a projected SELECT column from an expression node.
func NewSelectColumn(expr sst.Node) *SelectColumn {
	c := &SelectColumn{
		expr: expr,
	}
	return c
}

// Accept dispatches the SELECT column node to the provided visitor.
func (c *SelectColumn) Accept(v sst.Visitor) error {
	return v.VisitSelectColumn(c)
}

// Expr returns the expression projected by this SELECT column.
func (c *SelectColumn) Expr() sst.Node {
	return c.expr
}
