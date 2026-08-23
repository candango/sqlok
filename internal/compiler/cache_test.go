package compiler

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dql"
	"github.com/stretchr/testify/assert"
)

func TestCompileCachedReusesShapeAndCollectsCurrentArgs(t *testing.T) {
	cache := NewStatementCache()
	first := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
	)
	second := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(7)),
	)

	firstShape, firstArgs, err := CompileCached(cache, "users-by-id", first)
	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users WHERE users.id = ?", firstShape.SQL())
	assert.Equal(t, []any{42}, firstArgs)

	secondShape, secondArgs, err := CompileCached(cache, "users-by-id", second)
	assert.NoError(t, err)
	assert.Equal(t, firstShape.SQL(), secondShape.SQL())
	assert.Equal(t, []any{7}, secondArgs)
	assert.Equal(t, 1, cache.Len())
}

func TestCompiledStatementBindRejectsWrongArgumentCount(t *testing.T) {
	shape := newCompiledStatement("SELECT ?", 1)

	_, err := shape.Bind(nil)

	assert.EqualError(t, err, "compiled statement expects 1 arguments, got 0")
}

func TestStatementCacheSupportsZeroValue(t *testing.T) {
	var cache StatementCache

	assert.NoError(t, cache.Put("constant", newCompiledStatement("SELECT 1", 0)))
	assert.Equal(t, 1, cache.Len())
}

func TestStatementCacheInvalidatesShape(t *testing.T) {
	cache := NewStatementCache()
	shape := newCompiledStatement("SELECT 1", 0)

	assert.NoError(t, cache.Put("constant", shape))
	assert.Equal(t, 1, cache.Len())
	assert.True(t, cache.Invalidate("constant"))
	assert.False(t, cache.Invalidate("constant"))
	assert.Equal(t, 0, cache.Len())
}

func TestCompileCachedRejectsInvalidCacheInputs(t *testing.T) {
	stmt := dql.Select(sst.NewLiteral(1))

	_, _, err := CompileCached(nil, "constant", stmt)
	assert.ErrorIs(t, err, ErrNilStatementCache)

	_, _, err = CompileCached(NewStatementCache(), "", stmt)
	assert.ErrorIs(t, err, ErrEmptyShapeKey)
}
