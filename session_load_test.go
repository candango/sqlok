package sqlok

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

const sessionTestDriverName = "sqlok-session-test"

type sessionTestBinaryUser struct {
	ID      int `sqlok:"pk"`
	Payload []byte
}

type sessionTestResponse struct {
	columns          []string
	rows             [][]driver.Value
	queryErr         error
	execErr          error
	disableRecording bool
}

type sessionTestExec struct {
	query string
	args  []driver.NamedValue
}

type sessionTestQuery struct {
	query string
	args  []driver.NamedValue
}

var sessionTestDatabase = struct {
	sync.Mutex
	response sessionTestResponse
	queries  []sessionTestQuery
	execs    []sessionTestExec
}{}

func init() {
	sql.Register(sessionTestDriverName, sessionTestDriver{})
}

type sessionTestDriver struct{}

func (sessionTestDriver) Open(string) (driver.Conn, error) {
	return sessionTestConn{}, nil
}

type sessionTestConn struct{}

func (sessionTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are unsupported in session tests")
}

func (sessionTestConn) Close() error {
	return nil
}

func (sessionTestConn) Begin() (driver.Tx, error) {
	return sessionTestTx{}, nil
}

func (sessionTestConn) BeginTx(
	context.Context,
	driver.TxOptions,
) (driver.Tx, error) {
	return sessionTestTx{}, nil
}

func (sessionTestConn) ExecContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Result, error) {
	sessionTestDatabase.Lock()
	response := sessionTestDatabase.response
	if !response.disableRecording {
		sessionTestDatabase.execs = append(sessionTestDatabase.execs, sessionTestExec{
			query: query,
			args:  append([]driver.NamedValue(nil), args...),
		})
	}
	sessionTestDatabase.Unlock()
	if response.execErr != nil {
		return nil, response.execErr
	}
	return driver.RowsAffected(1), nil
}

func (sessionTestConn) QueryContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	sessionTestDatabase.Lock()
	response := sessionTestDatabase.response
	if !response.disableRecording {
		sessionTestDatabase.queries = append(sessionTestDatabase.queries, sessionTestQuery{
			query: query,
			args:  append([]driver.NamedValue(nil), args...),
		})
	}
	sessionTestDatabase.Unlock()
	if response.queryErr != nil {
		return nil, response.queryErr
	}
	return &sessionTestRows{
		columns: append([]string(nil), response.columns...),
		rows:    cloneSessionTestRows(response.rows),
	}, nil
}

var (
	_ driver.ConnBeginTx    = sessionTestConn{}
	_ driver.ExecerContext  = sessionTestConn{}
	_ driver.QueryerContext = sessionTestConn{}
)

type sessionTestTx struct{}

func (sessionTestTx) Commit() error {
	return nil
}

func (sessionTestTx) Rollback() error {
	return nil
}

type sessionTestRows struct {
	columns  []string
	rows     [][]driver.Value
	position int
}

func (r *sessionTestRows) Columns() []string {
	return r.columns
}

func (r *sessionTestRows) Close() error {
	return nil
}

func (r *sessionTestRows) Next(dest []driver.Value) error {
	if r.position == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.position])
	r.position++
	return nil
}

func cloneSessionTestRows(rows [][]driver.Value) [][]driver.Value {
	clone := make([][]driver.Value, len(rows))
	for position, row := range rows {
		clone[position] = append([]driver.Value(nil), row...)
	}
	return clone
}

