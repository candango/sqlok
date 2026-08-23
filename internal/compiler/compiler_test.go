package compiler

import (
	"testing"

	"github.com/candango/sqlok/internal/sst"
	"github.com/candango/sqlok/internal/sst/dml"
	"github.com/candango/sqlok/internal/sst/dql"
	"github.com/stretchr/testify/assert"
)

func TestCompileInsert(t *testing.T) {
	stmt := dml.Insert(
		sst.NewTableRef("users", sst.WithTableSchema("public")),
		sst.NewColumnRef("", "name"),
	).Values([]sst.ExpressionNode{sst.NewBindParam("ana")})

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "INSERT INTO public.users (name) VALUES (?)", sql)
	assert.Equal(t, []any{"ana"}, args)
}

func TestCompileUpdate(t *testing.T) {
	stmt := dml.Update(sst.NewTableRef("users")).
		Set(sst.NewColumnRef("", "name"), sst.NewBindParam("ana")).
		Where(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)))

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "UPDATE users SET name = ? WHERE users.id = ?", sql)
	assert.Equal(t, []any{"ana", 42}, args)
}

func TestCompileDelete(t *testing.T) {
	stmt := dml.Delete(sst.NewTableRef("users")).
		Where(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)))

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "DELETE FROM users WHERE users.id = ?", sql)
	assert.Equal(t, []any{42}, args)
}

func TestCompileSelectRejectsEmptyProjection(t *testing.T) {
	stmt := dql.Select().From(sst.NewTableRef("users"))

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "SELECT requires at least one expression")
}

func TestCompileSelectDistinct(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "name"),
	).From(
		sst.NewTableRef("users"),
	).Distinct()

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT DISTINCT users.name FROM users", sql)
	assert.Empty(t, args)
}

func TestCompileSelectGroupBy(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).GroupBy(
		sst.NewColumnRef("users", "id"),
		sst.NewColumnRef("users", "name"),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users GROUP BY users.id, users.name", sql)
	assert.Empty(t, args)
}

func TestCompileSelectHaving(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).GroupBy(
		sst.NewColumnRef("users", "id"),
	).Having(
		sst.Gt(sst.NewColumnRef("users", "id"), sst.NewBindParam(1)),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users GROUP BY users.id HAVING users.id > ?", sql)
	assert.Equal(t, []any{1}, args)
}

func TestCompileSelectWithColumnRef(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id", sst.WithColumnSchema("public")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT public.users.id", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithMultipleColumnRefs(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
		sst.NewColumnRef("users", "name"),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id, users.name", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithFromSource(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users", sst.WithTableSchema("public")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM public.users", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithWhere(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(42),
		),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users WHERE users.id = ?", sql)
	assert.Equal(t, []any{42}, args)
}

func TestCompileSelectWithRepeatedWhere(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(42),
		),
	).Where(
		sst.Eq(
			sst.NewColumnRef("users", "active"),
			sst.NewBindParam(true),
		),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users WHERE users.id = ? AND users.active = ?", sql)
	assert.Equal(t, []any{42, true}, args)
}

func TestCompileSelectWithLogicalWhere(t *testing.T) {
	tests := []struct {
		name      string
		condition sst.ExpressionNode
		expected  string
		args      []any
	}{
		{
			name: "and accepts multiple operands",
			condition: sst.And(
				sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
				sst.Eq(sst.NewColumnRef("users", "active"), sst.NewBindParam(true)),
				sst.Eq(sst.NewColumnRef("users", "role"), sst.NewBindParam("admin")),
			),
			expected: "SELECT users.id FROM users WHERE users.id = ? AND users.active = ? AND users.role = ?",
			args:     []any{42, true, "admin"},
		},
		{
			name: "or accepts multiple operands",
			condition: sst.Or(
				sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
				sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(7)),
				sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(99)),
			),
			expected: "SELECT users.id FROM users WHERE users.id = ? OR users.id = ? OR users.id = ?",
			args:     []any{42, 7, 99},
		},
		{
			name: "and groups lower precedence or",
			condition: sst.And(
				sst.Or(
					sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
					sst.Eq(sst.NewColumnRef("users", "name"), sst.NewBindParam("sandy")),
				),
				sst.Eq(sst.NewColumnRef("users", "active"), sst.NewBindParam(true)),
			),
			expected: "SELECT users.id FROM users WHERE (users.id = ? OR users.name = ?) AND users.active = ?",
			args:     []any{42, "sandy", true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt := dql.Select(
				sst.NewColumnRef("users", "id"),
			).From(
				sst.NewTableRef("users"),
			).Where(tt.condition)

			sql, args, err := Compile(stmt)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, sql)
			assert.Equal(t, tt.args, args)
		})
	}
}

func TestCompileSelectRejectsEmptyLogicalExpression(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).Where(sst.And())

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "AND requires at least one expression")
}

