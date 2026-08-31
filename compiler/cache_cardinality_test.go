package compiler

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"

	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
)

const (
	cacheMeasurementSmall = 1_000
	cacheMeasurementLarge = 100_000
)

var benchmarkCacheEntries int

func measuredCompiledStatement(index int) CompiledStatement {
	suffix := strconv.Itoa(index)
	return CompiledStatement{
		sql:             "SELECT column_" + suffix,
		shapeKey:        ShapeKey("shape-" + suffix),
		dialect:         string(defaultDialect.Name()),
		compilerVersion: defaultCompilerVersion,
	}
}

func TestStatementCacheCardinalityForDistinctShapes(t *testing.T) {
	cache := NewStatementCache()

	for index := range cacheMeasurementSmall {
		stmt := dql.Select(
			sst.NewColumnRef("users_"+strconv.Itoa(index), "id"),
		)
		if _, _, err := CompileCached(cache, stmt, nil); err != nil {
			t.Fatalf("compile shape %d: %v", index, err)
		}
	}

	if got := cache.Len(); got != cacheMeasurementSmall {
		t.Fatalf("cache cardinality = %d, want %d", got, cacheMeasurementSmall)
	}
}

func TestStatementCacheCardinalityForRuntimeValueVariations(t *testing.T) {
	cache := NewStatementCache()

	for index := range cacheMeasurementSmall {
		stmt := dql.Select(sst.NewBindParam(index))
		if _, _, err := CompileCached(cache, stmt, []any{index}); err != nil {
			t.Fatalf("compile runtime variation %d: %v", index, err)
		}
	}

	if got := cache.Len(); got != 1 {
		t.Fatalf("cache cardinality = %d, want 1", got)
	}
}

func TestStatementCacheCountsHitsAndMissesByShape(t *testing.T) {
	cache := NewStatementCache()
	hits := 0
	misses := 0

	for index := range cacheMeasurementSmall {
		shape := measuredCompiledStatement(index)
		if _, ok := cache.Get(shape.ShapeKey()); ok {
			hits++
			continue
		}
		misses++
		if err := cache.Put(shape.ShapeKey(), shape); err != nil {
			t.Fatalf("put shape %d: %v", index, err)
		}
	}
	for index := range cacheMeasurementSmall {
		if _, ok := cache.Get(measuredCompiledStatement(index).ShapeKey()); ok {
			hits++
		} else {
			misses++
		}
	}

	if hits != cacheMeasurementSmall || misses != cacheMeasurementSmall {
		t.Fatalf("hits = %d, misses = %d; want %d each", hits, misses, cacheMeasurementSmall)
	}
}

func TestStatementCacheHeapGrowth(t *testing.T) {
	for _, size := range []int{
		cacheMeasurementSmall,
		10_000,
		cacheMeasurementLarge,
	} {
		t.Run(fmt.Sprintf("entries-%d", size), func(t *testing.T) {
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			cache := NewStatementCache()
			for index := 0; index < size; index++ {
				shape := measuredCompiledStatement(index)
				if err := cache.Put(shape.ShapeKey(), shape); err != nil {
					t.Fatalf("put shape %d: %v", index, err)
				}
			}
			if got := cache.Len(); got != size {
				t.Fatalf("cache cardinality = %d, want %d", got, size)
			}

			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(cache)
			var heapDelta uint64
			if after.HeapAlloc > before.HeapAlloc {
				heapDelta = after.HeapAlloc - before.HeapAlloc
			}
			t.Logf(
				"entries=%d heap_delta=%d bytes bytes_per_entry=%.2f",
				size,
				heapDelta,
				float64(heapDelta)/float64(size),
			)
		})
	}
}

func BenchmarkStatementCachePopulate(b *testing.B) {
	for _, size := range []int{
		cacheMeasurementSmall,
		10_000,
		cacheMeasurementLarge,
	} {
		b.Run(fmt.Sprintf("entries-%d", size), func(b *testing.B) {
			shapes := make([]CompiledStatement, size)
			for index := range shapes {
				shapes[index] = measuredCompiledStatement(index)
			}

			b.ReportAllocs()
			b.ReportMetric(float64(size), "entries/op")
			b.ResetTimer()
			for b.Loop() {
				cache := NewStatementCache()
				for _, shape := range shapes {
					if err := cache.Put(shape.ShapeKey(), shape); err != nil {
						b.Fatal(err)
					}
				}
				benchmarkCacheEntries = cache.Len()
			}
		})
	}
}
