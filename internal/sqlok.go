package sqlok

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/candango/sqlok/internal/schema"
)

type DatabaseLoader interface {
	Load() error
	Tables() []*schema.Table
}

type Loader struct {
	ctx     context.Context
	db      *sql.DB
	tables  []*schema.Table
	builder SelectBuilder
}

func NewLoader(db *sql.DB, ctx context.Context) DatabaseLoader {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Loader{
		db:      db,
		ctx:     ctx,
		builder: NewSelectBuilder(),
	}
}

func (l *Loader) Load() error {
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

func (l *Loader) loadTables() ([]*schema.Table, error) {
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

	if err != nil {
		return nil, fmt.Errorf("Failed to run query : %v\n", err)
	}

	defer rows.Close()
	// ctypes, err := rows.ColumnTypes()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to process column types : %v\n", err)
	// }
	// columns, err := rows.Columns()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to process columns : %v\n", err)
	// }

	tables := []*schema.Table{}
	for rows.Next() {
		table := &schema.Table{}
		name := table.Name()
		if err := rows.Scan(&table.Schema, &name); err != nil {
			return nil, fmt.Errorf("Failed to scan row: %v", err)
		}

		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("Failed reading rows: %v", err)
	}
	return tables, nil
}

func (l *Loader) loadFields(table *schema.Table) ([]*schema.Field, error) {
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
		return nil, fmt.Errorf("Failed to run query : %v\n", err)
	}

	defer rows.Close()

	fields := []*schema.Field{}
	for rows.Next() {
		field := &schema.Field{}
		name := field.Name()
		if err := rows.Scan(&name, &field.Type); err != nil {
			return nil, fmt.Errorf("Failed to scan row: %v", err)
		}
		fields = append(fields, field)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("Failed reading rows: %v", err)
	}
	return fields, nil
}

func (l *Loader) Tables() []*schema.Table {
	return l.tables
}
