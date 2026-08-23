package dml

import (
	"testing"

	"github.com/candango/sqlok/internal/compiler"
	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

func TestCompileInsertWithMultipleRows(t *testing.T) {
	stmt := Insert(
		sst.NewTableRef("users"),
		sst.NewColumnRef("", "id"),
		sst.NewColumnRef("", "name"),
	).Values(
		[]sst.ExpressionNode{sst.NewBindParam(1), sst.NewBindParam("ana")},
		[]sst.ExpressionNode{sst.NewBindParam(2), sst.NewBindParam("bob")},
	)

	sql, args, err := compiler.Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "INSERT INTO users (id, name) VALUES (?, ?), (?, ?)", sql)
	assert.Equal(t, []any{1, "ana", 2, "bob"}, args)
}

func TestInsertRejectsValuesWithWrongColumnCount(t *testing.T) {
	stmt := Insert(
		sst.NewTableRef("users"),
		sst.NewColumnRef("", "id"),
		sst.NewColumnRef("", "name"),
	).Values([]sst.ExpressionNode{sst.NewBindParam(1)})

	_, _, err := compiler.Compile(stmt)

	assert.EqualError(t, err, "INSERT VALUES row has 1 values; expected 2")
}

func TestInsertRequiresValues(t *testing.T) {
	stmt := Insert(sst.NewTableRef("users"))

	_, _, err := compiler.Compile(stmt)

	assert.EqualError(t, err, "INSERT requires at least one VALUES row")
}
