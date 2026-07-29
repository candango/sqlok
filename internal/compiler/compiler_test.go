package compiler

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dql"
	"github.com/stretchr/testify/assert"
)

func TestCompileSelectWithColumnRef(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id", sst.WithColumnSchema("public")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT public.users.id", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithMultipleColumnRefs(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
		sst.NewColumnRef("users", "name"),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id, users.name", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithFromSource(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		dql.NewFromSource(
			sst.NewTableRef("users", sst.WithTableSchema("public")),
		),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM public.users", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithFromAndJoin(t *testing.T) {

	t.Run("should have from and join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(dql.NewFromSource(sst.NewTableRef("users"))).
			Join(dql.NewFromSource(sst.NewTableRef("orders")))

		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id, items.name FROM users JOIN orders", sql)
		assert.Empty(t, args)
	})

	t.Run("should have from, join chain with on clauses", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(dql.NewFromSource(sst.NewTableRef("users"))).
			Join(dql.NewFromSource(sst.NewTableRef("orders"))).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewColumnRef("orders", "user_id"))).
			Join(dql.NewFromSource(sst.NewTableRef("items"))).
			On(sst.Eq(sst.NewColumnRef("orders", "id"), sst.NewColumnRef("items", "order_id")))
		sql, args, err := Compile(stmt)

		expected := "SELECT users.id, items.name " +
			"FROM users " +
			"JOIN orders ON users.id = orders.user_id " +
			"JOIN items ON orders.id = items.order_id"

		assert.NoError(t, err)
		assert.Equal(t, expected, sql)
		assert.Empty(t, args)
	})

	t.Run("should have from, join with on clause and literal", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(dql.NewFromSource(sst.NewTableRef("users"))).
			Join(dql.NewFromSource(sst.NewTableRef("orders"))).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewLiteral(1)))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id, items.name FROM users JOIN orders ON users.id = 1", sql)
		assert.Empty(t, args)
	})
}
