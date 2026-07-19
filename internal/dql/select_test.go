package dql

import (
	"testing"

	"github.com/candango/sqlok/internal/elements"
	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

type fakeVisitor struct {
	visitedSelect     bool
	visitedColumns    int
	visitedColumnRefs int
}

func (v *fakeVisitor) VisitSelect(s sst.SelectNode) error {
	v.visitedSelect = true
	return nil
}

func (v *fakeVisitor) VisitSelectColumn(s sst.SelectColumnNode) error {
	v.visitedColumns++
	return nil
}

func (v *fakeVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *fakeVisitor) VisitTableRef(s sst.TableRefNode) error {
	return nil
}

type fakeExpr struct{}

func (e *fakeExpr) Accept(v sst.Visitor) error {
	return nil
}

type traversingVisitor struct {
	visitedSelect     bool
	visitedColumns    int
	visitedColumnRefs int
	visitedTableRef   bool
}

func (v *traversingVisitor) VisitSelect(s sst.SelectNode) error {
	v.visitedSelect = true

	for _, column := range s.Columns() {
		if err := column.Accept(v); err != nil {
			return err
		}
	}

	if source := s.Source(); source != nil {
		if err := source.Accept(v); err != nil {
			return err
		}
	}

	return nil
}

func (v *traversingVisitor) VisitSelectColumn(s sst.SelectColumnNode) error {
	v.visitedColumns++
	return s.Expr().Accept(v)
}

func (v *traversingVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *traversingVisitor) VisitTableRef(s sst.TableRefNode) error {
	v.visitedTableRef = true
	return nil
}

func TestSelectAcceptVisitsSelect(t *testing.T) {
	visitor := &fakeVisitor{}
	selectNode := NewSelect()

	if err := selectNode.Accept(visitor); err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}

	if !visitor.visitedSelect {
		t.Fatal("expected Select.Accept to call VisitSelect")
	}
}

func TestSelectTraversal(t *testing.T) {
	visitor := &traversingVisitor{}
	columnRef := elements.NewColumnRef("users", "id", elements.WithColumnSchema("public"))
	tableRef := elements.NewTableRef("users")
	selectNode := NewSelect(NewSelectColumn(columnRef)).From(tableRef)

	assert.Len(t, selectNode.Columns(), 1)
	assert.NoError(t, selectNode.Accept(visitor))

	assert.True(t, visitor.visitedSelect)
	assert.Equal(t, 1, visitor.visitedColumns)
	assert.Equal(t, 1, visitor.visitedColumnRefs)
	assert.True(t, visitor.visitedTableRef)
}
