package sst

import "fmt"

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

// Eq creates an equality expression.
func Eq(left, right ExpressionNode) *BinaryExpression {
	return NewBinaryExpression(left, right, Equal)
}

// Gt creates a greater-than expression.
func Gt(left, right ExpressionNode) *BinaryExpression {
	return NewBinaryExpression(left, right, GreaterThan)
}

// Expr marks BinaryExpression as an expression node.
func (e *BinaryExpression) Expr() string {
	return fmt.Sprintf("%s %s %s", e.left.Expr(), string(e.op), e.right.Expr())
}

// Accept dispatches the binary expression to the provided visitor.
func (e *BinaryExpression) Accept(v Visitor) error {
	return v.VisitBinaryExpression(e)
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
