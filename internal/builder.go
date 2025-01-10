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

type QueryBuilder interface {
	Build() (string, []any)
	Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error)
}

type queryBuilder struct {
	query string
	args  []any
}

func NewQueryBuilder() QueryBuilder {
	return &queryBuilder{}
}

func (b *queryBuilder) Build() (string, []any) {
	return b.query, b.args
}

func (b *queryBuilder) Execute(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	query, args := b.Build()
	log.Info("executing query: ", query, "  with args: ", args)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %v", err)
	}
	return rows, err
}

type SelectBuilder interface {
	QueryBuilder
	Select(columns ...string) SelectBuilder
	From(table string) SelectBuilder
	Where(condition string, args ...interface{}) SelectBuilder
	And(condition string, args ...interface{}) SelectBuilder
	Or(condition string, args ...interface{}) SelectBuilder
	OrderBy(columns ...string) SelectBuilder
	Limit(limit int) SelectBuilder
	Offset(limit int) SelectBuilder
	Join(joinType JoinType, table string, on string) SelectBuilder
}

type selectBuilder struct {
	QueryBuilder
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
	b := &selectBuilder{
		QueryBuilder: NewQueryBuilder(),
	}
	b.Clear()
	return b
}

func (q *selectBuilder) Clear() SelectBuilder {
	q.selectColumns = []string{}
	q.fromTable = ""
	q.where = []string{}
	q.whereArgs = []any{}
	q.orderBy = []string{}
	q.limit = 0
	q.offset = 0
	q.joins = []joinInfo{}
	return q
}

func (q *selectBuilder) Select(columns ...string) SelectBuilder {
	q.selectColumns = append(q.selectColumns, columns...)
	return q
}

func (q *selectBuilder) From(table string) SelectBuilder {
	q.fromTable = table
	return q
}

func (q *selectBuilder) Where(condition string, args ...any) SelectBuilder {
	q.where = append(q.where, condition)
	q.whereArgs = append(q.whereArgs, args...)
	return q
}

func (q *selectBuilder) And(condition string, args ...any) SelectBuilder {
	return q.Where(condition, args...)
}

func (q *selectBuilder) Or(condition string, args ...any) SelectBuilder {
	return q.Where(condition, args...)
}

func (q *selectBuilder) OrderBy(columns ...string) SelectBuilder {
	q.orderBy = append(q.orderBy, columns...)
	return q
}

func (q *selectBuilder) Limit(limit int) SelectBuilder {
	q.limit = limit
	return q
}

func (q *selectBuilder) Offset(offset int) SelectBuilder {
	q.offset = offset
	return q
}

func (q *selectBuilder) Join(joinType JoinType, table string, on string) SelectBuilder {
	q.joins = append(q.joins, joinInfo{joinType, table, on})
	return q
}

func (q *selectBuilder) Build() (string, []any) {
	var b strings.Builder

	b.WriteString("SELECT ")
	b.WriteString(strings.Join(q.selectColumns, ", "))

	b.WriteString(" FROM ")
	b.WriteString(q.fromTable)

	for _, join := range q.joins {
		b.WriteString(fmt.Sprintf(" %s %s ON %s ", join.joinType, join.table, join.on))
	}

	// TODO: Implement the AND/OR properly
	if len(q.where) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(q.where, " AND "))
	}

	if len(q.orderBy) > 0 {
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(q.orderBy, ", "))
	}

	if q.limit > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d ", q.limit))
	}

	if q.offset > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d ", q.offset))
	}
	args := q.whereArgs
	q.Clear()
	return b.String(), args
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
