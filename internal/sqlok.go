package sqlok

import (
	"context"
	"errors"
	"fmt"

	sql "database/sql"

	"github.com/candango/sqlok/internal/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type DatabaseLoader interface {
	Connect() error
	Disconnect() error
	Load() error
	Tables() []*schema.Table
}

type PostgresLoader struct {
	cString string
	ctx     context.Context
	db      *sql.DB
	tables  []*schema.Table
	builder SelectBuilder
}

func NewPostgresLoader(cString string, ctx context.Context) DatabaseLoader {
	return &PostgresLoader{
		cString: cString,
		ctx:     ctx,
		builder: NewSelectBuiler(),
	}

}

func (l *PostgresLoader) Connect() error {
	var err error
	l.ctx = context.Background()
	l.db, err = sql.Open("pgx", l.cString)
	if err != nil {
		return errors.New(fmt.Sprintf("Unable to connect to the database: %v\n", err))
	}
	return nil
}

func (l *PostgresLoader) Disconnect() error {
	return l.db.Close()
}

func (l *PostgresLoader) Load() error {
	tables, err := l.loadTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		fields, err := l.loadFields(table)
		if err != nil {
			return err
		}
		table.Fields = fields
	}
	l.tables = tables
	return nil
}

func (l *PostgresLoader) loadTables() ([]*schema.Table, error) {
	l.builder.Select(
		"table_schema", "table_name",
	).From(
		"information_schema.tables",
	).Where(
		"table_type = 'BASE TABLE'",
	).And(
		"table_schema not in ('pg_catalog', 'information_schema')",
	)

	rows, err := l.builder.Execute(l.ctx, l.db)
	fmt.Println(rows)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to run query : %v\n", err))
	}

	defer rows.Close()

	tables := []*schema.Table{}
	for rows.Next() {
		table := &schema.Table{}
		if err := rows.Scan(&table.Schema, &table.Name); err != nil {
			return nil, errors.New(fmt.Sprintf("Failed to scan row: %v", err))
		}

		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		return nil, errors.New(fmt.Sprintf("Failed reading rows: %v", err))
	}
	return tables, nil
}

func (l *PostgresLoader) loadFields(table *schema.Table) ([]*schema.Field, error) {
	l.builder.Select(
		"column_name", "data_type",
	).From(
		"information_schema.columns",
	).Where(
		"table_schema = $1", table.Schema,
	).And(
		"table_name = $2", table.Name,
	)

	rows, err := l.builder.Execute(l.ctx, l.db)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to run query : %v\n", err))
	}

	defer rows.Close()

	fields := []*schema.Field{}
	for rows.Next() {
		field := &schema.Field{}
		if err := rows.Scan(&field.Name, &field.Type); err != nil {
			return nil, errors.New(fmt.Sprintf("Failed to scan row: %v", err))
		}
		fields = append(fields, field)
	}
	if err = rows.Err(); err != nil {
		return nil, errors.New(fmt.Sprintf("Failed reading rows: %v", err))
	}
	return fields, nil
}

func (l *PostgresLoader) Tables() []*schema.Table {
	return l.tables
}
