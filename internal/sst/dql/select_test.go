package dql

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

type fakeVisitor struct {
	visitedSelect     bool
	visitedColumns    int
	visitedColumnRefs int
}

func (v *fakeVisitor) VisitSelect(s sst.SelectStatementNode) error {
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

func (v *fakeVisitor) VisitFromSource(s sst.FromSourceNode) error {
	return nil
}

func (v *fakeVisitor) VisitTableRef(s sst.TableRefNode) error {
	return nil
}

func (v *fakeVisitor) VisitJoin(s sst.JoinNode) error {
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
	visitedFrom       bool
	visitedJoin       bool
	joinEvents        []string
}

func (v *traversingVisitor) VisitSelect(s sst.SelectStatementNode) error {
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

func (v *traversingVisitor) VisitFromSource(s sst.FromSourceNode) error {
	v.visitedFrom = true

	if table := s.Table(); table != nil {
		if err := table.Accept(v); err != nil {
			return err
		}
	}

	if join := s.Join(); join != nil {
		return join.Accept(v)
	}

	return nil
}

func (v *traversingVisitor) VisitTableRef(s sst.TableRefNode) error {
	v.visitedTableRef = true
	v.joinEvents = append(v.joinEvents, s.Name())
	return nil
}

func (v *traversingVisitor) VisitJoin(j sst.JoinNode) error {
	v.visitedJoin = true
	v.joinEvents = append(v.joinEvents, "join")

	right := j.Right()
	if table := right.Table(); table != nil {
		if err := table.Accept(v); err != nil {
			return err
		}
	}

	if on := j.On(); on != nil {
		v.joinEvents = append(v.joinEvents, "on")
		if err := on.Accept(v); err != nil {
			return err
		}
	}

	if next := right.Join(); next != nil {
		return next.Accept(v)
	}
	return nil
}

func TestSelectAcceptVisitsSelect(t *testing.T) {
	visitor := &fakeVisitor{}
	selectNode := Select()

	if err := selectNode.Accept(visitor); err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}

	if !visitor.visitedSelect {
		t.Fatal("expected Select.Accept to call VisitSelect")
	}
}

func TestSelectTraversal(t *testing.T) {
	visitor := &traversingVisitor{}
	columnRef := sst.NewColumnRef("users", "id", sst.WithColumnSchema("public"))
	tableRef := sst.NewTableRef("users")
	fromSource := NewFromSource(tableRef)
	stmt := Select(NewSelectColumn(columnRef)).From(fromSource)

	assert.Len(t, stmt.Columns(), 1)
	assert.NoError(t, stmt.Accept(visitor))

	assert.True(t, visitor.visitedSelect)
	assert.Equal(t, 1, visitor.visitedColumns)
	assert.Equal(t, 1, visitor.visitedColumnRefs)
	assert.True(t, visitor.visitedFrom)
	assert.True(t, visitor.visitedTableRef)
}

func TestSelectJoinTraversalChain(t *testing.T) {
	t.Run("should traverse through a complete join chain", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(NewFromSource(sst.NewTableRef("users"))).
			Join(NewFromSource(sst.NewTableRef("orders"))).
			On(&fakeExpr{}).
			Join(NewFromSource(sst.NewTableRef("items"))).
			On(&fakeExpr{})

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"on",
			"join",
			"items",
			"on",
		}, visitor.joinEvents)
	})

	t.Run("should allow cartesian product due to a missing on clause", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(NewFromSource(sst.NewTableRef("users"))).
			Join(NewFromSource(sst.NewTableRef("orders"))).
			Join(NewFromSource(sst.NewTableRef("items"))).
			On(&fakeExpr{})

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"join",
			"items",
			"on",
		}, visitor.joinEvents)
	})
}
