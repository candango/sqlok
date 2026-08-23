package compiler

import (
	"sync"
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

func TestCompileCachedRebuildsAfterShapeInvalidation(t *testing.T) {
	cache := NewStatementCache()
	first := dql.Select(sst.NewLiteral(1))
	second := dql.Select(sst.NewLiteral(2))

	shape, _, err := CompileCached(cache, "literal", first)
	assert.NoError(t, err)
	assert.Equal(t, "SELECT 1", shape.SQL())

	assert.True(t, cache.Invalidate("literal"))
	shape, _, err = CompileCached(cache, "literal", second)
	assert.NoError(t, err)
	assert.Equal(t, "SELECT 2", shape.SQL())
}

func TestCompiledStatementBindLayoutCannotBeMutatedThroughAccessor(t *testing.T) {
	shape := newCompiledStatement("SELECT ?", 1)
	layout := shape.BindLayout()
	layout[0] = Binding{position: 99}

	assert.Equal(t, 0, shape.BindLayout()[0].Position())
}

func TestStatementCacheSupportsConcurrentReaders(t *testing.T) {
	cache := NewStatementCache()
	shape := newCompiledStatement("SELECT ?", 1)
	assert.NoError(t, cache.Put("query", shape))

	const readers = 16
	const iterations = 100
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				cached, ok := cache.Get("query")
				if !ok {
					t.Errorf("expected cached statement")
					return
				}
				if _, err := cached.Bind([]any{1}); err != nil {
					t.Errorf("bind returned error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestCompileCachedRejectsInvalidCacheInputs(t *testing.T) {
	stmt := dql.Select(sst.NewLiteral(1))

	_, _, err := CompileCached(nil, "constant", stmt)
	assert.ErrorIs(t, err, ErrNilStatementCache)

	_, _, err = CompileCached(NewStatementCache(), "", stmt)
	assert.ErrorIs(t, err, ErrEmptyShapeKey)
}
