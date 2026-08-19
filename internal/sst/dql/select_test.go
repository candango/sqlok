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

func (v *fakeVisitor) VisitStatement(s sst.StatementNode) error {
	switch s.(type) {
	case sst.SelectStatementNode:
		v.visitedSelect = true
	}
	return nil
}

func (v *fakeVisitor) VisitClause(s sst.ClauseNode) error {
	return nil
}

func (v *fakeVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *fakeVisitor) VisitExpression(expr sst.ExpressionNode) error {
	return nil
}

func (v *fakeVisitor) VisitExpressionGroupStart() error {
	return nil
}

func (v *fakeVisitor) VisitExpressionGroupEnd() error {
	return nil
}

func (v *fakeVisitor) VisitFromSource(s sst.FromSourceNode) error {
	return nil
}

func (v *fakeVisitor) VisitListSeparator(index int) error {
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
	visitedWhere             bool
	visitedJoin              bool
	joinEvents               []string
	visitedBinaryExpressions int
	visitedLogicalOperators  int
	visitedNotExpressions    int
	bindParams               []any
	visitedLiterals          int
}

func (v *traversingVisitor) VisitStatement(s sst.StatementNode) error {
	switch s.(type) {
	case sst.SelectStatementNode:
		v.visitedSelect = true
	}
	return nil
}

func (v *traversingVisitor) VisitColumnRef(s sst.ColumnRefNode) error {
	v.visitedColumnRefs++
	return nil
}

func (v *traversingVisitor) VisitClause(s sst.ClauseNode) error {
	if s.Declaration() == "WHERE" {
		v.visitedWhere = true
	}
	return nil
}

func (v *traversingVisitor) VisitExpression(expr sst.ExpressionNode) error {
	switch e := expr.(type) {
	case sst.BinaryExpressionNode:
		v.visitedBinaryExpressions++
		v.joinEvents = append(
			v.joinEvents,
			"binary:"+e.Left().Expr()+string(e.Expr())+e.Right().Expr(),
		)
	case sst.LogicalExpressionNode:
		v.visitedLogicalOperators++
	case *sst.NotExpression:
		v.visitedNotExpressions++
	case sst.BindParamNode:
		v.bindParams = append(v.bindParams, e.Value())
	case *sst.Literal:
		v.visitedLiterals++
	}
	return nil
}

func (v *traversingVisitor) VisitExpressionGroupStart() error {
	return nil
}

func (v *traversingVisitor) VisitExpressionGroupEnd() error {
	return nil
}

func (v *traversingVisitor) VisitListSeparator(index int) error {
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
	stmt := Select(columnRef).From(tableRef)

	assert.Len(t, stmt.Columns().Items(), 1)
	assert.NoError(t, stmt.Accept(visitor))

	assert.True(t, visitor.visitedSelect)
	assert.Equal(t, 1, visitor.visitedColumnRefs)
	assert.True(t, visitor.visitedFrom)
	assert.True(t, visitor.visitedTableRef)
}

func TestSelectWhereTraversal(t *testing.T) {
	visitor := &traversingVisitor{}
	stmt := Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(42),
		),
	)

	assert.NoError(t, stmt.Accept(visitor))
	assert.True(t, visitor.visitedWhere)
	assert.Equal(t, 1, visitor.visitedBinaryExpressions)
	assert.Equal(t, []any{42}, visitor.bindParams)
}

func TestSelectLogicalWhereTraversal(t *testing.T) {
	visitor := &traversingVisitor{}
	stmt := Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.And(
			sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
			sst.Eq(sst.NewColumnRef("users", "active"), sst.NewBindParam(true)),
			sst.Eq(sst.NewColumnRef("users", "role"), sst.NewBindParam("admin")),
		),
	)

	assert.NoError(t, stmt.Accept(visitor))
	assert.Equal(t, 2, visitor.visitedLogicalOperators)
	assert.Equal(t, 3, visitor.visitedBinaryExpressions)
	assert.Equal(t, []any{42, true, "admin"}, visitor.bindParams)
}

func TestSelectNotTraversal(t *testing.T) {
	visitor := &traversingVisitor{}
	stmt := Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Not(sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(42),
		)),
	)

	assert.NoError(t, stmt.Accept(visitor))
	assert.Equal(t, 1, visitor.visitedNotExpressions)
	assert.Equal(t, 1, visitor.visitedBinaryExpressions)
	assert.Equal(t, []any{42}, visitor.bindParams)
}

func TestSelectJoinTraversalChain(t *testing.T) {
	t.Run("should traverse through a complete join chain", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewColumnRef("orders", "user_id"))).
			Join(sst.NewTableRef("items")).
			On(sst.Eq(sst.NewColumnRef("orders", "id"), sst.NewColumnRef("items", "order_id")))

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, 2, visitor.visitedBinaryExpressions)
		assert.Equal(t, 4, visitor.visitedColumnRefs)
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"on",
			"binary:users.id = orders.user_id",
			"join",
			"items",
			"on",
			"binary:orders.id = items.order_id",
		}, visitor.joinEvents)
	})

	t.Run("should traverse through a join chain and resolve a bind param", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(2)))

		assert.NoError(t, stmt.Accept(visitor))
		assert.Equal(t, 1, visitor.visitedBinaryExpressions)
		assert.Equal(t, 1, visitor.visitedColumnRefs)
		assert.Equal(t, 1, len(visitor.bindParams))
		assert.Equal(t, []any{2}, visitor.bindParams)
		assert.Equal(t, []string{
			"users",
			"join",
			"orders",
			"on",
			"binary:users.id = ?",
		}, visitor.joinEvents)
	})

	t.Run("should traverse through a join chain and resolve a literal", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
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
			"binary:users.id = 1",
		}, visitor.joinEvents)
	})

	t.Run("should allow cartesian product due to a missing on clause", func(t *testing.T) {
		visitor := &traversingVisitor{}

		stmt := Select().
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			Join(sst.NewTableRef("items")).
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
