package sqlok

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

type JoinType string

const (
	InnerJoin JoinType = "INNER JOIN"
	Join      JoinType = "JOIN"
	LeftJoin  JoinType = "LEFT JOIN"
	OuterJoin JoinType = "OUTER JOIN"
	RightJoin JoinType = "RIGHT JOIN"
)

type ConditionType string

const (
	AndCondition ConditionType = "AND"
	OrCondition  ConditionType = "OR"
)

func And(condition ...string) string {
	return fmt.Sprintf("%s %s", AndCondition, strings.Join(condition, " "))
}

func Or(condition ...string) string {
	return fmt.Sprintf("%s %s", OrCondition, strings.Join(condition, " "))
}

// QueryBuilder is an interface for building SQL queries.
// It provides a method to construct the SQL statement and its parameters.
type QueryBuilder interface {

	// Build constructs and returns the SQL query string along with its
	// arguments.
	// It does not execute the query but prepares it for execution.
	Build() (string, []any)
}

// DQLExecutor is an interface for executing Data Query Language (DQL)
// operations,
// specifically SELECT statements, which return data sets.
type DQLExecutor interface {

	// Execute runs the constructed SQL query to fetch data from the database.
	// It expects the query to return rows of data.
	Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error)
}

// DMLExecutor is an interface for executing Data Manipulation Language (DML)
// operations,
// such as INSERT, UPDATE, or DELETE, which modify data but do not return rows.
type DMLExecutor interface {

	// ExecuteDML executes a DML query that modifies data in the database.
	// It does not return rows but provides information about the operation's
	// outcome.
	Execute(ctx context.Context, db *sql.DB) (sql.Result, error)
}

type queryBuilder struct {
	query string
	args  []any
}

type SelectBuilder interface {
	QueryBuilder
	DQLExecutor
	Select(columns ...string) SelectBuilder
	From(table string) SelectBuilder
	Where(condition string, args ...any) SelectBuilder
	And(condition string, args ...any) SelectBuilder
	Or(condition string, args ...any) SelectBuilder
	OrderBy(columns ...string) SelectBuilder
	Limit(limit int) SelectBuilder
	Offset(limit int) SelectBuilder
	Join(joinType JoinType, table string, on string) SelectBuilder
}

type selectBuilder struct {
	selectColumns []string
	fromTable     string
	where         []string
	whereArgs     []any
	orderBy       []string
	limit         int
	offset        int
	joins         []joinInfo
}

type joinInfo struct {
	joinType JoinType
	table    string
	// TODO: the on string should be revewied
	on string
}

func NewSelectBuiler() SelectBuilder {
	b := &selectBuilder{}
	b.Clear()
	return b
}

func (b *selectBuilder) Clear() SelectBuilder {
	b.selectColumns = []string{}
	b.fromTable = ""
	b.where = []string{}
	b.whereArgs = []any{}
	b.orderBy = []string{}
	b.limit = 0
	b.offset = 0
	b.joins = []joinInfo{}
	return b
}

func (b *selectBuilder) Select(columns ...string) SelectBuilder {
	b.selectColumns = append(b.selectColumns, columns...)
	return b
}

func (b *selectBuilder) From(table string) SelectBuilder {
	b.fromTable = table
	return b
}

func (b *selectBuilder) Where(condition string, args ...any) SelectBuilder {
	b.where = append(b.where, condition)
	b.whereArgs = append(b.whereArgs, args...)
	return b
}

func (b *selectBuilder) And(condition string, args ...any) SelectBuilder {
	return b.Where(And(condition), args...)
}

func (b *selectBuilder) Or(condition string, args ...any) SelectBuilder {
	return b.Where(Or(condition), args...)
}

func (b *selectBuilder) OrderBy(columns ...string) SelectBuilder {
	b.orderBy = append(b.orderBy, columns...)
	return b
}

