package elements

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewColumnRefWithSchema(t *testing.T) {
	column := NewColumnRef("users", "id", WithColumnSchema("public"))

	assert.Equal(t, "public", column.Schema())
}

func TestNewTableRefWithSchema(t *testing.T) {
	table := NewTableRef("users", WithTableSchema("public"))

	assert.Equal(t, "public", table.Schema())
}
