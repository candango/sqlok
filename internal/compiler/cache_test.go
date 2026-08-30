package compiler

import (
	"sync"
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dql"
	"github.com/stretchr/testify/assert"
)

func TestCompileCachedReusesShapeAndBindsSuppliedArgs(t *testing.T) {
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

	firstShape, firstArgs, err := CompileCached(cache, first, []any{42})
	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users WHERE users.id = ?", firstShape.SQL())
	assert.Equal(t, []any{42}, firstArgs)

	secondShape, secondArgs, err := CompileCached(cache, second, []any{7})
	assert.NoError(t, err)
	assert.Equal(t, firstShape.SQL(), secondShape.SQL())
	assert.Equal(t, []any{7}, secondArgs)
	assert.Equal(t, 1, cache.Len())
}

func TestCompileCachedReusesShapeForDifferentPaginationValues(t *testing.T) {
	cache := NewStatementCache()
	first := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Limit(10).
		Offset(20)
	second := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Limit(30).
		Offset(40)

	firstShape, firstArgs, err := CompileCached(cache, first, []any{10, 20})
	assert.NoError(t, err)
	secondShape, secondArgs, err := CompileCached(cache, second, []any{30, 40})
	assert.NoError(t, err)

	assert.Equal(t, "SELECT users.id FROM users LIMIT ? OFFSET ?", firstShape.SQL())
	assert.Equal(t, firstShape.SQL(), secondShape.SQL())
	assert.Equal(t, []any{10, 20}, firstArgs)
	assert.Equal(t, []any{30, 40}, secondArgs)
	assert.Equal(t, firstShape.ShapeKey(), secondShape.ShapeKey())
	assert.Equal(t, 1, cache.Len())
}

func TestCompileShapeUsesExplicitParameterSlots(t *testing.T) {
	shape, err := CompileShape(dql.Select(sst.NewParameterSlot(0)))

	assert.NoError(t, err)
	assert.Equal(t, "SELECT ?", shape.SQL())
	assert.Equal(t, []Binding{{position: 0, kind: SlotParameter}}, shape.BindLayout())
	assert.NoError(t, func() error {
		_, bindErr := shape.Bind([]any{"value"})
		return bindErr
	}())
}

func TestCompileRejectsShapeOnlyParameterSlot(t *testing.T) {
	_, _, err := Compile(dql.Select(sst.NewParameterSlot(0)))

	assert.ErrorIs(t, err, ErrUnboundParameterSlot)
}

func TestCompileRejectsMixedShapeAndRuntimeSlots(t *testing.T) {
	_, _, err := Compile(dql.Select(
		sst.NewParameterSlot(0),
		sst.NewBindParam(42),
	))

	assert.ErrorIs(t, err, ErrUnboundParameterSlot)
}

func TestCompileRejectsOutOfOrderParameterSlot(t *testing.T) {
	_, _, err := Compile(dql.Select(sst.NewParameterSlot(1)))

	assert.EqualError(t, err, "parameter slot expects position 0, got 1")
}