func TestCompileSelectWithNot(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Where(
		sst.Not(sst.Or(
			sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
			sst.Eq(sst.NewColumnRef("users", "name"), sst.NewBindParam("sandy")),
		)),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users WHERE NOT (users.id = ? OR users.name = ?)", sql)
	assert.Equal(t, []any{42, "sandy"}, args)
}

func TestCompileSelectWithOrderBy(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).OrderBy(
		sst.Asc(sst.NewColumnRef("users", "name")),
		sst.Desc(sst.NewColumnRef("users", "id")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users ORDER BY users.name ASC, users.id DESC", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithEmptyOrderBy(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).OrderBy()

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithClearedOrderBy(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).OrderBy(
		sst.Desc(sst.NewColumnRef("users", "name")),
	).ClearOrderBy()

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users", sql)
	assert.Empty(t, args)
}

func TestCompileSelectOrderByDefaultsToAscending(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).OrderBy(
		sst.NewOrderItem(sst.NewColumnRef("users", "name")),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users ORDER BY users.name ASC", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithLimit(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Limit(10)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users LIMIT 10", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithRepeatedLimitUsesLastValue(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Limit(10).
		Limit(20)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users LIMIT 20", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithZeroLimit(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Limit(0)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users LIMIT 0", sql)
	assert.Empty(t, args)
}

func TestCompileSelectRejectsNegativeLimit(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).Limit(-1)

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "LIMIT cannot be negative")
}

func TestCompileSelectWithOffset(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Offset(10)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users OFFSET 10", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithRepeatedOffsetUsesLastValue(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Offset(10).
		Offset(20)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users OFFSET 20", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithZeroOffset(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).Offset(0)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users OFFSET 0", sql)
	assert.Empty(t, args)
}

func TestCompileSelectRejectsNegativeOffset(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).Offset(-1)

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "OFFSET cannot be negative")
}

func TestCompileSelectWithFullOuterJoin(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).From(
		sst.NewTableRef("users"),
	).FullJoin(
		sst.NewTableRef("orders"),
	).On(
		sst.Eq(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("orders", "user_id"),
		),
	)

	sql, args, err := Compile(stmt)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT users.id FROM users FULL OUTER JOIN orders ON users.id = orders.user_id", sql)
	assert.Empty(t, args)
}

func TestCompileSelectWithFromAndJoin(t *testing.T) {

	t.Run("should have from and join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders"))

		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id, items.name FROM users JOIN orders", sql)
		assert.Empty(t, args)
	})

	t.Run("should have an explicit inner join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
		).
			From(sst.NewTableRef("users")).
			InnerJoin(sst.NewTableRef("orders")).
			On(sst.Eq(
				sst.NewColumnRef("users", "id"),
				sst.NewColumnRef("orders", "user_id"),
			))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id FROM users INNER JOIN orders ON users.id = orders.user_id", sql)
		assert.Empty(t, args)
	})

	t.Run("should have an explicit cross join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
		).
			From(sst.NewTableRef("users")).
			CrossJoin(sst.NewTableRef("orders"))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id FROM users CROSS JOIN orders", sql)
		assert.Empty(t, args)
	})

	t.Run("should have an explicit left join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
		).
			From(sst.NewTableRef("users")).
			LeftJoin(sst.NewTableRef("orders")).
			On(sst.Eq(
				sst.NewColumnRef("users", "id"),
				sst.NewColumnRef("orders", "user_id"),
			))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id FROM users LEFT JOIN orders ON users.id = orders.user_id", sql)
		assert.Empty(t, args)
	})

	t.Run("should have an explicit right join", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
		).
			From(sst.NewTableRef("users")).
			RightJoin(sst.NewTableRef("orders")).
			On(sst.Eq(
				sst.NewColumnRef("users", "id"),
				sst.NewColumnRef("orders", "user_id"),
			))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id FROM users RIGHT JOIN orders ON users.id = orders.user_id", sql)
		assert.Empty(t, args)
	})

	t.Run("should have from, join chain with on clauses", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewColumnRef("orders", "user_id"))).
			Join(sst.NewTableRef("items")).
			On(sst.Eq(sst.NewColumnRef("orders", "id"), sst.NewColumnRef("items", "order_id")))
		sql, args, err := Compile(stmt)

		expected := "SELECT users.id, items.name " +
			"FROM users " +
			"JOIN orders ON users.id = orders.user_id " +
			"JOIN items ON orders.id = items.order_id"

		assert.NoError(t, err)
		assert.Equal(t, expected, sql)
		assert.Empty(t, args)
	})

	t.Run("should have from, join with on clause and literal", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewColumnRef("items", "name"),
		).
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewLiteral(1)))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id, items.name FROM users JOIN orders ON users.id = 1", sql)
		assert.Empty(t, args)
	})

	t.Run("should bind values be resolved in order", func(t *testing.T) {
		stmt := dql.Select(
			sst.NewColumnRef("users", "id"),
			sst.NewBindParam(42),
		).
			From(sst.NewTableRef("users")).
			Join(sst.NewTableRef("orders")).
			On(sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam("second")))
		sql, args, err := Compile(stmt)

		assert.NoError(t, err)
		assert.Equal(t, "SELECT users.id, ? FROM users JOIN orders ON users.id = ?", sql)
		assert.Equal(t, []any{42, "second"}, args)
	})
}

func TestCompileRejectSelectBuilderError(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).Join(sst.NewTableRef("order"))

	_, _, err := Compile(stmt)

	assert.EqualError(t, err, "JOIN requires a FROM source")
}
