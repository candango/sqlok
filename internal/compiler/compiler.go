package compiler

import (
	"errors"
	"strings"

	"github.com/candango/sqlok/internal/sst"
)

// Compile compiles a statement node into SQL text and bound arguments.
// The compiler currently implements SELECT rendering; additional statement
// roots can use the same StatementNode boundary as their visitors are added.
func Compile(stmt sst.StatementNode) (string, []any, error) {
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

// VisitSelect renders a SELECT statement and visits its projected columns
// and primary FROM source.
func (c *Compiler) VisitSelect(stmt sst.SelectStatementNode) error {
	c.parts = append(c.parts, "SELECT ")
	for i, column := range stmt.Columns() {
		if i > 0 {
			c.parts = append(c.parts, ", ")
		}
		if err := column.Accept(c); err != nil {
			return err
		}
	}
	if source := stmt.Source(); source != nil {
		c.parts = append(c.parts, " FROM ")
		if err := source.Accept(c); err != nil {
			return err
		}
	}
	return nil
}

// VisitExpression renders the current expression node. Composite binary
// expressions have already traversed their operands before this call.
func (c *Compiler) VisitExpression(expr sst.ExpressionNode) error {
	switch e := expr.(type) {
	case sst.BinaryExpressionNode:
		switch e.Operator() {
		case sst.Equal,
			sst.NotEqual,
			sst.GreaterThan,
			sst.GreaterThanOrEqual,
			sst.LessThan,
			sst.LessThanOrEqual:
			c.parts = append(c.parts, " ", string(e.Operator()), " ")
		default:
			return errors.New("unsupported comparison operator")
		}
	default:
		c.parts = append(c.parts, e.Expr())
	}
	return nil
}

// VisitFromSource renders the base SELECT source reference. Forward JOIN
// traversal will continue from the source's attached join through Right.
func (c *Compiler) VisitFromSource(source sst.FromSourceNode) error {
	if table := source.Table(); table != nil {
		if err := table.Accept(c); err != nil {
			return err
		}
	}

	if join := source.Join(); join != nil {
		if err := join.Accept(c); err != nil {
			return err
		}
	}
	return nil
}

// VisitJoin renders a JOIN relationship. Its Right source is the forward
// traversal edge; Left is a back-reference and must not be traversed here.
func (c *Compiler) VisitJoin(j sst.JoinNode) error {
	c.parts = append(c.parts, " ", string(j.Type()), " ")

	right := j.Right()
	if table := right.Table(); table != nil {
		if err := table.Accept(c); err != nil {
			return err
		}
	}

	if on := j.On(); on != nil {
		c.parts = append(c.parts, " ON ")
		if err := on.Accept(c); err != nil {
			return err
		}
	}

	if next := right.Join(); next != nil {
		return next.Accept(c)
	}
	return nil
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

// VisitTableRef renders a qualified or unqualified SQL table reference.
func (c *Compiler) VisitTableRef(table sst.TableRefNode) error {
	parts := make([]string, 0, 2)
	if table.Schema() != "" {
		parts = append(parts, table.Schema())
	}
	parts = append(parts, table.Name())
	c.parts = append(c.parts, strings.Join(parts, "."))
	return nil
}
