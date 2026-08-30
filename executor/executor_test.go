package executor

import (
	"context"
	"database/sql"
	"testing"

	"github.com/candango/sqlok/compiler"
	"github.com/candango/sqlok/sst"
	"github.com/candango/sqlok/sst/dql"
	"github.com/stretchr/testify/assert"
)

type recordingExecutor struct {
	execSQL    string
	execArgs   []any
	execCalls  int
	querySQL   string
	queryArgs  []any
	queryCalls int
}

func (e *recordingExecutor) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	e.execSQL = query
	e.execArgs = append([]any(nil), args...)
	e.execCalls++
	return nil, nil
}

func (e *recordingExecutor) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	e.querySQL = query
	e.queryArgs = append([]any(nil), args...)
	e.queryCalls++
	return nil, nil
}

func TestExecDogfoodsPreparedPlan(t *testing.T) {
	stmt := dql.Select(
		sst.NewColumnRef("users", "id"),
	).Where(
		sst.Eq(sst.NewColumnRef("users", "id"), sst.NewBindParam(42)),
	)
	plan, err := compiler.CompileShape(stmt)
	assert.NoError(t, err)

	target := &recordingExecutor{}
	_, err = Exec(context.Background(), target, plan, []any{7})

	assert.NoError(t, err)
	assert.Equal(t, 1, target.execCalls)
	assert.Equal(t, "SELECT users.id WHERE users.id = ?", target.execSQL)
	assert.Equal(t, []any{7}, target.execArgs)
}

func TestQueryDogfoodsPreparedPlan(t *testing.T) {
	plan, err := compiler.CompileShape(dql.Select(sst.NewLiteral(1)))
	assert.NoError(t, err)

	target := &recordingExecutor{}
	rows, err := Query(context.Background(), target, plan, nil)

	assert.NoError(t, err)
	assert.Nil(t, rows)
	assert.Equal(t, 1, target.queryCalls)
	assert.Equal(t, "SELECT 1", target.querySQL)
	assert.Empty(t, target.queryArgs)
}

func TestExecRejectsArgumentsBeforeCallingTarget(t *testing.T) {
	plan, err := compiler.CompileShape(dql.Select(sst.NewBindParam(42)))
	assert.NoError(t, err)

	target := &recordingExecutor{}
	_, err = Exec(context.Background(), target, plan, nil)

	assert.EqualError(t, err, "bind compiled statement: compiled statement expects 1 arguments, got 0")
	assert.Zero(t, target.execCalls)
}

func TestExecRejectsNilTarget(t *testing.T) {
	plan, err := compiler.CompileShape(dql.Select(sst.NewLiteral(1)))
	assert.NoError(t, err)

	_, err = Exec(context.Background(), nil, plan, nil)

	assert.ErrorIs(t, err, ErrNilExecutor)
}
