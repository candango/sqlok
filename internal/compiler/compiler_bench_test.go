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
	return dql.Select(
		sst.NewColumnRef("users", "id"),
		sst.NewBindParam(benchmarkID),
	).
		From(sst.NewTableRef("users")).
		Join(sst.NewTableRef("orders")).
		On(sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam("second"),
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
