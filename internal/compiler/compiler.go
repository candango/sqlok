package compiler

import (
	"strconv"
	"strings"

	"github.com/candango/sqlok/internal/sst"
)

// Compile compiles a statement node into SQL text and bound arguments.
func Compile(stmt sst.StatementNode) (string, []any, error) {
	if err := stmt.Err(); err != nil {
		return "", nil, err
	}

	c := &Compiler{}
	if err := stmt.Accept(c); err != nil {
		return "", nil, err
	}
	return strings.Join(c.parts, ""), c.args, nil
}

// Compiler walks SQL semantic tree nodes and renders SQL.
type Compiler struct {
	parts []string
	args  []any
}

var _ sst.Visitor = (*Compiler)(nil)

// VisitStatement renders a statement declaration.
func (c *Compiler) VisitStatement(stmt sst.StatementNode) error {
	c.parts = append(c.parts, stmt.Declaration(), " ")
	return nil
}

// VisitClause renders a clause declaration.
func (c *Compiler) VisitClause(clause sst.ClauseNode) error {
	c.parts = append(c.parts, " ", clause.Declaration(), " ")
	return nil
}

// VisitExpression renders the current expression node. Composite binary
// expressions have already traversed their operands before this call.
func (c *Compiler) VisitExpression(expr sst.ExpressionNode) error {
	if param, ok := expr.(sst.BindParamNode); ok {
		c.args = append(c.args, param.Value())
	}
	c.parts = append(c.parts, expr.Expr())
	return nil
}

// VisitExpressionGroupStart renders the opening parenthesis of a grouped
// expression.
func (c *Compiler) VisitExpressionGroupStart() error {
	c.parts = append(c.parts, "(")
	return nil
}

// VisitExpressionGroupEnd renders the closing parenthesis of a grouped
// expression.
func (c *Compiler) VisitExpressionGroupEnd() error {
	c.parts = append(c.parts, ")")
	return nil
}

// VisitSpace renders one SQL whitespace boundary.
func (c *Compiler) VisitSpace() error {
	c.parts = append(c.parts, " ")
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
	if column.Schema() != "" {
		if err := validateIdentifier("schema", column.Schema()); err != nil {
			return err
		}
	}
	if column.Table() != "" {
		if err := validateIdentifier("table", column.Table()); err != nil {
			return err
		}
	}
	if err := validateIdentifier("column", column.Name()); err != nil {
		return err
	}

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

// VisitListSeparator renders a comma before every list item after the first.
func (c *Compiler) VisitListSeparator(index int, sep string) error {
	if index > 0 {
		c.parts = append(c.parts, sep)
	}
	return nil
}

// VisitTableRef renders a qualified or unqualified SQL table reference.
func (c *Compiler) VisitTableRef(table sst.TableRefNode) error {
	if table.Schema() != "" {
		if err := validateIdentifier("schema", table.Schema()); err != nil {
			return err
		}
	}
	if err := validateIdentifier("table", table.Name()); err != nil {
		return err
	}

	parts := make([]string, 0, 2)
	if table.Schema() != "" {
		parts = append(parts, table.Schema())
	}
	parts = append(parts, table.Name())
	c.parts = append(c.parts, strings.Join(parts, "."))
	return nil
}

// VisitOrderItem renders an ORDER BY direction.
func (c *Compiler) VisitOrderItem(item sst.OrderItemNode) error {
	c.parts = append(c.parts, " ", string(item.Direction()))
	return nil
}

// VisitLimit renders the SELECT row limit value.
func (c *Compiler) VisitLimit(limit sst.LimitNode) error {
	c.parts = append(c.parts, strconv.Itoa(limit.Value()))
	return nil
}

// VisitOffset renders the SELECT row offset value.
func (c *Compiler) VisitOffset(offset sst.OffsetNode) error {
	c.parts = append(c.parts, strconv.Itoa(offset.Value()))
	return nil
}
