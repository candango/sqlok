// Package compiler turns SQL semantic-tree statements into SQL and arguments.
package compiler

import (
	"fmt"
	"strings"

	"github.com/candango/sqlok/dialect"
	"github.com/candango/sqlok/sst"
)

// Compile compiles a statement node into SQL text and bound arguments using
// the default dialect.
func Compile(stmt sst.StatementNode) (string, []any, error) {
	return compileWithContext(stmt, defaultShapeContext())
}

// CompileWithDialect compiles a statement using the supplied dialect.
func CompileWithDialect(
	stmt sst.StatementNode,
	renderingDialect dialect.Dialect,
) (string, []any, error) {
	context, err := newShapeContext(renderingDialect)
	if err != nil {
		return "", nil, err
	}
	return compileWithContext(stmt, context)
}

func compileWithContext(
	stmt sst.StatementNode,
	context shapeContext,
) (string, []any, error) {
	sqlText, args, bindings, err := compileStatementWithContext(stmt, context)
	if err != nil {
		return "", nil, err
	}
	if len(args) != len(bindings) {
		return "", nil, fmt.Errorf(
			"%w: expected %d arguments, got %d",
			ErrUnboundParameterSlot,
			len(bindings),
			len(args),
		)
	}
	return sqlText, args, nil
}

func compileStatementWithContext(
	stmt sst.StatementNode,
	context shapeContext,
) (string, []any, []Binding, error) {
	if err := context.validate(); err != nil {
		return "", nil, nil, err
	}
	if err := stmt.Err(); err != nil {
		return "", nil, nil, err
	}

	c := &Compiler{dialect: context.dialect}
	if err := stmt.Accept(c); err != nil {
		return "", nil, nil, err
	}
	return strings.Join(c.parts, ""), c.args, c.bindings, nil
}

// Compiler walks SQL semantic tree nodes and renders SQL.
type Compiler struct {
	parts    []string
	args     []any
	bindings []Binding
	dialect  dialect.Dialect
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
	text, ok := expr.(sst.ExpressionTextNode)
	if !ok {
		return fmt.Errorf("expression %T has no SQL text", expr)
	}
	c.parts = append(c.parts, text.Expr())
	return nil
}

// VisitBindParam renders a runtime bind parameter through the dialect.
func (c *Compiler) VisitBindParam(param sst.BindParamNode) error {
	return c.bind(SlotBind, param.Value())
}

// VisitParameterSlot reserves a runtime position without requiring a value.
func (c *Compiler) VisitParameterSlot(slot sst.ParameterSlotNode) error {
	source := ""
	if named, ok := slot.(sst.NamedParameterSlotNode); ok {
		source = strings.TrimSpace(named.Name())
		if source == "" {
			return ErrEmptyParameterSlotName
		}
	} else if slot.Position() != len(c.bindings) {
		return fmt.Errorf(
			"parameter slot expects position %d, got %d",
			len(c.bindings),
			slot.Position(),
		)
	}
	return c.reserveWithSource(SlotParameter, nil, false, source)
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

// VisitLimit renders the SELECT row limit as a runtime bind slot.
func (c *Compiler) VisitLimit(limit sst.LimitNode) error {
	return c.bind(SlotLimit, limit.Value())
}

// VisitOffset renders the SELECT row offset as a runtime bind slot.
func (c *Compiler) VisitOffset(offset sst.OffsetNode) error {
	return c.bind(SlotOffset, offset.Value())
}

func (c *Compiler) bind(kind SlotKind, value any) error {
	return c.reserve(kind, value, true)
}

func (c *Compiler) reserve(kind SlotKind, value any, hasValue bool) error {
	return c.reserveWithSource(kind, value, hasValue, "")
}

func (c *Compiler) reserveWithSource(
	kind SlotKind,
	value any,
	hasValue bool,
	source string,
) error {
	position := len(c.bindings)
	c.parts = append(c.parts, c.dialect.Placeholder(position))
	c.bindings = append(c.bindings, Binding{
		position: position,
		kind:     kind,
		source:   source,
	})
	if hasValue {
		c.args = append(c.args, value)
	}
	return nil
}
