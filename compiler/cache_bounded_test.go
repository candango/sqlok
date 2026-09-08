package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBoundedStatementCacheEvictsOldestShape(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	first := measuredCompiledStatement(1)
	second := measuredCompiledStatement(2)
	third := measuredCompiledStatement(3)
	assert.NoError(t, cache.Put(first.ShapeKey(), first))
	assert.NoError(t, cache.Put(second.ShapeKey(), second))
	assert.NoError(t, cache.Put(third.ShapeKey(), third))

	_, firstFound := cache.Get(first.ShapeKey())
	_, secondFound := cache.Get(second.ShapeKey())
	_, thirdFound := cache.Get(third.ShapeKey())

	assert.False(t, firstFound)
	assert.True(t, secondFound)
	assert.True(t, thirdFound)
	assert.Equal(t, 2, cache.Len())
}

func TestBoundedStatementCacheUpdatesWithoutGrowing(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	first := measuredCompiledStatement(1)
	updated := measuredCompiledStatement(10)
	updated.shapeKey = first.ShapeKey()
	second := measuredCompiledStatement(2)
	third := measuredCompiledStatement(3)

	assert.NoError(t, cache.Put(first.ShapeKey(), first))
	assert.NoError(t, cache.Put(second.ShapeKey(), second))
	assert.NoError(t, cache.Put(updated.ShapeKey(), updated))
	assert.Equal(t, 2, cache.Len())

	cached, found := cache.Get(first.ShapeKey())
	assert.True(t, found)
	assert.Equal(t, updated.SQL(), cached.SQL())

	assert.NoError(t, cache.Put(third.ShapeKey(), third))
	_, found = cache.Get(first.ShapeKey())
	assert.False(t, found)
	assert.Equal(t, 2, cache.Len())
}

func TestBoundedStatementCacheInvalidationSkipsRemovedEntries(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	first := measuredCompiledStatement(1)
	second := measuredCompiledStatement(2)
	third := measuredCompiledStatement(3)
	assert.NoError(t, cache.Put(first.ShapeKey(), first))
	assert.NoError(t, cache.Put(second.ShapeKey(), second))
	assert.True(t, cache.Invalidate(first.ShapeKey()))
	assert.NoError(t, cache.Put(third.ShapeKey(), third))

	_, secondFound := cache.Get(second.ShapeKey())
	_, thirdFound := cache.Get(third.ShapeKey())
	assert.True(t, secondFound)
	assert.True(t, thirdFound)
	assert.Equal(t, 2, cache.Len())
}

func TestBoundedStatementCacheReinsertionPreservesInsertionOrder(t *testing.T) {
	cache, err := NewBoundedStatementCache(3)
	assert.NoError(t, err)

	first := measuredCompiledStatement(1)
	second := measuredCompiledStatement(2)
	third := measuredCompiledStatement(3)
	assert.NoError(t, cache.Put(first.ShapeKey(), first))
	assert.NoError(t, cache.Put(second.ShapeKey(), second))
	assert.NoError(t, cache.Put(third.ShapeKey(), third))

	assert.True(t, cache.Invalidate(first.ShapeKey()))
	assert.NoError(t, cache.Put(first.ShapeKey(), first))

	fourth := measuredCompiledStatement(4)
	assert.NoError(t, cache.Put(fourth.ShapeKey(), fourth))

	_, firstFound := cache.Get(first.ShapeKey())
	_, secondFound := cache.Get(second.ShapeKey())
	_, thirdFound := cache.Get(third.ShapeKey())
	_, fourthFound := cache.Get(fourth.ShapeKey())
	assert.True(t, firstFound)
	assert.False(t, secondFound)
	assert.True(t, thirdFound)
	assert.True(t, fourthFound)
}

