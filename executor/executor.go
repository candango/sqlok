// Package executor defines the driver-agnostic execution boundary for
// compiled SQL plans.
package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/candango/sqlok/compiler"
)

// Executor is the database operation contract implemented by database/sql
// handles such as *sql.DB, *sql.Tx, and *sql.Conn.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ErrNilExecutor reports an execution attempt without a database handle.
var ErrNilExecutor = errors.New("executor cannot be nil")

// Exec binds current values to a prepared SQL shape and executes it.
func Exec(
	ctx context.Context,
	target Executor,
	plan compiler.CompiledStatement,
	args []any,
) (sql.Result, error) {
	if target == nil {
		return nil, ErrNilExecutor
	}

	boundArgs, err := plan.Bind(args)
	if err != nil {
		return nil, fmt.Errorf("bind compiled statement: %w", err)
	}
	return target.ExecContext(ctx, plan.SQL(), boundArgs...)
}

// Query binds current values to a prepared SQL shape and queries it.
func Query(
	ctx context.Context,
	target Executor,
	plan compiler.CompiledStatement,
	args []any,
) (*sql.Rows, error) {
	if target == nil {
		return nil, ErrNilExecutor
	}

	boundArgs, err := plan.Bind(args)
	if err != nil {
		return nil, fmt.Errorf("bind compiled statement: %w", err)
	}
	return target.QueryContext(ctx, plan.SQL(), boundArgs...)
}
