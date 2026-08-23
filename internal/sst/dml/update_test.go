package dml

import (
	"testing"

	"github.com/candango/sqlok/internal/compiler"
	"github.com/candango/sqlok/internal/sst"
	"github.com/stretchr/testify/assert"
)

func TestCompileUpdateWithWhere(t *testing.T) {
	stmt := Update(sst.NewTableRef("users")).
		Set(sst.NewColumnRef("", "name"), sst.NewBindParam("ana")).
		Set(sst.NewColumnRef("", "active"), sst.NewBindParam(true)).
		Where(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42))).
		Where(sst.Eq(sst.NewColumnRef("users", "role"), sst.NewBindParam("admin")))

	sql, args, err := compiler.Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "UPDATE users SET name = ?, active = ? WHERE users.id = ? AND users.role = ?", sql)
	assert.Equal(t, []any{"ana", true, 42, "admin"}, args)
}

func TestUpdateRequiresAssignments(t *testing.T) {
	stmt := Update(sst.NewTableRef("users"))

	_, _, err := compiler.Compile(stmt)

	assert.EqualError(t, err, "UPDATE requires at least one SET assignment")
}
