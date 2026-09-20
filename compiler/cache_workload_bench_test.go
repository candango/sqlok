package compiler

import (
	"fmt"
	"testing"

	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
)

// benchmarkScaledStatement builds a SELECT whose traversal cost grows with
// scale: scale projected columns, scale joins, and a scale-term AND predicate.
// It is the workload knob used to compare Compile, CompileCached, and
// PlanRegistry across query complexity instead of at a single point.
func benchmarkScaledStatement(scale int) sst.StatementNode {
	columns := make([]sst.ExpressionNode, 0, scale)
	for i := range scale {
		columns = append(columns, sst.NewColumnRef("users", fmt.Sprintf("c%d", i)))
	}

	stmt := dql.Select(columns...).From(sst.NewTableRef("users"))
	for i := range scale {
		stmt = stmt.Join(sst.NewTableRef(fmt.Sprintf("t%d", i))).On(sst.Eq(
			sst.NewColumnRef(fmt.Sprintf("t%d", i), "user_id"),
			sst.NewColumnRef("users", "id"),
		))
	}

	terms := make([]sst.ExpressionNode, 0, scale)
	for i := range scale {
		terms = append(terms, sst.Eq(
			sst.NewColumnRef("users", fmt.Sprintf("c%d", i)),
			sst.NewBindParam(benchmarkID+i),
		))
	}
	return stmt.Where(sst.And(terms...))
}

func benchmarkScaledArgs(scale int) []any {
	args := make([]any, scale)
	for i := range args {
		args[i] = benchmarkID + i
	}
	return args
}

var benchmarkScales = []int{1, 4, 16, 64}

// BenchmarkWorkloadCompileDirect is the no-cache baseline: full AST compile
// on every call, statement construction hoisted out of the loop.
func BenchmarkWorkloadCompileDirect(b *testing.B) {
	for _, scale := range benchmarkScales {
		b.Run(fmt.Sprintf("scale=%d", scale), func(b *testing.B) {
			stmt := benchmarkScaledStatement(scale)
			b.ReportAllocs()
			for b.Loop() {
				benchmarkSQL, benchmarkArgs, benchmarkErr = Compile(stmt)
			}
		})
	}
}

// BenchmarkWorkloadCompileCachedHit derives the shape key, hits the cache, and
// reuses the compiled SQL. Statement construction is hoisted out of the loop
// so only the lookup path is measured.
func BenchmarkWorkloadCompileCachedHit(b *testing.B) {
	for _, scale := range benchmarkScales {
		b.Run(fmt.Sprintf("scale=%d", scale), func(b *testing.B) {
			cache := NewStatementCache()
			stmt := benchmarkScaledStatement(scale)
			args := benchmarkScaledArgs(scale)
			if _, _, err := CompileCached(cache, stmt, args); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				shape, bound, err := CompileCached(cache, stmt, args)
				benchmarkSQL, benchmarkArgs, benchmarkErr = shape.SQL(), bound, err
			}
		})
	}
}

// BenchmarkWorkloadPlanRegistryHit looks the compiled shape up by a stable
// application-owned plan ID, avoiding AST traversal and key derivation.
func BenchmarkWorkloadPlanRegistryHit(b *testing.B) {
	for _, scale := range benchmarkScales {
		b.Run(fmt.Sprintf("scale=%d", scale), func(b *testing.B) {
			shape, err := Prepare(
				NewStatementCache(),
				benchmarkScaledStatement(scale),
				defaultDialect,
			)
			if err != nil {
				b.Fatal(err)
			}

			registry := NewPlanRegistry()
			const planID PlanID = "workload-plan"
			if err := registry.Put(planID, shape); err != nil {
				b.Fatal(err)
			}
			args := benchmarkScaledArgs(scale)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				plan, ok := registry.Get(planID)
				if !ok {
					b.Fatal("plan not found")
				}
				benchmarkArgs, benchmarkErr = plan.Bind(args)
				benchmarkSQL = plan.SQL()
			}
		})
	}
}

// BenchmarkWorkloadDeriveShapeKey isolates the shape-key derivation that
// CompileCached must pay before it can look an entry up. It is the mechanism
// behind the cached-path cost, so it is measured separately from the compile
// it is supposed to avoid.
func BenchmarkWorkloadDeriveShapeKey(b *testing.B) {
	for _, scale := range benchmarkScales {
		b.Run(fmt.Sprintf("scale=%d", scale), func(b *testing.B) {
			stmt := benchmarkScaledStatement(scale)
			context := defaultShapeContext()
			b.ReportAllocs()
			for b.Loop() {
				key, err := deriveShapeKeyWithContext(stmt, context)
				benchmarkSQL, benchmarkErr = string(key), err
			}
		})
	}
}

// BenchmarkWorkloadNamedArgumentBufferHit measures the named prepared path
// with a reused buffer and prebuilt dynamic values.
func BenchmarkWorkloadNamedArgumentBufferHit(b *testing.B) {
	cache, err := NewBoundedStatementCache(16)
	if err != nil {
		b.Fatal(err)
	}
	shape, err := Prepare(
		cache,
		dql.Select(
			sst.NewNamedParameterSlot("user_id"),
			sst.NewNamedParameterSlot("tenant_id"),
		),
		defaultDialect,
	)
	if err != nil {
		b.Fatal(err)
	}

	type namedValues struct {
		userID any
		tenant any
	}
	values := make([]namedValues, benchmarkArgumentRingSize)
	for index := range values {
		values[index] = namedValues{
			userID: benchmarkID + index,
			tenant: fmt.Sprintf("tenant-%d", index),
		}
	}

	buffer := shape.NewArgumentBuffer()
	iteration := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		current := values[iteration&benchmarkArgumentRingMask]
		iteration++
		buffer.Reset()
		if err := buffer.Set("tenant_id", current.tenant); err != nil {
			b.Fatal(err)
		}
		if err := buffer.Set("user_id", current.userID); err != nil {
			b.Fatal(err)
		}
		benchmarkArgs, benchmarkErr = shape.BindBuffer(buffer)
		benchmarkSQL = shape.SQL()
	}
}
