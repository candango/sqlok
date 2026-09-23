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

type sessionTestResponse struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

type sessionTestQuery struct {
	query string
	args  []driver.NamedValue
}

var sessionTestDatabase = struct {
	sync.Mutex
	response sessionTestResponse
	queries  []sessionTestQuery
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
	return nil, errors.New("transactions are unsupported in session tests")
}

func (sessionTestConn) QueryContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	sessionTestDatabase.Lock()
	response := sessionTestDatabase.response
	sessionTestDatabase.queries = append(sessionTestDatabase.queries, sessionTestQuery{
		query: query,
		args:  append([]driver.NamedValue(nil), args...),
	})
	sessionTestDatabase.Unlock()
	if response.err != nil {
		return nil, response.err
	}
	return &sessionTestRows{
		columns: append([]string(nil), response.columns...),
		rows:    cloneSessionTestRows(response.rows),
	}, nil
}

var _ driver.QueryerContext = sessionTestConn{}

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
}
