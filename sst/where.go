package sst

import "errors"

// WhereCriteriaNode represents a WHERE clause shared by DQL and DML
// statements.
type WhereCriteriaNode interface {
	ClauseNode

	// Condition returns the boolean expression used by the WHERE clause.
	Condition() ExpressionNode
}

// WhereCriteria represents a WHERE clause around one boolean expression.
type WhereCriteria struct {
	condition ExpressionNode
}

var _ WhereCriteriaNode = (*WhereCriteria)(nil)

// NewWhereCriteria creates a WHERE clause for the provided condition.
func NewWhereCriteria(condition ExpressionNode) *WhereCriteria {
	return &WhereCriteria{condition: condition}
}

// Declaration returns the WHERE clause keyword.
func (w *WhereCriteria) Declaration() string {
	return "WHERE"
}

// Condition returns the expression used by the WHERE clause.
func (w *WhereCriteria) Condition() ExpressionNode {
	return w.condition
}

// Accept traverses the WHERE condition.
func (w *WhereCriteria) Accept(v Visitor) error {
	if w.condition == nil {
		return errors.New("WHERE condition cannot be nil")
	}
	return w.condition.Accept(v)
}
