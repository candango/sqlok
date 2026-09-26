package sqlok

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
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
	lastInsertID     int64
	lastInsertErr    error
	disableRecording bool
}

type sessionTestExec struct {
	query string
	args  []driver.NamedValue
}

type sessionTestResult struct {
	id  int64
	err error
}

func (r sessionTestResult) LastInsertId() (int64, error) {
	return r.id, r.err
}

func (r sessionTestResult) RowsAffected() (int64, error) {
	return 1, nil
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
	return sessionTestResult{
		id:  response.lastInsertID,
		err: response.lastInsertErr,
	}, nil
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

func TestSessionFlushWritesPendingAndDirtyEntities(t *testing.T) {
	t.Run("pending insert", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{lastInsertID: 7})
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
		assert.Equal(t, 7, user.Id)
		assert.Contains(t, session.snapshots, user)

		assert.Same(t, user, session.identityMap[reflect.TypeFor[TestUser]()][7])

		execs := sessionTestExecs()
		if !assert.Len(t, execs, 1) {
			return
		}
		assert.Equal(t, "INSERT INTO test_user (name) VALUES (?)", execs[0].query)
		assert.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: "Ana"}}, execs[0].args)
	})

	t.Run("generated key unsupported preserves pending state", func(t *testing.T) {
		db := newSessionTestDB(t, sessionTestResponse{
			lastInsertErr: errors.New("last insert id is unsupported"),
		})
		session := NewSession(db)
		user := &TestUser{Name: "Ana"}
		assert.NoError(t, session.Add(user))

		tx, err := db.BeginTx(context.Background(), nil)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		err = session.Flush(context.Background(), tx)
		assert.ErrorIs(t, err, ErrGeneratedKeyUnsupported)
		assert.Contains(t, session.pending, user)
		assert.Zero(t, user.Id)
		assert.Empty(t, session.identityMap)
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
