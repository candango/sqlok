package compiler

import (
	"strings"

	"github.com/candango/sqlok/internal/sst"
)

// Compile compiles a SELECT statement node into SQL text and bound arguments.
// TODO: Introduce sst.StatementNode once more statement roots exist, then make
// Compile receive that broader statement contract instead of sst.SelectNode.
func Compile(stmt sst.SelectNode) (string, []any, error) {
	c := &Compiler{}
	if err := stmt.Accept(c); err != nil {
		return "", nil, err
	}
	return strings.Join(c.parts, ""), c.args, nil
}

// Compiler walks SQL semantic tree nodes and renders SQL text.
type Compiler struct {
	parts []string
	args  []any
}

// VisitSelect renders a SELECT statement and visits its projected columns.
func (c *Compiler) VisitSelect(stmt sst.SelectNode) error {
	c.parts = append(c.parts, "SELECT ")
	for i, column := range stmt.Columns() {
		if i > 0 {
			c.parts = append(c.parts, ", ")
		}
		if err := column.Accept(c); err != nil {
			return err
		}
	}
	return nil
}

// VisitSelectColumn renders the expression projected by a SELECT column.
func (c *Compiler) VisitSelectColumn(column sst.SelectColumnNode) error {
	return column.Expr().Accept(c)
}

// VisitColumnRef renders a qualified or unqualified SQL column reference.
func (c *Compiler) VisitColumnRef(column sst.ColumnRefNode) error {
	parts := make([]string, 0, 3)
	if column.Schema() != "" {
		parts = append(parts, column.Schema())
	}
	if column.Table() != "" {
		parts = append(parts, column.Table())
	}
	parts = append(parts, column.Name())
	c.parts = append(c.parts, strings.Join(parts, "."))
	return nil
}
