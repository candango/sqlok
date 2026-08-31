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

func TestBoundedStatementCacheRejectsNonPositiveLimit(t *testing.T) {
	cache, err := NewBoundedStatementCache(0)

	assert.Nil(t, cache)
	assert.ErrorIs(t, err, ErrInvalidCacheLimit)
}
