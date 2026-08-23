package dml

import (
	"errors"
	"fmt"

	"github.com/candango/sqlok/internal/sst"
)

// InsertStatement is the concrete fluent builder and semantic root node of an
// INSERT statement. It implements sst.InsertBuilder for construction and
// sst.InsertStatementNode for traversal and compilation.
type InsertStatement struct {
	target  sst.TableRefNode
	columns *sst.CommaSeparatedList[sst.ColumnRefNode]
	values  *valuesClause
	err     error
}

var _ sst.InsertBuilder = (*InsertStatement)(nil)

// Insert creates an INSERT builder for the target table and optional columns.
func Insert(target sst.TableRefNode, columns ...sst.ColumnRefNode) *InsertStatement {
	i := &InsertStatement{target: target}
	if target == nil {
		i.err = errors.New("INSERT target table cannot be nil")
		return i
	}

	for _, column := range columns {
		if column == nil {
			i.err = errors.New("INSERT column cannot be nil")
			return i
		}
	}
	if len(columns) > 0 {
		i.columns = sst.NewCommaSeparatedList(columns...)
	}
	return i
}

// Accept dispatches the INSERT node and its children to the visitor.
func (i *InsertStatement) Accept(v sst.Visitor) error {
	if i.target == nil {
		return errors.New("INSERT target table cannot be nil")
	}
	if i.values == nil || len(i.values.rows.Items()) == 0 {
		return errors.New("INSERT requires at least one VALUES row")
	}

	if err := v.VisitStatement(i); err != nil {
		return err
	}
	if err := i.target.Accept(v); err != nil {
		return err
	}
	if i.columns != nil {
		if err := v.VisitSpace(); err != nil {
			return err
		}
		if err := v.VisitExpressionGroupStart(); err != nil {
			return err
		}
		if err := i.columns.Accept(v); err != nil {
			return err
		}
		if err := v.VisitExpressionGroupEnd(); err != nil {
			return err
		}
	}
	if err := v.VisitClause(i.values); err != nil {
		return err
	}
	return i.values.Accept(v)
}

// Declaration returns the INSERT statement keyword.
func (i *InsertStatement) Declaration() string {
	return "INSERT INTO"
}

// Err returns the first construction error recorded by the statement.
func (i *InsertStatement) Err() error {
	return i.err
}

// Target returns the table receiving inserted rows.
func (i *InsertStatement) Target() sst.TableRefNode {
	return i.target
}

// Columns returns the target columns in declaration order.
func (i *InsertStatement) Columns() *sst.CommaSeparatedList[sst.ColumnRefNode] {
	return i.columns
}

// Values appends one or more rows to the INSERT statement.
func (i *InsertStatement) Values(rows ...[]sst.ExpressionNode) sst.InsertBuilder {
	if i.err != nil {
		return i
	}
	if len(rows) == 0 {
		return i
	}
	if i.values == nil {
		i.values = newValuesClause()
	}

	columnCount := 0
	if i.columns != nil {
		columnCount = len(i.columns.Items())
	}
	for _, row := range rows {
		if len(row) == 0 {
			i.err = errors.New("INSERT VALUES row cannot be empty")
			return i
		}
		if columnCount > 0 && len(row) != columnCount {
			i.err = fmt.Errorf(
				"INSERT VALUES row has %d values; expected %d",
				len(row),
				columnCount,
			)
			return i
		}
		for _, value := range row {
			if value == nil {
				i.err = errors.New("INSERT VALUES expression cannot be nil")
				return i
			}
		}
		i.values.rows.Append(newValueRow(row...))
	}
	return i
}

type valuesClause struct {
	rows *sst.List[*valueRow]
}

var _ sst.ClauseNode = (*valuesClause)(nil)

func newValuesClause() *valuesClause {
	return &valuesClause{rows: sst.NewList[*valueRow]()}
}

func (c *valuesClause) Declaration() string {
	return "VALUES"
}

func (c *valuesClause) Accept(v sst.Visitor) error {
	for i, row := range c.rows.Items() {
		if err := v.VisitListSeparator(i, ", "); err != nil {
			return err
		}
		if err := row.Accept(v); err != nil {
			return err
		}
	}
	return nil
}

type valueRow struct {
	expressions *sst.CommaSeparatedList[sst.ExpressionNode]
}

func newValueRow(expressions ...sst.ExpressionNode) *valueRow {
	return &valueRow{expressions: sst.NewCommaSeparatedList(expressions...)}
}

func (r *valueRow) Accept(v sst.Visitor) error {
	if err := v.VisitExpressionGroupStart(); err != nil {
		return err
	}
	if err := r.expressions.Accept(v); err != nil {
		return err
	}
	return v.VisitExpressionGroupEnd()
}
