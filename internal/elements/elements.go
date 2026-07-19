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

// ColumnRefOption configures a column reference during construction.
type ColumnRefOption func(*ColumnRef)

// NewColumnRef creates a column reference qualified by table name and applies
// the provided construction options.
func NewColumnRef(tbl string, name string, options ...ColumnRefOption) *ColumnRef {
	c := &ColumnRef{
		name:  name,
		table: tbl,
	}

	for _, option := range options {
		option(c)
	}

	return c
}

// WithColumnSchema qualifies a column reference with a schema name.
func WithColumnSchema(schema string) ColumnRefOption {
	return func(c *ColumnRef) {
		c.schema = schema
	}
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

// TableRef represents a reference to a SQL table, optionally qualified by a
// schema.
type TableRef struct {
	name   string
	schema string
}

// TableRefOption configures a table reference during construction.
type TableRefOption func(*TableRef)

// NewTableRef creates a table reference with the provided table name and
// applies the provided construction options.
func NewTableRef(name string, options ...TableRefOption) *TableRef {
	tr := &TableRef{
		name: name,
	}

	for _, option := range options {
		option(tr)
	}

	return tr
}

// WithTableSchema qualifies a table reference with a schema name.
func WithTableSchema(schema string) TableRefOption {
	return func(tr *TableRef) {
		tr.schema = schema
	}
}

// Accept dispatches the table reference node to the provided visitor.
func (tr *TableRef) Accept(v sst.Visitor) error {
	return v.VisitTableRef(tr)
}

// Name returns the referenced table name.
func (tr *TableRef) Name() string {
	return tr.name
}

// Schema returns the optional schema qualifier.
func (tr *TableRef) Schema() string {
	return tr.schema
}
