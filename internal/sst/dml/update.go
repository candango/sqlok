package dml

import (
	"errors"

	"github.com/candango/sqlok/internal/sst"
)

// UpdateStatement is the concrete fluent builder and semantic root node of
// an UPDATE statement. It implements sst.UpdateBuilder for construction and
// sst.UpdateStatementNode for traversal and compilation.
type UpdateStatement struct {
	target      sst.TableRefNode
	assignments *sst.List[sst.AssignmentNode]
	where       sst.WhereCriteriaNode
	err         error
}

var _ sst.UpdateBuilder = (*UpdateStatement)(nil)

// Update creates an UPDATE builder for the target table.
func Update(target sst.TableRefNode) *UpdateStatement {
	u := &UpdateStatement{
		target:      target,
		assignments: sst.NewList[sst.AssignmentNode](),
	}
	if target == nil {
		u.err = errors.New("UPDATE target table cannot be nil")
	}
	return u
}

// Accept dispatches the UPDATE node and its children to the visitor.
func (u *UpdateStatement) Accept(v sst.Visitor) error {
	if u.target == nil {
		return errors.New("UPDATE target table cannot be nil")
	}
	if len(u.assignments.Items()) == 0 {
		return errors.New("UPDATE requires at least one SET assignment")
	}

	if err := v.VisitStatement(u); err != nil {
		return err
	}
	if err := u.target.Accept(v); err != nil {
		return err
	}
	if err := v.VisitClause(setClause{}); err != nil {
		return err
	}
	for i, assignment := range u.assignments.Items() {
		if err := v.VisitListSeparator(i, ", "); err != nil {
			return err
		}
		if err := assignment.Accept(v); err != nil {
			return err
		}
	}
	if u.where == nil {
		return nil
	}
	if err := v.VisitClause(u.where); err != nil {
		return err
	}
	return u.where.Accept(v)
}

// Declaration returns the UPDATE statement keyword.
func (u *UpdateStatement) Declaration() string {
	return "UPDATE"
}

// Err returns the first construction error recorded by the statement.
func (u *UpdateStatement) Err() error {
	return u.err
}

// Target returns the table being updated.
func (u *UpdateStatement) Target() sst.TableRefNode {
	return u.target
}

// Assignments returns SET assignments in declaration order.
func (u *UpdateStatement) Assignments() *sst.List[sst.AssignmentNode] {
	return u.assignments
}

// Set appends an assignment to the UPDATE statement.
func (u *UpdateStatement) Set(column sst.ColumnRefNode, value sst.ExpressionNode) sst.UpdateBuilder {
	if u.err != nil {
		return u
	}
	if column == nil {
		u.err = errors.New("UPDATE column cannot be nil")
		return u
	}
	if value == nil {
		u.err = errors.New("UPDATE value cannot be nil")
		return u
	}
	u.assignments.Append(&assignment{column: column, value: value})
	return u
}

// Where adds or combines a WHERE condition.
func (u *UpdateStatement) Where(condition sst.ExpressionNode) sst.UpdateBuilder {
	if u.err != nil {
		return u
	}
	if condition == nil {
		u.err = errors.New("WHERE condition cannot be nil")
		return u
	}
	if u.where != nil {
		condition = sst.And(u.where.Condition(), condition)
	}
	u.where = sst.NewWhereCriteria(condition)
	return u
}

type setClause struct{}

var _ sst.ClauseNode = setClause{}

func (setClause) Declaration() string {
	return "SET"
}

func (setClause) Accept(sst.Visitor) error {
	return nil
}

type assignment struct {
	column sst.ColumnRefNode
	value  sst.ExpressionNode
}

var _ sst.AssignmentNode = (*assignment)(nil)

func (a *assignment) Accept(v sst.Visitor) error {
	if err := a.column.Accept(v); err != nil {
		return err
	}
	if err := v.VisitExpression(assignmentOperator{}); err != nil {
		return err
	}
	return a.value.Accept(v)
}

func (a *assignment) Column() sst.ColumnRefNode {
	return a.column
}

func (a *assignment) Value() sst.ExpressionNode {
	return a.value
}

type assignmentOperator struct{}

var _ sst.ExpressionNode = assignmentOperator{}

func (assignmentOperator) Accept(sst.Visitor) error {
	return nil
}

func (assignmentOperator) Expr() string {
	return " = "
}