func (b *selectBuilder) Limit(limit int) SelectBuilder {
	b.limit = limit
	return b
}

func (b *selectBuilder) Offset(offset int) SelectBuilder {
	b.offset = offset
	return b
}

func (b *selectBuilder) Join(joinType JoinType, table string, on string) SelectBuilder {
	b.joins = append(b.joins, joinInfo{joinType, table, on})
	return b
}

func (b *selectBuilder) Build() (string, []any) {
	var sb strings.Builder

	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(b.selectColumns, ", "))

	sb.WriteString(" FROM ")
	sb.WriteString(b.fromTable)

	for _, join := range b.joins {
		sb.WriteString(fmt.Sprintf(" %s %s ON %s ", join.joinType, join.table, join.on))
	}

	// TODO: Implement the AND/OR properly
	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " "))
	}

	if len(b.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(strings.Join(b.orderBy, ", "))
	}

	if b.limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d ", b.limit))
	}

	if b.offset > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d ", b.offset))
	}
	args := b.whereArgs
	b.Clear()
	return sb.String(), args
}

func (b *selectBuilder) Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	query, args := b.Build()
	log.Info("executing query: ", query, "  with args: ", args)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %v", err)
	}
	return rows, err
}

type InsertBuilder interface {
	QueryBuilder
	DMLExecutor
	InsertInto(table string) InsertBuilder
	Columns(columns ...string) InsertBuilder
	Values(values ...any) InsertBuilder
}

type insertBuilder struct {
	table   string
	columns []string
	values  [][]any
	args    []any
}

func NewInsertBuiler() InsertBuilder {
	b := &insertBuilder{}
	b.Clear()
	return b
}

func (b *insertBuilder) InsertInto(table string) InsertBuilder {
	b.table = table
	return b
}

func (b *insertBuilder) Columns(columns ...string) InsertBuilder {
	b.columns = columns
	return b
}

func (b *insertBuilder) Values(values ...any) InsertBuilder {
	b.values = append(b.values, values)
	return b
}

func (b *insertBuilder) Clear() InsertBuilder {
	b.table = ""
	b.columns = []string{}
	b.values = [][]any{}
	return b
}

func (b *insertBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(b.table)

	if len(b.columns) > 0 {
		sb.WriteString(" (")
		sb.WriteString(strings.Join(b.columns, ", "))
		sb.WriteString(")")
	}

	sb.WriteString(" VALUES")
	var allArgs []any
	var valuesPlaceholders []string
	for i, valueSet := range b.values {
		if i > 0 {
			sb.WriteString(", ")
		}
		placeholders := make([]string, len(valueSet))
		for j := range valueSet {
			// TODO: WHAT!!!!????
			placeholders[j] = fmt.Sprintf("$%v", j+1)
			allArgs = append(allArgs, valueSet[j])
		}
		valuesPlaceholders = append(valuesPlaceholders, "("+strings.Join(placeholders, ", ")+")")
	}

	sb.WriteString(strings.Join(valuesPlaceholders, ", "))

	sb.WriteString(" RETURNING id")

	args := allArgs
	b.Clear()

	return sb.String(), args
}

func (b *insertBuilder) Execute(ctx context.Context, db *sql.DB) (sql.Result, error) {
	query, args := b.Build()
	log.Info("executing INSERT query: ", query, "  with args: ", args)
	if strings.Contains(query, "RETURNING id") {
		lid := int64(0)
		ra := int64(0)
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query execution failed: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			err := rows.Scan(&id)
			if err != nil {
				return nil, fmt.Errorf("failed reading row id after the insert operation: %v", err)
			}
			lid = id
			ra++
		}
		res := sqlokResult{0, 0}
		if ra == 0 {
			return res, nil
		}
		res = sqlokResult{lid, ra}
		return res, nil
	}
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %v", err)
	}
	return res, err
}

