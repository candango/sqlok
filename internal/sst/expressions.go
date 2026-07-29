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

// BinaryExpressionNode represents an expression with two expression operands.
type BinaryExpressionNode interface {
	ExpressionNode
	Left() ExpressionNode
	Operator() ComparisonOperator
	Right() ExpressionNode
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
	return string(e.op)
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