func newSessionTestDB(t *testing.T, response sessionTestResponse) *sql.DB {
	t.Helper()
	sessionTestDatabase.Lock()
	sessionTestDatabase.response = response
	sessionTestDatabase.queries = nil
	sessionTestDatabase.execs = nil
	sessionTestDatabase.Unlock()

	db, err := sql.Open(sessionTestDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setSessionTestResponse(response sessionTestResponse) {
	sessionTestDatabase.Lock()
	sessionTestDatabase.response = response
	sessionTestDatabase.Unlock()
}

func sessionTestQueries() []sessionTestQuery {
	sessionTestDatabase.Lock()
	defer sessionTestDatabase.Unlock()
	return append([]sessionTestQuery(nil), sessionTestDatabase.queries...)
}

func sessionTestExecs() []sessionTestExec {
	sessionTestDatabase.Lock()
	defer sessionTestDatabase.Unlock()
	return append([]sessionTestExec(nil), sessionTestDatabase.execs...)
}

func TestSessionLoadContextQueriesPreparedPlanAndCachesIdentity(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(7), "Ana"}},
	})
	session := NewSession(db)

	first, err := LoadContext[TestUser](context.Background(), session, 7)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, &TestUser{
		TestUserBase: TestUserBase{Id: 7},
		Name:         "Ana",
	}, first)
	assert.Contains(t, session.snapshots, first)
	assert.Len(t, session.snapshots[first], 2)
	assert.Contains(t, session.snapshots[first], "id")
	assert.Contains(t, session.snapshots[first], "name")

	queries := sessionTestQueries()
	if !assert.Len(t, queries, 1) {
		return
	}
	assert.Equal(
		t,
		"SELECT test_user.id, test_user.name FROM test_user WHERE test_user.id = ? LIMIT ?",
		queries[0].query,
	)
	assert.Equal(t, []driver.NamedValue{
		{Ordinal: 1, Value: int64(7)},
		{Ordinal: 2, Value: int64(1)},
	}, queries[0].args)
	assert.Equal(t, 1, session.loadPlans.Len())
	assert.Equal(t, 1, session.loadCache.Len())

	second, err := LoadContext[TestUser](context.Background(), session, 7)
	assert.NoError(t, err)
	assert.Same(t, first, second)
	assert.Len(t, sessionTestQueries(), 1)

	setSessionTestResponse(sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(8), "Bia"}},
	})
	third, err := LoadContext[TestUser](context.Background(), session, 8)
	assert.NoError(t, err)
	assert.Equal(t, "Bia", third.Name)
	assert.Len(t, sessionTestQueries(), 2)
	assert.Equal(t, 1, session.loadPlans.Len())
	assert.Equal(t, 1, session.loadCache.Len())
}

func TestSessionLoadContextHandlesMissingAndInvalidRows(t *testing.T) {
	t.Run("missing row", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{columns: []string{"id", "name"}})
		loaded, err := LoadContext[TestUser](context.Background(), NewSession(db), 7)
		assert.NoError(t, err)
		assert.Nil(t, loaded)
	})

	t.Run("mismatched identity", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			columns: []string{"id", "name"},
			rows:    [][]driver.Value{{int64(8), "Bia"}},
		})
		session := NewSession(db)
		loaded, err := LoadContext[TestUser](context.Background(), session, 7)
		assert.Error(t, err)
		assert.Nil(t, loaded)
		assert.Empty(t, session.identityMap)
	})

	t.Run("nil context", func(t *testing.T) {
		loaded, err := LoadContext[TestUser](nil, NewSession(nil), 7)
		assert.ErrorIs(t, err, ErrNilLoadContext)
		assert.Nil(t, loaded)
	})
}

func TestSessionLoadContextReusesPointerIdentity(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{int64(0), "Zero"}},
	})
	session := NewSession(db)

	first, err := LoadContext[TestPointerUser](context.Background(), session, 0)
	if !assert.NoError(t, err) {
		return
	}
	if !assert.NotNil(t, first.Id) {
		return
	}
	assert.Zero(t, *first.Id)

	second, err := LoadContext[TestPointerUser](context.Background(), session, 0)
	assert.NoError(t, err)
	assert.Same(t, first, second)
	assert.Len(t, sessionTestQueries(), 1)
}

func TestSessionLoadContextRejectsMappingFailureWithoutState(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"id", "name"},
		rows:    [][]driver.Value{{"not-an-id", "Ana"}},
	})
	session := NewSession(db)

	loaded, err := LoadContext[TestUser](context.Background(), session, 7)
	assert.Error(t, err)
	assert.Nil(t, loaded)
	assert.Empty(t, session.identityMap)
	assert.Empty(t, session.snapshots)
}

