package sst

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWhereCriteria(t *testing.T) {
	condition := Eq(NewColumnRef("users", "id"), NewBindParam(42))
	where := NewWhereCriteria(condition)

	assert.Equal(t, "WHERE", where.Declaration())
	assert.Same(t, condition, where.Condition())
}