func TestBoundedStatementCacheInvalidationReleasesEvictionOrder(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	for index := range 1_000 {
		shape := measuredCompiledStatement(index)
		assert.NoError(t, cache.Put(shape.ShapeKey(), shape))
		assert.True(t, cache.Invalidate(shape.ShapeKey()))
	}

	assert.Equal(t, 0, cache.Len())
	assert.Nil(t, cache.order)
	assert.Equal(t, 0, cache.head)
}

func TestBoundedStatementCacheRejectsNonPositiveLimit(t *testing.T) {
	cache, err := NewBoundedStatementCache(0)

	assert.Nil(t, cache)
	assert.ErrorIs(t, err, ErrInvalidCacheLimit)
}

// TestBoundedStatementCacheKeepsEvictionOrderBoundedUnderSustainedLoad drives
// the Put-only bounded-cache workload: unique shapes arriving forever past
// capacity, with no invalidation. It is the only test exercising
// compactOrderLocked, which is the mechanism that stops the eviction queue
// from retaining every key ever inserted.
func TestBoundedStatementCacheKeepsEvictionOrderBoundedUnderSustainedLoad(t *testing.T) {
	const maxEntries = 64

	cache, err := NewBoundedStatementCache(maxEntries)
	assert.NoError(t, err)

	for index := range 10_000 {
		shape := measuredCompiledStatement(index)
		assert.NoError(t, cache.Put(shape.ShapeKey(), shape))

		cache.mu.RLock()
		queued := len(cache.order)
		head := cache.head
		cache.mu.RUnlock()

		assert.LessOrEqual(t, head, queued,
			"eviction queue head passed its length at insertion %d", index)
		assert.LessOrEqual(t, queued, 2*maxEntries,
			"eviction queue grew past twice the cache bound at insertion %d", index)
		assert.Equal(t, cache.Len(), queued-head,
			"live queue diverged from entries at insertion %d", index)
		assert.LessOrEqual(t, queued-head, maxEntries,
			"live queue exceeded the cache bound at insertion %d", index)
	}

	assert.Equal(t, maxEntries, cache.Len())

	newest := measuredCompiledStatement(9_999)
	oldest := measuredCompiledStatement(0)
	_, newestFound := cache.Get(newest.ShapeKey())
	_, oldestFound := cache.Get(oldest.ShapeKey())
	assert.True(t, newestFound)
	assert.False(t, oldestFound)
}

// TestBoundedStatementCacheClearResetsEvictionState covers the lifecycle
// contract for Clear on a bounded cache: entries, the eviction queue, and the
// queue head must all be reset, and the cache must remain usable afterwards.
func TestBoundedStatementCacheClearResetsEvictionState(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	for index := range 5 {
		shape := measuredCompiledStatement(index)
		assert.NoError(t, cache.Put(shape.ShapeKey(), shape))
	}
	assert.Equal(t, 2, cache.Len())

	cache.Clear()

	assert.Equal(t, 0, cache.Len())
	assert.Nil(t, cache.order)
	assert.Equal(t, 0, cache.head)

	revived := measuredCompiledStatement(42)
	assert.NoError(t, cache.Put(revived.ShapeKey(), revived))
	_, found := cache.Get(revived.ShapeKey())
	assert.True(t, found)
	assert.Equal(t, 1, cache.Len())
}

// TestBoundedStatementCacheInvalidateOnEmptyCacheIsSafe covers the
// removeOrderKeyLocked guard for an already drained eviction queue, which is
// reachable whenever Invalidate runs against a bounded cache holding nothing.
func TestBoundedStatementCacheInvalidateOnEmptyCacheIsSafe(t *testing.T) {
	cache, err := NewBoundedStatementCache(2)
	assert.NoError(t, err)

	absent := measuredCompiledStatement(1)
	assert.False(t, cache.Invalidate(absent.ShapeKey()))

	assert.Equal(t, 0, cache.Len())
	assert.Nil(t, cache.order)
	assert.Equal(t, 0, cache.head)

	assert.NoError(t, cache.Put(absent.ShapeKey(), absent))
	_, found := cache.Get(absent.ShapeKey())
	assert.True(t, found)
}
