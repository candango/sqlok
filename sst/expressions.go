package sst

import (
	"errors"
	"fmt"
)

// ExpressionNode represents a SQL expression that can participate in a
// projection or expression operation. Rendering is dispatched through the
// visitor because runtime parameters do not carry placeholder text.
type ExpressionNode interface {
	Node
}

// ExpressionTextNode represents an expression whose SQL text is owned by the
// node rather than produced by a runtime binding path.
type ExpressionTextNode interface {
	ExpressionNode
	Expr() string
}

const (
	atomicExpressionPrecedence     = 4
	comparisonExpressionPrecedence = 3
	notExpressionPrecedence        = 3
	andExpressionPrecedence        = 2
	orExpressionPrecedence         = 1
)

type precedenceNode interface {
	precedence() int
}

func expressionPrecedence(expr ExpressionNode) int {
	if node, ok := expr.(precedenceNode); ok {
		return node.precedence()
	}
	return atomicExpressionPrecedence
}

// BinaryExpressionNode represents an expression with two expression operands.
type BinaryExpressionNode interface {
	ExpressionTextNode
	Left() ExpressionNode
	Operator() ComparisonOperator
	Right() ExpressionNode
}

// LogicalExpressionNode represents a logical operation over one or more
// expressions.
type LogicalExpressionNode interface {
	ExpressionTextNode
	Operands() []ExpressionNode
	Operator() BooleanOperator
}

// BindParamNode represents a node backed by a runtime argument. Placeholder
// rendering belongs to the compiler and dialect, not to this contract.
type BindParamNode interface {
	Node

	// Value returns the runtime argument associated with the node.
	Value() any
}

// ParameterSlotNode represents a declared runtime argument position without a
// value available during shape compilation.
type ParameterSlotNode interface {
	Node

	// Position returns the zero-based runtime argument position.
	Position() int
}

// NamedParameterSlotNode represents a runtime slot with explicit logical
// identity. Its name is used to address the value during prepared binding.
type NamedParameterSlotNode interface {
	ParameterSlotNode

	// Name returns the logical identity of the runtime slot.
	Name() string
}

// BinaryExpression represents a comparison between two expressions.
type BinaryExpression struct {
	left  ExpressionNode
	op    ComparisonOperator
	right ExpressionNode
}

var _ BinaryExpressionNode = (*BinaryExpression)(nil)

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

