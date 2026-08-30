// These benchmarks keep a deliberately naive string-builder baseline next to
// the AST/compiler measurements. The AST path is expected to cost more because
// it builds, traverses, and renders semantic nodes. Use the numbers as a
// directional performance baseline and revisit them when optimizing the
// compiler or adding statement-shape caching; they are not an apples-to-apples
// comparison of equivalent query shapes yet.
package compiler

import (
	"strings"
	"testing"

	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
)

const (
	benchmarkID               = 42
	benchmarkArgumentRingSize = 256
	benchmarkArgumentRingMask = benchmarkArgumentRingSize - 1
)

var (
	benchmarkSQL  string
	benchmarkArgs []any
	benchmarkErr  error
)

func benchmarkArgumentRing() [][]any {
	ring := make([][]any, benchmarkArgumentRingSize)
	for i := range ring {
		ring[i] = []any{benchmarkID + i, "second"}
	}
	return ring
}

func benchmarkStringQuery() (string, []any) {
	return "SELECT users.id FROM users INNER JOIN orders " +
		"ON orders.user_id = users.id WHERE users.id = ?", []any{benchmarkID}
}

func benchmarkIdentifier(name string) string {
	return name
}

func benchmarkSelectSQL(columns, source, join, on, where string) string {
	return "SELECT " + columns + " FROM " + source +
		" INNER JOIN " + join + " ON " + on + " WHERE " + where
}

func benchmarkFunctionQuery() (string, []any) {
	column := benchmarkIdentifier("users.id")
	source := benchmarkIdentifier("users")
	join := benchmarkIdentifier("orders")
	on := benchmarkIdentifier("orders.user_id") +
		" = " + benchmarkIdentifier("users.id")
	where := benchmarkIdentifier("users.id") + " = ?"

	return benchmarkSelectSQL(column, source, join, on, where), []any{benchmarkID}
}

func benchmarkFunctionQueryWithJoinParts() (string, []any) {
	parts := []string{
		"SELECT ", "users.id", " FROM ", "users",
		" INNER JOIN ", "orders", " ON ",
		"orders.user_id = users.id", " WHERE users.id = ?",
	}
	return strings.Join(parts, ""), []any{benchmarkID}
}

func BenchmarkStringQueryAssembly(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQL, benchmarkArgs = benchmarkStringQuery()
	}
}

func BenchmarkFunctionQueryAssembly(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQL, benchmarkArgs = benchmarkFunctionQuery()
	}
}

func BenchmarkFunctionQueryWithJoinParts(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQL, benchmarkArgs = benchmarkFunctionQueryWithJoinParts()
	}
}

func benchmarkASTStatement() sst.StatementNode {
	return benchmarkASTStatementWithValues(benchmarkID, "second")
}

func benchmarkASTStatementWithValues(id int, second string) sst.StatementNode {
	return dql.Select(
		sst.NewColumnRef("users", "id"),
		sst.NewBindParam(id),
	).
		From(sst.NewTableRef("users")).
		Join(sst.NewTableRef("orders")).
		On(sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(second),
		))
}

func BenchmarkASTCompileEndToEnd(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQL, benchmarkArgs, benchmarkErr = Compile(benchmarkASTStatement())
	}
}

func BenchmarkASTCompileExistingStatement(b *testing.B) {
	stmt := benchmarkASTStatement()
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSQL, benchmarkArgs, benchmarkErr = Compile(stmt)
	}
}

func BenchmarkCompileCachedMiss(b *testing.B) {
	cache := NewStatementCache()
	b.ReportAllocs()
	for b.Loop() {
		cache.Clear()
		shape, args, err := CompileCached(
			cache,
			benchmarkASTStatement(),
			[]any{benchmarkID, "second"},
		)
		benchmarkSQL, benchmarkArgs, benchmarkErr = shape.SQL(), args, err
	}
}

// BenchmarkCompileCachedHit reuses one statement so the hit comparison does
// not include AST construction work.
func BenchmarkCompileCachedHit(b *testing.B) {
	cache := NewStatementCache()
	stmt := benchmarkASTStatement()
	_, _, benchmarkErr = CompileCached(
		cache,
		stmt,
		[]any{benchmarkID, "second"},
	)
	if benchmarkErr != nil {
		b.Fatal(benchmarkErr)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		shape, args, err := CompileCached(
			cache,
			stmt,
			[]any{benchmarkID + 1, "updated"},
		)
		benchmarkSQL, benchmarkArgs, benchmarkErr = shape.SQL(), args, err
	}
}

// BenchmarkASTCompileCachedShape measures the current prepared-plan path after
// the statement shape and bind layout have already been prepared. The argument
// buffer is populated on every iteration. Bind currently validates argument
// count and returns the already ordered argument slice.
func BenchmarkASTCompileCachedShape(b *testing.B) {
	shape, err := Prepare(
		NewStatementCache(),
		benchmarkASTStatement(),
		defaultDialect,
	)
	if err != nil {
		b.Fatal(err)
	}
	argumentRing := benchmarkArgumentRing()
	iteration := 0

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		args := argumentRing[iteration&benchmarkArgumentRingMask]
		iteration++
		benchmarkArgs, benchmarkErr = shape.Bind(args)
		benchmarkSQL = shape.SQL()
	}
}

// BenchmarkArgumentRingLookup calibrates the cost of selecting current
// arguments from the preallocated ring used by the warm-path benchmarks.
func BenchmarkArgumentRingLookup(b *testing.B) {
	argumentRing := benchmarkArgumentRing()
	iteration := 0

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkArgs = argumentRing[iteration&benchmarkArgumentRingMask]
		iteration++
	}
}

// BenchmarkPlanRegistryHit measures the explicit warm lookup path. It avoids
// AST traversal and shape-key derivation by using an application-owned plan ID.
// The argument buffer is populated on every iteration.
func BenchmarkPlanRegistryHit(b *testing.B) {
	shape, err := Prepare(
		NewStatementCache(),
		benchmarkASTStatement(),
		defaultDialect,
	)
	if err != nil {
		b.Fatal(err)
	}

	registry := NewPlanRegistry()
	const planID PlanID = "users-by-id"
	if err := registry.Put(planID, shape); err != nil {
		b.Fatal(err)
	}
	argumentRing := benchmarkArgumentRing()
	iteration := 0

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		args := argumentRing[iteration&benchmarkArgumentRingMask]
		iteration++
		plan, ok := registry.Get(planID)
		if !ok {
			b.Fatal("prepared plan not found")
		}
		benchmarkArgs, benchmarkErr = plan.Bind(args)
		benchmarkSQL = plan.SQL()
	}
}
