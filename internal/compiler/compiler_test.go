package compiler

import (
	"testing"

	"github.com/candango/sqlok/internal/dql"
	"github.com/candango/sqlok/internal/elements"
	"github.com/stretchr/testify/assert"
)

func TestCompileSelectWithColumnRef(t *testing.T) {
	stmt := dql.NewSelect(
		dql.NewSelectColumn(
			elements.NewColumnRef("users", "id", elements.WithColumnSchema("public")),
		),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT public.users.id", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithMultipleColumnRefs(t *testing.T) {
	stmt := dql.NewSelect(
		dql.NewSelectColumn(elements.NewColumnRef("users", "id")),
		dql.NewSelectColumn(elements.NewColumnRef("users", "name")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id, users.name", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithFromSource(t *testing.T) {
	stmt := dql.NewSelect(
		dql.NewSelectColumn(elements.NewColumnRef("users", "id")),
	).From(
		elements.NewTableRef("users", elements.WithTableSchema("public")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM public.users", sql)
	assert.Empty(t, args)
}