func TestCompileShapeRecordsSlotKinds(t *testing.T) {
	shape, err := CompileShape(
		dql.Select(sst.NewColumnRef("users", "id")).
			Where(sst.Eq(
				sst.NewColumnRef("users", "id"),
				sst.NewBindParam(42),
			)).
			Limit(10).
			Offset(20),
	)

	assert.NoError(t, err)
	assert.Equal(t, []Binding{
		{position: 0, kind: SlotBind},
		{position: 1, kind: SlotLimit},
		{position: 2, kind: SlotOffset},
	}, shape.BindLayout())
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

func TestCompileCachedDerivesDifferentKeysForDifferentShapes(t *testing.T) {
	cache := NewStatementCache()
	first := dql.Select(sst.NewColumnRef("users", "id"))
	second := dql.Select(sst.NewColumnRef("orders", "id"))

	firstShape, _, err := CompileCached(cache, first, nil)
	assert.NoError(t, err)
	secondShape, _, err := CompileCached(cache, second, nil)
	assert.NoError(t, err)

	assert.NotEqual(t, firstShape.ShapeKey(), secondShape.ShapeKey())
	assert.Equal(t, "SELECT users.id", firstShape.SQL())
	assert.Equal(t, "SELECT orders.id", secondShape.SQL())
	assert.Equal(t, 2, cache.Len())
}

func TestCompileCachedRebuildsAfterShapeInvalidation(t *testing.T) {
	cache := NewStatementCache()
	stmt := dql.Select(sst.NewLiteral(1))

	shape, _, err := CompileCached(cache, stmt, nil)
	assert.NoError(t, err)
	assert.Equal(t, "SELECT 1", shape.SQL())

	assert.True(t, cache.Invalidate(shape.ShapeKey()))
	shape, _, err = CompileCached(cache, stmt, nil)
	assert.NoError(t, err)
	assert.Equal(t, "SELECT 1", shape.SQL())
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

	_, _, err := CompileCached(nil, stmt, nil)
	assert.ErrorIs(t, err, ErrNilStatementCache)

	_, _, err = CompileCachedWithContext(NewStatementCache(), stmt, nil, ShapeContext{})
	assert.ErrorIs(t, err, ErrInvalidShapeContext)
}

func TestCompileCachedIncludesShapeContext(t *testing.T) {
	stmt := dql.Select(sst.NewColumnRef("users", "id"))
	cache := NewStatementCache()
	postgresDialect := postgresTestDialect{}
	sqliteDialect := namedQuestionMarkTestDialect{name: "sqlite"}
	postgres := ShapeContext{Dialect: postgresDialect, CompilerVersion: "compiler-v1"}
	sqlite := ShapeContext{Dialect: sqliteDialect, CompilerVersion: "compiler-v1"}

	postgresShape, _, err := CompileCachedWithContext(cache, stmt, nil, postgres)
	assert.NoError(t, err)
	sqliteShape, _, err := CompileCachedWithContext(cache, stmt, nil, sqlite)
	assert.NoError(t, err)

	assert.NotEqual(t, postgresShape.ShapeKey(), sqliteShape.ShapeKey())
	assert.Equal(t, "postgres", postgresShape.Dialect())
	assert.Equal(t, "sqlite", sqliteShape.Dialect())
	assert.Equal(t, 2, cache.Len())
}

func TestCompileCachedDistinguishesQuestionMarkDialectIdentities(t *testing.T) {
	stmt := dql.Select(sst.NewColumnRef("users", "id"))
	cache := NewStatementCache()
	mysqlDialect := namedQuestionMarkTestDialect{name: "mysql"}
	sqliteDialect := namedQuestionMarkTestDialect{name: "sqlite"}

	mysqlShape, _, err := CompileCachedWithContext(cache, stmt, nil, ShapeContext{
		Dialect:         mysqlDialect,
		CompilerVersion: "compiler-v1",
	})
	assert.NoError(t, err)
	sqliteShape, _, err := CompileCachedWithContext(cache, stmt, nil, ShapeContext{
		Dialect:         sqliteDialect,
		CompilerVersion: "compiler-v1",
	})
	assert.NoError(t, err)

	assert.Equal(t, mysqlShape.SQL(), sqliteShape.SQL())
	assert.NotEqual(t, mysqlShape.ShapeKey(), sqliteShape.ShapeKey())
	assert.Equal(t, 2, cache.Len())
}

func TestDeriveShapeKeyIgnoresBindValues(t *testing.T) {
	context := DefaultShapeContext()
	first := dql.Select(sst.NewBindParam(1))
	second := dql.Select(sst.NewBindParam(2))

	firstKey, err := DeriveShapeKey(first, context)
	assert.NoError(t, err)
	secondKey, err := DeriveShapeKey(second, context)
	assert.NoError(t, err)

	assert.Equal(t, firstKey, secondKey)
}
