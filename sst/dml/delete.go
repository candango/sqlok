package dml

import (
	"errors"

	"github.com/candango/sqlok/sst"
)

// DeleteStatement is the concrete fluent builder and semantic root node of a
// DELETE statement. It implements sst.DeleteBuilder for construction and
// sst.DeleteStatementNode for traversal and compilation.
type DeleteStatement struct {
	target sst.TableRefNode
	where  sst.WhereCriteriaNode
	err    error
}

var _ sst.DeleteBuilder = (*DeleteStatement)(nil)

// Delete creates a DELETE builder for the target table.
func Delete(target sst.TableRefNode) *DeleteStatement {
	d := &DeleteStatement{target: target}
	if target == nil {
		d.err = errors.New("DELETE target table cannot be nil")
	}
	return d
}

// Accept dispatches the DELETE node and its children to the visitor.
// Callers should check Err before traversal.
func (d *DeleteStatement) Accept(v sst.Visitor) error {
	if err := v.VisitStatement(d); err != nil {
		return err
	}
	if err := d.target.Accept(v); err != nil {
		return err
	}
	if d.where == nil {
		return nil
	}
	if err := v.VisitClause(d.where); err != nil {
		return err
	}
	return d.where.Accept(v)
}

// Declaration returns the DELETE statement keyword.
func (d *DeleteStatement) Declaration() string {
	return "DELETE FROM"
}

// Err returns the first construction or deferred structural validation error.
func (d *DeleteStatement) Err() error {
	if d.err != nil {
		return d.err
	}
	if d.target == nil {
		return errors.New("DELETE target table cannot be nil")
	}
	return nil
}

// Target returns the table from which rows are deleted.
func (d *DeleteStatement) Target() sst.TableRefNode {
	return d.target
}

// Where adds or combines a WHERE condition.
func (d *DeleteStatement) Where(condition sst.ExpressionNode) sst.DeleteBuilder {
	if d.err != nil {
		return d
	}
	if condition == nil {
		d.err = errors.New("WHERE condition cannot be nil")
		return d
	}
	if d.where != nil {
		condition = sst.And(d.where.Condition(), condition)
	}
	d.where = sst.NewWhereCriteria(condition)
	return d
}
