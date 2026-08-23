package compiler

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dql"
	"github.com/stretchr/testify/assert"
)

func TestCompileRejectsInvalidTableIdentifier(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users; DROP TABLE users --"),
	)

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "invalid table identifier \"users; DROP TABLE users --\"")
}

func TestCompileRejectsInvalidColumnIdentifier(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id; DROP TABLE users --"),
	)

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "invalid column identifier \"id; DROP TABLE users --\"")
}

func TestCompileRejectsInvalidSchemaIdentifier(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id", sst.WithColumnSchema("public schema")),
	)

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "invalid schema identifier \"public schema\"")
}

func TestCompileAcceptsPortableIdentifiers(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "user_id", sst.WithColumnSchema("public")),
	).From(
		sst.NewTableRef("users", sst.WithTableSchema("public")),
	)

	sqlText, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT public.users.user_id FROM public.users", sqlText)
	assert.Empty(t, args)
}
