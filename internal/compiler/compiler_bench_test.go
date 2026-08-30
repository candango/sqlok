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

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dql"
)

var (
	benchmarkID   = 42
	benchmarkSQL  string
	benchmarkArgs []any
	benchmarkErr  error
)

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
	for i := 0; i < b.N; i++ {
		benchmarkSQL, benchmarkArgs = benchmarkFunctionQuery()
	}
}

func BenchmarkFunctionQueryWithJoinParts(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
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
// the statement shape and bind layout have already been prepared. Bind currently
// validates argument count and returns the already ordered argument slice.
func BenchmarkASTCompileCachedShape(b *testing.B) {
	shape, err := Prepare(
		NewStatementCache(),
		benchmarkASTStatement(),
		DefaultShapeContext(),
	)
	if err != nil {
		b.Fatal(err)
	}
	args := []any{benchmarkID, "second"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkArgs, benchmarkErr = shape.Bind(args)
		benchmarkSQL = shape.SQL()
	}
}
