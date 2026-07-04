package elements

import "github.com/candango/sqlok/internal/sst"

type Column struct {
	name string
}

func NewColumn(name string) *Column {
	c := &Column{
		name: name,
	}
	return c
}

func (c *Column) Accept(v sst.Visitor) error {
	return v.VisitColumn(c)
}

func (c *Column) Name() string {
	return c.name
}
