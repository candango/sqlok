package sst

import (
	"errors"
	"fmt"
)

// ExpressionNode represents a SQL expression that can participate in a
// projection or expression operation.
type ExpressionNode interface {
	Node
	Expr() string
}

// ExpressionList represents a comma-separated list of expressions.
type ExpressionList struct {
	items []ExpressionNode
}

// NewExpressionList creates an expression list.
func NewExpressionList(items ...ExpressionNode) *ExpressionList {
	return &ExpressionList{
		items: append([]ExpressionNode(nil), items...),
	}
}

// Items returns the expressions in order.
func (l *ExpressionList) Items() []ExpressionNode {
	return l.items
}

// Accept visits each expression and delegates separators to the visitor.
func (l *ExpressionList) Accept(v Visitor) error {
	for i, item := range l.items {
		if err := v.VisitListSeparator(i); err != nil {
			return err
		}
		if err := item.Accept(v); err != nil {
			return err
		}
	}
	return nil
}

// BinaryExpressionNode represents an expression with two expression operands.
type BinaryExpressionNode interface {
	ExpressionNode
	Left() ExpressionNode
	Operator() ComparisonOperator
	Right() ExpressionNode
}

// BindParamNode represents an expression backed by a runtime argument.
type BindParamNode interface {
	ExpressionNode

	// Value returns the runtime argument associated with the expression.
	Value() any
}

// BinaryExpression represents a comparison between two expressions.
type BinaryExpression struct {
	left  ExpressionNode
	op    ComparisonOperator
	right ExpressionNode
}

// NewBinaryExpression creates a binary expression with the provided operands
// and comparison operator.
func NewBinaryExpression(left, right ExpressionNode, op ComparisonOperator) *BinaryExpression {
	return &BinaryExpression{
		left:  left,
		op:    op,
		right: right,
	}
}

// Eq creates an equality expression.
func Eq(left, right ExpressionNode) *BinaryExpression {
	return NewBinaryExpression(left, right, Equal)
}

// Gt creates a greater-than expression.
func Gt(left, right ExpressionNode) *BinaryExpression {
	return NewBinaryExpression(left, right, GreaterThan)
}

// Expr returns the operator token for the binary expression.
func (e *BinaryExpression) Expr() string {
	return " " + string(e.op) + " "
}

// Accept traverses the operands and dispatches the binary expression between
// them so visitors can render infix operators in the correct order.
func (e *BinaryExpression) Accept(v Visitor) error {
	if err := e.Left().Accept(v); err != nil {
		return err
	}
	switch e.Operator() {
	case Equal,
		NotEqual,
		GreaterThan,
		GreaterThanOrEqual,
		LessThan,
		LessThanOrEqual:
		if err := v.VisitExpression(e); err != nil {
			return err
		}
	default:
		return errors.New("unsupported comparison operator")
	}
	return e.Right().Accept(v)
}

// Left returns the left expression operand.
func (e *BinaryExpression) Left() ExpressionNode {
	return e.left
}

// Right returns the right expression operand.
func (e *BinaryExpression) Right() ExpressionNode {
	return e.right
}

// Operator returns the comparison operator.
func (e *BinaryExpression) Operator() ComparisonOperator {
	return e.op
}

// BindParam represents a runtime argument rendered as a placeholder.
type BindParam struct {
	value any
}

// NewBindParam creates a bind-parameter expression for the provided value.
func NewBindParam(value any) *BindParam {
	return &BindParam{value: value}
}

// Accept dispatches the bind-parameter expression to the provided visitor.
func (p *BindParam) Accept(v Visitor) error {
	return v.VisitExpression(p)
}

// Expr returns the placeholder representation of the bind parameter.
func (p *BindParam) Expr() string {
	return "?"
}

// Value returns the runtime value collected by the compiler.
func (p *BindParam) Value() any {
	return p.value
}

// Literal represents an expression rendered directly as SQL text.
type Literal struct {
	value any
}

// NewLiteral creates a literal expression from the provided value.
func NewLiteral(value any) *Literal {
	return &Literal{value: value}
}

// Expr returns the SQL text representation of the literal.
func (l *Literal) Expr() string {
	return fmt.Sprintf("%v", l.value)
}

// Accept dispatches the literal expression to the provided visitor.
func (l *Literal) Accept(v Visitor) error {
	return v.VisitExpression(l)
}
