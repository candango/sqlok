package dql

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

type fakeVisitor struct {
	visitedSelect     bool
	visitedColumnRefs int
}

func (v *fakeVisitor) VisitSelect(s sst.SelectStatementNode) error {
	v.visitedSelect = true
	return nil
}

func (v *fakeVisitor) VisitBinaryExpression(expr sst.BinaryExpressionNode) error {
	return nil
}

func (v *fakeVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *fakeVisitor) VisitFromSource(s sst.FromSourceNode) error {
	return nil
}

func (v *fakeVisitor) VisitLiteral(expr sst.LiteralNode) error {
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
	visitedSelect            bool
	visitedColumnRefs        int
	visitedTableRef          bool
	visitedFrom              bool
	visitedJoin              bool
	joinEvents               []string
	visitedBinaryExpressions int
	visitedLiterals          int
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

func (v *traversingVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *traversingVisitor) VisitBinaryExpression(expr sst.BinaryExpressionNode) error {
	v.visitedBinaryExpressions++

	if err := expr.Left().Accept(v); err != nil {
		return err
	}

	v.joinEvents = append(v.joinEvents, "left:"+expr.Left().Expr())
	v.joinEvents = append(v.joinEvents, "binary:"+string(expr.Operator()))
	v.joinEvents = append(v.joinEvents, "right:"+expr.Right().Expr())

	return expr.Right().Accept(v)
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

func (v *traversingVisitor) VisitLiteral(l sst.LiteralNode) error {
	v.visitedLiterals++
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
	stmt := Select(columnRef).From(fromSource)

	assert.Len(t, stmt.Columns(), 1)
	assert.NoError(t, stmt.Accept(visitor))

	assert.True(t, visitor.visitedSelect)
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
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewColumnRef("orders", "user_id"))).
			Join(NewFromSource(sst.NewTableRef("items"))).
			On(sst.Eq(sst.NewColumnRef("orders", "id"), sst.NewColumnRef("items", "order_id")))

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, 2, visitor.visitedBinaryExpressions)
		assert.Equal(t, 4, visitor.visitedColumnRefs)
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"on",
			"left:users.id",
			"binary:=",
			"right:orders.user_id",
			"join",
			"items",
			"on",
			"left:orders.id",
			"binary:=",
			"right:items.order_id",
		}, visitor.joinEvents)
	})

	t.Run("should traverse through a join chain and resolve a literal", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(NewFromSource(sst.NewTableRef("users"))).
			Join(NewFromSource(sst.NewTableRef("orders"))).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewLiteral(1)))

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, 1, visitor.visitedBinaryExpressions)
		assert.Equal(t, 1, visitor.visitedColumnRefs)
		assert.Equal(t, 1, visitor.visitedLiterals)
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"on",
			"left:users.id",
			"binary:=",
			"right:1",
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