func TestSessionFlushWritesPendingAndDirtyEntities(t *testing.T) {
	t.Run("pending insert", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{})
		session := NewSession(db)
		user := &TestUser{Name: "Ana"}
		assert.NoError(t, session.Add(user))

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		assert.NoError(t, session.Flush(context.Background(), tx))
		assert.Empty(t, session.pending)
		assert.Contains(t, session.snapshots, user)

		execs := sessionTestExecs()
		if !assert.Len(t, execs, 1) {
			return
		}
		assert.Equal(t, "INSERT INTO test_user (name) VALUES (?)", execs[0].query)
		assert.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: "Ana"}}, execs[0].args)
	})

	t.Run("dirty update", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{})
		session := NewSession(db)
		user := &TestUser{TestUserBase: TestUserBase{Id: 7}, Name: "Ana"}
		assert.NoError(t, session.Add(user))
		user.Name = "Bia"

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		assert.NoError(t, session.Flush(context.Background(), tx))

		execs := sessionTestExecs()
		if !assert.Len(t, execs, 1) {
			return
		}
		assert.Equal(
			t,
			"UPDATE test_user SET name = ? WHERE test_user.id = ?",
			execs[0].query,
		)
		assert.Equal(t, []driver.NamedValue{
			{Ordinal: 1, Value: "Bia"},
			{Ordinal: 2, Value: int64(7)},
		}, execs[0].args)

		assert.NoError(t, session.Flush(context.Background(), tx))
		assert.Len(t, sessionTestExecs(), 1)
	})

	t.Run("mutable mapped value", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{})
		session := NewSession(db)
		user := &sessionTestBinaryUser{ID: 7, Payload: []byte{1}}
		assert.NoError(t, session.Add(user))
		user.Payload[0] = 2

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		assert.NoError(t, session.Flush(context.Background(), tx))

		execs := sessionTestExecs()
		if !assert.Len(t, execs, 1) {
			return
		}
		assert.Equal(
			t,
			"UPDATE session_test_binary_user SET payload = ? WHERE session_test_binary_user.id = ?",
			execs[0].query,
		)
		assert.Equal(t, []driver.NamedValue{
			{Ordinal: 1, Value: []byte{2}},
			{Ordinal: 2, Value: int64(7)},
		}, execs[0].args)
	})

	t.Run("write failure preserves Session state", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			execErr: errors.New("write failed"),
		})
		session := NewSession(db)
		pending := &TestUser{Name: "Ana"}
		assert.NoError(t, session.Add(pending))

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		assert.Error(t, session.Flush(context.Background(), tx))
		assert.Contains(t, session.pending, pending)
		assert.NotContains(t, session.snapshots, pending)
		assert.NoError(t, tx.Rollback())

		setSessionTestResponse(sessionTestResponse{})
		nextTx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = nextTx.Rollback() })
		assert.NoError(t, session.Flush(context.Background(), nextTx))
		assert.Empty(t, session.pending)
	})

	t.Run("primary key mutation", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{})
		session := NewSession(db)
		user := &TestUser{TestUserBase: TestUserBase{Id: 7}, Name: "Ana"}
		assert.NoError(t, session.Add(user))
		user.Id = 8

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		assert.ErrorIs(t, session.Flush(context.Background(), tx), ErrPrimaryKeyMutation)
		assert.Empty(t, sessionTestExecs())
	})

	t.Run("explicit transaction and context", func(t *testing.T) {
		session := NewSession(nil)
		assert.ErrorIs(t, session.Flush(nil, nil), ErrNilFlushContext)
		assert.ErrorIs(t, session.Flush(context.Background(), nil), ErrNilFlushTransaction)
	})
}

func TestSessionLoadContextBindsCompositeKeyInDeclarationOrder(t *testing.T) {
	db := newSessionTestDB(t, sessionTestResponse{
		columns: []string{"org_id", "user_id", "name"},
		rows:    [][]driver.Value{{int64(1), int64(200), "Ana"}},
	})
	session := NewSession(db)

	loaded, err := LoadContext[TestCompositeUser](
		context.Background(),
		session,
		CompositeKey{1, 200},
	)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, &TestCompositeUser{OrgId: 1, UserId: 200, Name: "Ana"}, loaded)

	queries := sessionTestQueries()
	if !assert.Len(t, queries, 1) {
		return
	}
	assert.Equal(
		t,
		"SELECT test_composite_user.org_id, test_composite_user.user_id, test_composite_user.name FROM test_composite_user WHERE test_composite_user.org_id = ? AND test_composite_user.user_id = ? LIMIT ?",
		queries[0].query,
	)
	assert.Equal(t, []driver.NamedValue{
		{Ordinal: 1, Value: int64(1)},
		{Ordinal: 2, Value: int64(200)},
		{Ordinal: 3, Value: int64(1)},
	}, queries[0].args)

	second, err := LoadContext[TestCompositeUser](
		context.Background(),
		session,
		CompositeKey{1, 200},
	)
	assert.NoError(t, err)
	assert.Same(t, loaded, second)
	assert.Len(t, sessionTestQueries(), 1)
}
