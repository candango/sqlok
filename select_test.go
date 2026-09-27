package sqlok

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectMapsRowsAndReusesSessionIdentity(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(7), "Ana"}},
	})
	session := NewSession(db)
	query := Select(TestUser{}).Where(Eq("name", "Ana"))

	users, err := query.All(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, &TestUser{
		TestUserBase: TestUserBase{Id: 7},
		Name:         "Ana",
	}, users[0])
	assert.Contains(t, session.snapshots, users[0])

	queries := sessionTestQueries()
	require.Len(t, queries, 1)
	assert.Equal(
		t,
		"SELECT test_user.id, test_user.name FROM test_user WHERE test_user.name = ?",
		queries[0].query,
	)
	assert.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: "Ana"}}, queries[0].args)
	assert.Equal(t, 1, session.readPlans.Len())
	assert.Equal(t, 1, session.readCache.Len())

	setSessionTestResponse(sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(7), "Changed outside"}},
	})
	again, err := query.All(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Same(t, users[0], again[0])
	assert.Equal(t, "Ana", again[0].Name)
	assert.Len(t, sessionTestQueries(), 2)
	assert.Equal(t, 1, session.readPlans.Len())
	assert.Equal(t, 1, session.readCache.Len())

	setSessionTestResponse(sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(8), "Bia"}},
	})
	other, err := Select(TestUser{}).
		Where(Eq("name", "Bia")).
		All(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, "Bia", other[0].Name)
	assert.Equal(t, 1, session.readPlans.Len())
	assert.Equal(t, 1, session.readCache.Len())
	queries = sessionTestQueries()
	require.Len(t, queries, 3)
	assert.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: "Bia"}}, queries[2].args)
}

func TestSelectWhereIsGenerativeAndCombinesCriteria(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"org_id", "user_id", "name"},
		rows:    [][]driver.Value{{int64(7), int64(11), "Ana"}},
	})
	session := NewSession(db)
	base := Select(TestCompositeUser{})
	query := base.Where(Eq("org_id", 7)).Where(Eq("user_id", 11))

	entities, err := query.All(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, entities, 1)
	assert.Equal(t, &TestCompositeUser{OrgId: 7, UserId: 11, Name: "Ana"}, entities[0])

	queries := sessionTestQueries()
	require.Len(t, queries, 1)
	assert.Equal(
		t,
		"SELECT test_composite_user.org_id, test_composite_user.user_id, test_composite_user.name FROM test_composite_user WHERE test_composite_user.org_id = ? AND test_composite_user.user_id = ?",
		queries[0].query,
	)
	assert.Equal(t, []driver.NamedValue{
		{Ordinal: 1, Value: int64(7)},
		{Ordinal: 2, Value: int64(11)},
	}, queries[0].args)

	assert.Empty(t, base.criteria)
	baseEntities, err := base.All(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, baseEntities, 1)
	queries = sessionTestQueries()
	require.Len(t, queries, 2)
	assert.Equal(
		t,
		"SELECT test_composite_user.org_id, test_composite_user.user_id, test_composite_user.name FROM test_composite_user",
		queries[1].query,
	)
	assert.Empty(t, queries[1].args)
}

