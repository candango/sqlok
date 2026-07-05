package dql

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

type fakeVisitor struct {
	visitedSelect  bool
	visitedColumns int
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
	return nil
}

type fakeExpr struct{}

func (e *fakeExpr) Accept(v sst.Visitor) error {
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

func TestNewSelectStoresColumns(t *testing.T) {
	visitor := &fakeVisitor{}
	col1 := NewSelectColumn(&fakeExpr{})
	col2 := NewSelectColumn(&fakeExpr{})
	selectNode := NewSelect(col1, col2)

	assert.Len(t, selectNode.Columns(), 2)
	for _, col := range selectNode.Columns() {
		assert.NoError(t, col.Accept(visitor))
	}
	assert.Equal(t, 2, visitor.visitedColumns)
}
