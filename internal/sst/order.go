package sst

// OrderDirection identifies the direction of an ORDER BY item.
type OrderDirection string

const (
	// AscDirection orders values from lowest to highest.
	AscDirection OrderDirection = "ASC"
	// DescDirection orders values from highest to lowest.
	DescDirection OrderDirection = "DESC"
)

// OrderItemNode represents one expression and direction in an ORDER BY list.
type OrderItemNode interface {
	Node

	// Expression returns the expression being ordered.
	Expression() ExpressionNode

	// Direction returns the ordering direction.
	Direction() OrderDirection
}

// OrderItem represents one expression and direction in an ORDER BY clause.
type OrderItem struct {
	expression ExpressionNode
	direction  OrderDirection
}

// Accept dispatches the order item to the provided visitor.
func (oi *OrderItem) Accept(v Visitor) error {
	if err := oi.expression.Accept(v); err != nil {
		return err
	}
	return v.VisitOrderItem(oi)
}

// Expression returns the expression being ordered.
func (oi *OrderItem) Expression() ExpressionNode {
	return oi.expression
}

// Direction returns the ordering direction.
func (oi *OrderItem) Direction() OrderDirection {
	return oi.direction
}

// OrderItemOption configures an OrderItem during construction.
type OrderItemOption func(*OrderItem)

// NewOrderItem creates an ORDER BY item with the provided expression.
func NewOrderItem(expr ExpressionNode, options ...OrderItemOption) *OrderItem {
	oi := &OrderItem{
		expression: expr,
	}

	for _, option := range options {
		option(oi)
	}

	return oi
}

// WithDirection configures the direction of an ORDER BY item.
func WithDirection(direction OrderDirection) OrderItemOption {
	return func(oi *OrderItem) {
		oi.direction = direction
	}
}

// Asc creates an ORDER BY item with ascending direction.
func Asc(expr ExpressionNode) *OrderItem {
	return NewOrderItem(expr, WithDirection(AscDirection))
}

// Desc creates an ORDER BY item with descending direction.
func Desc(expr ExpressionNode) *OrderItem {
	return NewOrderItem(expr, WithDirection(DescDirection))
}
