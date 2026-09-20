package compiler

import (
	"sync"
	"testing"

	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
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

func TestPlanRegistryReusesPreparedPlanByID(t *testing.T) {
	plan, err := Prepare(
		NewStatementCache(),
		dql.Select(sst.NewBindParam(42)),
		defaultDialect,
	)
	assert.NoError(t, err)

	registry := NewPlanRegistry()
	assert.NoError(t, registry.Put("user-by-id", plan))

	cached, ok := registry.Get("user-by-id")
	assert.True(t, ok)
	assert.Equal(t, plan.ShapeKey(), cached.ShapeKey())
	assert.Equal(t, "SELECT ?", cached.SQL())

	args, err := cached.Bind([]any{7})
	assert.NoError(t, err)
	assert.Equal(t, []any{7}, args)
	assert.Equal(t, 1, registry.Len())
}

func TestPlanRegistrySupportsZeroValue(t *testing.T) {
	var registry PlanRegistry
	plan := newCompiledStatement("SELECT 1", 0)

	assert.NoError(t, registry.Put("constant", plan))
	cached, ok := registry.Get("constant")

	assert.True(t, ok)
	assert.Equal(t, plan.SQL(), cached.SQL())
}

func TestPlanRegistryRejectsInvalidIDs(t *testing.T) {
	registry := NewPlanRegistry()
	plan := newCompiledStatement("SELECT 1", 0)

	assert.ErrorIs(t, registry.Put("", plan), ErrEmptyPlanID)
	assert.ErrorIs(t, (*PlanRegistry)(nil).Put("constant", plan), ErrNilPlanRegistry)
	_, ok := registry.Get(PlanID(" "))
	assert.False(t, ok)
}

func TestStatementCacheGetPreservesPublishedLayout(t *testing.T) {
	cache := NewStatementCache()
	shape := newCompiledStatement("SELECT ?", 1)
	assert.NoError(t, cache.Put("query", shape))

	cached, ok := cache.Get("query")
	assert.True(t, ok)
	layout := cached.BindLayout()
	layout[0] = Binding{position: 99, kind: SlotOffset}

	cached, ok = cache.Get("query")
	assert.True(t, ok)
	assert.Equal(t, 0, cached.BindLayout()[0].Position())
	assert.Equal(t, SlotBind, cached.BindLayout()[0].Kind())
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

	_, _, err = CompileCachedWithDialect(NewStatementCache(), stmt, nil, nil)
	assert.ErrorIs(t, err, ErrInvalidDialect)
}

func TestCompileCachedIncludesDialectIdentity(t *testing.T) {
	stmt := dql.Select(sst.NewColumnRef("users", "id"))
	cache := NewStatementCache()
	postgresDialect := postgresTestDialect{}
	sqliteDialect := namedQuestionMarkTestDialect{name: "sqlite"}
	postgresShape, _, err := CompileCachedWithDialect(cache, stmt, nil, postgresDialect)
	assert.NoError(t, err)
	sqliteShape, _, err := CompileCachedWithDialect(cache, stmt, nil, sqliteDialect)
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

	mysqlShape, _, err := CompileCachedWithDialect(cache, stmt, nil, mysqlDialect)
	assert.NoError(t, err)
	sqliteShape, _, err := CompileCachedWithDialect(cache, stmt, nil, sqliteDialect)
	assert.NoError(t, err)

	assert.Equal(t, mysqlShape.SQL(), sqliteShape.SQL())
	assert.NotEqual(t, mysqlShape.ShapeKey(), sqliteShape.ShapeKey())
	assert.Equal(t, 2, cache.Len())
}

func TestDeriveShapeKeyIgnoresBindValues(t *testing.T) {
	first := dql.Select(sst.NewBindParam(1))
	second := dql.Select(sst.NewBindParam(2))

	firstKey, err := DeriveShapeKey(first, defaultDialect)
	assert.NoError(t, err)
	secondKey, err := DeriveShapeKey(second, defaultDialect)
	assert.NoError(t, err)

	assert.Equal(t, firstKey, secondKey)
}

func TestCompiledStatementBindsNamedSlotsInSQLOrder(t *testing.T) {
	shape, err := CompileShape(dql.Select(
		sst.NewNamedParameterSlot("id"),
		sst.NewNamedParameterSlot("tenant"),
	))
	assert.NoError(t, err)
	assert.Equal(t, "SELECT ?, ?", shape.SQL())
	assert.Equal(t, []Binding{
		{position: 0, kind: SlotParameter, source: "id"},
		{position: 1, kind: SlotParameter, source: "tenant"},
	}, shape.BindLayout())

	buffer := shape.NewArgumentBuffer()
	assert.NoError(t, buffer.Set("tenant", "acme"))
	assert.NoError(t, buffer.Set("id", 42))

	args, err := shape.BindBuffer(buffer)
	assert.NoError(t, err)
	assert.Equal(t, []any{42, "acme"}, args)
}

func TestCompiledStatementRejectsPositionalBindingForNamedSlots(t *testing.T) {
	shape, err := CompileShape(dql.Select(
		sst.NewNamedParameterSlot("first"),
		sst.NewNamedParameterSlot("second"),
	))
	assert.NoError(t, err)

	_, err = shape.Bind([]any{"wrong-first", "wrong-second"})
	assert.ErrorIs(t, err, ErrSlotAddressedArgumentsRequired)
}

func TestArgumentBufferValidatesNamedSlotIdentity(t *testing.T) {
	shape, err := CompileShape(dql.Select(
		sst.NewNamedParameterSlot("id"),
		sst.NewNamedParameterSlot("tenant"),
	))
	assert.NoError(t, err)

	buffer := shape.NewArgumentBuffer()
	assert.ErrorIs(t, buffer.Set("unknown", 1), ErrUnknownBindSlot)
	assert.NoError(t, buffer.Set("id", 1))
	assert.ErrorIs(t, buffer.Set("id", 2), ErrDuplicateBindSlot)

	_, err = shape.BindBuffer(buffer)
	assert.ErrorIs(t, err, ErrMissingBindSlot)

	buffer.Reset()
	assert.NoError(t, buffer.Set("id", 1))
	assert.NoError(t, buffer.Set("tenant", "acme"))
	args, err := shape.BindBuffer(buffer)
	assert.NoError(t, err)
	assert.Equal(t, []any{1, "acme"}, args)
}