func TestSelectSupportsComparisonAndNullCriteria(t *testing.T) {
	tests := []struct {
		name        string
		criteria    []SelectCriterion
		expectedSQL string
		args        []driver.NamedValue
	}{
		{
			name:        "not equal",
			criteria:    []SelectCriterion{Ne("id", 7)},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id <> ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
		{
			name:        "greater than",
			criteria:    []SelectCriterion{Gt("id", 7)},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id > ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
		{
			name:        "greater than or equal",
			criteria:    []SelectCriterion{Gte("id", 7)},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id >= ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
		{
			name:        "less than",
			criteria:    []SelectCriterion{Lt("id", 7)},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id < ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
		{
			name:        "less than or equal",
			criteria:    []SelectCriterion{Lte("id", 7)},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id <= ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
		{
			name:        "is null",
			criteria:    []SelectCriterion{IsNull("name")},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.name IS NULL",
		},
		{
			name:        "is not null",
			criteria:    []SelectCriterion{IsNotNull("name")},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.name IS NOT NULL",
		},
		{
			name: "null and bound criteria",
			criteria: []SelectCriterion{
				IsNotNull("name"),
				Eq("id", 7),
			},
			expectedSQL: "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.name IS NOT NULL AND test_user.id = ?",
			args:        []driver.NamedValue{{Ordinal: 1, Value: int64(7)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newSessionTestDB(t, sessionTestResponse{
				columns: []string{"id", "name"},
				rows:    [][]driver.Value{{int64(7), "Ana"}},
			})
			session := NewSession(db)
			_, err := Select(TestUser{}).Where(tt.criteria...).All(context.Background(), session)
			require.NoError(t, err)

			queries := sessionTestQueries()
			require.Len(t, queries, 1)
			assert.Equal(t, tt.expectedSQL, queries[0].query)
			assert.Equal(t, tt.args, queries[0].args)
		})
	}
}

func TestSelectProjectionReturnsRowsInRequestedColumnOrder(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"name", "id"},
		rows:    [][]driver.Value{{"Ana", int64(7)}},
	})

	rows, err := Select(TestUser{}).
		Where(Eq("id", 7)).
		Columns("name", "id").
		All(context.Background(), NewSession(db))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"name", "id"}, rows[0].Columns())
	assert.Equal(t, []any{"Ana", int64(7)}, rows[0].Values())
	value, ok := rows[0].Value("name")
	assert.True(t, ok)
	assert.Equal(t, "Ana", value)
	_, ok = rows[0].Value("missing")
	assert.False(t, ok)

	queries := sessionTestQueries()
	require.Len(t, queries, 1)
	assert.Equal(t, "SELECT test_user.name, test_user.id FROM test_user WHERE test_user.id = ?", queries[0].query)
	assert.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: int64(7)}}, queries[0].args)
}

func TestSelectProjectionScalarsRequiresOneColumn(t *testing.T) {
	t.Run("returns scalar values", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"name"},
			rows:    [][]driver.Value{{"Ana"}, {"Bia"}},
		})

		values, err := Select(TestUser{}).
			Columns("name").
			Scalars(context.Background(), NewSession(db))
		require.NoError(t, err)
		assert.Equal(t, []any{"Ana", "Bia"}, values)

		queries := sessionTestQueries()
		require.Len(t, queries, 1)
		assert.Equal(t, "SELECT test_user.name FROM test_user", queries[0].query)
	})

	t.Run("rejects multiple columns", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
		})
		_, err := Select(TestUser{}).
			Columns("id", "name").
			Scalars(context.Background(), NewSession(db))
		assert.ErrorIs(t, err, ErrScalarSelectProjection)
		assert.Empty(t, sessionTestQueries())
	})
}

func TestSelectProjectionValidation(t *testing.T) {
	query := Select(TestUser{})
	_, err := query.Columns().All(context.Background(), NewSession(nil))
	assert.ErrorIs(t, err, ErrEmptySelectProjection)

	_, err = query.Columns("missing").All(context.Background(), NewSession(nil))
	assert.ErrorIs(t, err, ErrUnmappedSelectColumn)

	_, err = query.Columns(" ").All(context.Background(), NewSession(nil))
	assert.ErrorIs(t, err, ErrEmptySelectColumn)
}

func TestSelectPredicateOperatorsUseDistinctPreparedPlans(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(7), "Ana"}},
	})
	session := NewSession(db)

	_, err := Select(TestUser{}).Where(Eq("id", 7)).All(context.Background(), session)
	require.NoError(t, err)

	setSessionTestResponse(sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(7), "Ana"}},
	})
	_, err = Select(TestUser{}).Where(Gt("id", 7)).All(context.Background(), session)
	require.NoError(t, err)

	assert.Equal(t, 2, session.readPlans.Len())
	queries := sessionTestQueries()
	require.Len(t, queries, 2)
	assert.Equal(t, "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id = ?", queries[0].query)
	assert.Equal(t, "SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id > ?", queries[1].query)
}

