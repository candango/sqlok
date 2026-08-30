package dml

import (
	"testing"

	"github.com/candango/sqlok/compiler"
	"github.com/candango/sqlok/sst"
	"github.com/stretchr/testify/assert"
)

func TestCompileDeleteWithWhere(t *testing.T) {
	stmt := Delete(sst.NewTableRef("users")).
		Where(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42))).
		Where(sst.Eq(sst.NewColumnRef("users", "active"), sst.NewBindParam(true)))

	sql, args, err := compiler.Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "DELETE FROM users WHERE users.id = ? AND users.active = ?", sql)
	assert.Equal(t, []any{42, true}, args)
}

func TestCompileDeleteWithoutWhere(t *testing.T) {
	stmt := Delete(sst.NewTableRef("users"))

	sql, args, err := compiler.Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "DELETE FROM users", sql)
	assert.Empty(t, args)
}