// TODO: avoid per-drive implementation, let's do the inversion of control
// for that
type sqlokResult struct {
	id int64
	ra int64
}

func (r sqlokResult) LastInsertId() (int64, error) {
	return r.id, nil
}

func (r sqlokResult) RowsAffected() (int64, error) {
	return r.ra, nil
}

type UpdateBuilder interface {
	QueryBuilder
	DMLExecutor
	Update(table string) UpdateBuilder
	Set(column string, value any) UpdateBuilder
	Where(condition string, args ...any) UpdateBuilder
	And(condition string, args ...any) UpdateBuilder
	Or(condition string, args ...any) UpdateBuilder
}

type updateBuilder struct {
	table string
	set   []string
	where []string
	args  []any
}

func NewUpdateBuilder() UpdateBuilder {
	b := &updateBuilder{}
	b.Clear()
	return b
}

func (b *updateBuilder) Update(table string) UpdateBuilder {
	b.table = table
	return b
}

func (b *updateBuilder) Set(column string, value any) UpdateBuilder {
	b.set = append(b.set, fmt.Sprintf("%s = $%d", column, len(b.args)+1))
	b.args = append(b.args, value)
	return b
}

func (b *updateBuilder) Where(condition string, args ...any) UpdateBuilder {
	b.where = append(b.where, condition)
	b.args = append(b.args, args...)
	return b
}

func (b *updateBuilder) And(condition string, args ...any) UpdateBuilder {
	return b.Where(And(condition), args...)
}

func (b *updateBuilder) Or(condition string, args ...any) UpdateBuilder {
	return b.Where(Or(condition), args...)
}

func (b *updateBuilder) Clear() UpdateBuilder {
	b.table = ""
	b.set = []string{}
	b.where = []string{}
	b.args = []any{}
	return b
}

func (b *updateBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString("UPDATE ")
	sb.WriteString(b.table)

	sb.WriteString(" SET ")
	sb.WriteString(strings.Join(b.set, ", "))

	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " "))
	}

	args := b.args
	b.Clear()

	return sb.String(), args
}

func (b *updateBuilder) Execute(ctx context.Context, db *sql.DB) (sql.Result, error) {
	query, args := b.Build()
	log.Info("executing UPDATE query: ", query, "  with args: ", args)
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %v", err)
	}
	return res, err
}

type DeleteBuilder interface {
	QueryBuilder
	DMLExecutor
	Delete(table string) DeleteBuilder
	Where(condition string, args ...any) DeleteBuilder
	And(condition string, args ...any) DeleteBuilder
	Or(condition string, args ...any) DeleteBuilder
}

type deleteBuilder struct {
	table string
	where []string
	args  []any
}

func NewDeleteBuiler() DeleteBuilder {
	b := &deleteBuilder{}
	b.Clear()
	return b
}

func (b *deleteBuilder) Delete(table string) DeleteBuilder {
	b.table = table
	return b
}

func (b *deleteBuilder) Where(condition string, args ...any) DeleteBuilder {
	b.where = append(b.where, condition)
	b.args = append(b.args, args...)
	return b
}

func (b *deleteBuilder) And(condition string, args ...any) DeleteBuilder {
	return b.Where(And(condition), args...)
}

func (b *deleteBuilder) Or(condition string, args ...any) DeleteBuilder {
	return b.Where(Or(condition), args...)
}

func (b *deleteBuilder) Clear() DeleteBuilder {
	b.table = ""
	b.where = []string{}
	b.args = []any{}
	return b
}

func (b *deleteBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString("DELETE FROM ")
	sb.WriteString(b.table)

	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " "))
	}

	args := b.args
	b.Clear()

	return sb.String(), args
}

func (b *deleteBuilder) Execute(ctx context.Context, db *sql.DB) (sql.Result, error) {
	query, args := b.Build()
	log.Info("executing DELETE query: ", query, "  with args: ", args)
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %v", err)
	}
	return res, err
}