func TestSelectOneOrNoneEnforcesCardinality(t *testing.T) {
	t.Run("one row", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(7), "Ana"}},
		})
		entity, err := Select(TestUser{}).
			Where(Eq("id", 7)).
			OneOrNone(context.Background(), NewSession(db))
		require.NoError(t, err)
		assert.Equal(t, &TestUser{TestUserBase: TestUserBase{Id: 7}, Name: "Ana"}, entity)
		queries := sessionTestQueries()
		require.Len(t, queries, 1)
		assert.Contains(t, queries[0].query, "LIMIT ?")
		assert.Equal(t, []driver.NamedValue{
			{Ordinal: 1, Value: int64(7)},
			{Ordinal: 2, Value: int64(2)},
		}, queries[0].args)
	})

	t.Run("no rows", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		entity, err := Select(TestUser{}).
			Where(Eq("id", 7)).
			OneOrNone(context.Background(), NewSession(db))
		require.NoError(t, err)
		assert.Nil(t, entity)
	})

	t.Run("multiple rows", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(7), "Ana"}, {int64(8), "Bia"}},
		})
		entity, err := Select(TestUser{}).
			Where(Eq("name", "shared")).
			OneOrNone(context.Background(), NewSession(db))
		assert.ErrorIs(t, err, ErrMultipleSelectRows)
		assert.Nil(t, entity)
	})

	t.Run("one requires a row", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		entity, err := Select(TestUser{}).One(context.Background(), NewSession(db))
		assert.ErrorIs(t, err, ErrNoSelectRows)
		assert.Nil(t, entity)
	})

	t.Run("zero integer primary key is present when loaded", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(0), "Zero"}},
		})
		session := NewSession(db)
		entity, err := Select(TestUser{}).One(context.Background(), session)
		require.NoError(t, err)
		assert.Zero(t, entity.Id)
		assert.Contains(t, session.snapshots, entity)
		assert.Empty(t, session.pending)
	})

	t.Run("selected zero-key entity can be updated", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(0), "Zero"}},
		})
		session := NewSession(db)
		entity, err := Select(TestUser{}).
			Where(Eq("id", 0)).
			One(context.Background(), session)
		require.NoError(t, err)
		entity.Name = "Updated"

		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		require.NoError(t, session.Flush(context.Background(), tx))
		execs := sessionTestExecs()
		require.Len(t, execs, 1)
		assert.Equal(t, "UPDATE test_user SET name = ? WHERE test_user.id = ?", execs[0].query)
		assert.Equal(t, []driver.NamedValue{
			{Ordinal: 1, Value: "Updated"},
			{Ordinal: 2, Value: int64(0)},
		}, execs[0].args)
	})

	t.Run("pointer primary key zero is present", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(0), "Zero"}},
		})
		entity, err := Select(TestPointerUser{}).
			Where(Eq("id", 0)).
			One(context.Background(), NewSession(db))
		require.NoError(t, err)
		require.NotNil(t, entity.Id)
		assert.Zero(t, *entity.Id)
	})
}

func TestSelectValidationMissingRowsAndErrors(t *testing.T) {
	t.Run("nil equality requires a NULL predicate", func(t *testing.T) {
		var value *string
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		_, err := Select(TestUser{}).
			Where(Eq("name", value)).
			All(context.Background(), NewSession(db))
		assert.ErrorIs(t, err, ErrNilSelectValue)
		assert.Empty(t, sessionTestQueries())
	})

	t.Run("unmapped column", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		session := NewSession(db)
		_, err := Select(TestUser{}).
			Where(Eq("missing", "Ana")).
			All(context.Background(), session)
		assert.ErrorIs(t, err, ErrUnmappedSelectColumn)
		assert.Empty(t, sessionTestQueries())
	})

	t.Run("empty result is non-nil", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		users, err := Select(TestUser{}).All(context.Background(), NewSession(db))
		require.NoError(t, err)
		assert.NotNil(t, users)
		assert.Empty(t, users)
	})

	t.Run("mapping failure leaves no identity state", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{"not-an-id", "Ana"}},
		})
		session := NewSession(db)
		users, err := Select(TestUser{}).All(context.Background(), session)
		assert.Error(t, err)
		assert.Nil(t, users)
		assert.Empty(t, session.identityMap)
		assert.Empty(t, session.snapshots)
	})

	t.Run("missing primary key mapping", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"name"},
			rows:    [][]driver.Value{{"Ana"}},
		})
		session := NewSession(db)
		users, err := Select(TestUnkeyedUser{}).All(context.Background(), session)
		assert.ErrorIs(t, err, ErrLoadedEntityWithoutPrimaryKey)
		assert.Nil(t, users)
		assert.Empty(t, session.identityMap)
		assert.Empty(t, session.snapshots)
		assert.Empty(t, session.pending)
	})

	t.Run("zero value query", func(t *testing.T) {
		var query SelectQuery[TestUser]
		_, err := query.All(context.Background(), NewSession(nil))
		assert.ErrorIs(t, err, ErrNilSelectQuery)
	})

	t.Run("nil inputs", func(t *testing.T) {
		query := Select(TestUser{})
		_, err := query.All(nil, NewSession(nil))
		assert.ErrorIs(t, err, ErrNilSelectContext)
		_, err = query.All(context.Background(), nil)
		assert.ErrorIs(t, err, ErrNilSession)
		_, err = query.All(context.Background(), NewSession(nil))
		assert.ErrorIs(t, err, ErrNilSessionDatabase)
	})
}
