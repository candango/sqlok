package elements

import "github.com/candango/sqlok/internal/sst"

// NOTE: Keep this file compact while the elements package is small. Split
// concrete nodes into focused files like column_ref.go, literal.go, or
// binary.go once this starts becoming a grab bag.

// ColumnRef represents a reference to a SQL column, optionally qualified by a
// schema and table.
type ColumnRef struct {
	name   string
	schema string
	table  string
}

// NewColumnRef creates a column reference qualified by table name.
func NewColumnRef(table string, name string) *ColumnRef {
	return &ColumnRef{
		name:  name,
		table: table,
	}
}

// WithSchema qualifies the column reference with a schema name.
func (c *ColumnRef) WithSchema(schema string) *ColumnRef {
	c.schema = schema
	return c
}

// Accept dispatches the column reference node to the provided visitor.
func (c *ColumnRef) Accept(v sst.Visitor) error {
	return v.VisitColumnRef(c)
}

// Name returns the referenced column name.
func (c *ColumnRef) Name() string {
	return c.name
}

// Schema returns the optional schema qualifier.
func (c *ColumnRef) Schema() string {
	return c.schema
}

// Table returns the table qualifier.
func (c *ColumnRef) Table() string {
	return c.table
}