func (e *BinaryExpression) precedence() int {
	return comparisonExpressionPrecedence
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

// LogicalExpression represents a logical operation over one or more
// expressions.
type LogicalExpression struct {
	operands []ExpressionNode
	op       BooleanOperator
}

var _ LogicalExpressionNode = (*LogicalExpression)(nil)

// NewLogicalExpression creates a logical expression with the provided
// operator and operands.
func NewLogicalExpression(operator BooleanOperator, operands ...ExpressionNode) *LogicalExpression {
	return &LogicalExpression{
		operands: append([]ExpressionNode(nil), operands...),
		op:       operator,
	}
}

// And creates a logical AND expression from one or more operands.
func And(operands ...ExpressionNode) *LogicalExpression {
	return NewLogicalExpression(AndOperator, operands...)
}

// Or creates a logical OR expression from one or more operands.
func Or(operands ...ExpressionNode) *LogicalExpression {
	return NewLogicalExpression(OrOperator, operands...)
}

// Expr returns the boolean operator token for the logical expression.
func (e *LogicalExpression) Expr() string {
	return " " + string(e.op) + " "
}

// Accept traverses the operands and dispatches the logical operator between
// them, grouping lower-precedence operands when required.
func (e *LogicalExpression) Accept(v Visitor) error {
	switch e.Operator() {
	case AndOperator, OrOperator:
	default:
		return errors.New("unsupported boolean operator")
	}
	if len(e.operands) == 0 {
		return fmt.Errorf("%s requires at least one expression", e.Operator())
	}

	for i, operand := range e.operands {
		if i > 0 {
			if err := v.VisitExpression(e); err != nil {
				return err
			}
		}
		if err := e.acceptOperand(v, operand); err != nil {
			return err
		}
	}
	return nil
}

func (e *LogicalExpression) acceptOperand(v Visitor, operand ExpressionNode) error {
	grouped := expressionPrecedence(operand) < e.precedence()
	if grouped {
		if err := v.VisitExpressionGroupStart(); err != nil {
			return err
		}
	}

	if err := operand.Accept(v); err != nil {
		return err
	}
	if grouped {
		return v.VisitExpressionGroupEnd()
	}
	return nil
}

func (e *LogicalExpression) precedence() int {
	switch e.Operator() {
	case AndOperator:
		return andExpressionPrecedence
	case OrOperator:
		return orExpressionPrecedence
	default:
		return 0
	}
}

// Operands returns the logical expression operands in traversal order.
func (e *LogicalExpression) Operands() []ExpressionNode {
	return e.operands
}

// Operator returns the boolean operator.
func (e *LogicalExpression) Operator() BooleanOperator {
	return e.op
}

// NotExpression represents boolean negation of one expression.
type NotExpression struct {
	operand ExpressionNode
}

var _ ExpressionNode = (*NotExpression)(nil)

// Not creates a logical NOT expression.
func Not(operand ExpressionNode) *NotExpression {
	return &NotExpression{operand: operand}
}

// Expr returns the NOT keyword and trailing space.
func (e *NotExpression) Expr() string {
	return "NOT "
}

// Accept renders NOT and traverses its operand, grouping compound expressions.
func (e *NotExpression) Accept(v Visitor) error {
	if e.operand == nil {
		return errors.New("NOT requires an expression")
	}
	if err := v.VisitExpression(e); err != nil {
		return err
	}

	grouped := isCompoundExpression(e.operand)
	if grouped {
		if err := v.VisitExpressionGroupStart(); err != nil {
			return err
		}
	}
	if err := e.operand.Accept(v); err != nil {
		return err
	}
	if grouped {
		return v.VisitExpressionGroupEnd()
	}
	return nil
}

func (e *NotExpression) precedence() int {
	return notExpressionPrecedence
}

func isCompoundExpression(expr ExpressionNode) bool {
	switch expr.(type) {
	case BinaryExpressionNode, LogicalExpressionNode:
		return true
	default:
		return false
	}
}

// Operand returns the expression being negated.
func (e *NotExpression) Operand() ExpressionNode {
	return e.operand
}

// BindParam represents a runtime argument rendered as a placeholder.
type BindParam struct {
	value any
}

var _ BindParamNode = (*BindParam)(nil)

// NewBindParam creates a bind-parameter expression for the provided value.
func NewBindParam(value any) *BindParam {
	return &BindParam{value: value}
}

// Accept dispatches the bind parameter to its dedicated visitor method.
func (p *BindParam) Accept(v Visitor) error {
	return v.VisitBindParam(p)
}

// Value returns the runtime value collected by the compiler.
func (p *BindParam) Value() any {
	return p.value
}

// ParameterSlot represents a runtime position declared without a value.
type ParameterSlot struct {
	position int
}

var _ ParameterSlotNode = (*ParameterSlot)(nil)

// NewParameterSlot creates a zero-based runtime parameter slot.
func NewParameterSlot(position int) *ParameterSlot {
	return &ParameterSlot{position: position}
}

// NamedParameterSlot represents a runtime slot addressed by logical name
// rather than by its SQL position.
type NamedParameterSlot struct {
	name string
}

var _ NamedParameterSlotNode = (*NamedParameterSlot)(nil)

// NewNamedParameterSlot creates a runtime parameter slot addressed by its
// logical name rather than by its SQL position.
func NewNamedParameterSlot(name string) *NamedParameterSlot {
	return &NamedParameterSlot{name: name}
}

// Accept dispatches the parameter slot to its dedicated visitor method.
func (p *ParameterSlot) Accept(v Visitor) error {
	return v.VisitParameterSlot(p)
}

// Position returns the zero-based runtime argument position declared by the
// slot.
func (p *ParameterSlot) Position() int {
	return p.position
}

// Accept dispatches the named parameter slot to the provided visitor.
func (p *NamedParameterSlot) Accept(v Visitor) error {
	return v.VisitParameterSlot(p)
}

// Position returns -1 because the SQL position is assigned during traversal.
func (p *NamedParameterSlot) Position() int {
	return -1
}

// Name returns the logical identity of the named runtime slot.
func (p *NamedParameterSlot) Name() string {
	return p.name
}

// Literal represents an expression rendered directly as SQL text.
type Literal struct {
	value any
}

var _ ExpressionNode = (*Literal)(nil)

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
