/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cli

import (
	"context"
	"errors"
	"fmt"
	"log"

	sqlok "github.com/candango/sqlok/internal"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

// databaseCmd represents the database command
var databaseCmd = &cobra.Command{
	Use:   "database",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, "postgres://nessie:DominioProjenv2020AdmDBshhh@127.0.0.1:5432/motl")
		if err != nil {
			log.Fatalf("Unable to connect to the database: %v\n", err)
		}

		defer conn.Close(ctx)
		tables, err := getTables(conn, ctx)
		if err != nil {
			log.Fatalf("Failed to process tables : %v\n", err)
		}

		for _, table := range tables {
			fmt.Printf("%s.%s\n", table.Schema, table.Name)
			fields, err := getFields(table, conn, ctx)
			if err != nil {
				log.Fatalf("Failed to process fields : %v\n", err)
			}
			for _, field := range fields {
				fmt.Printf("     %s %s\n", field.Name, field.Type)
			}
			table.Fields = fields
		}

		// for _, table := range tables {
		// 	fmt.Printf("%s.%s\n", table.Schema, table.Name)
		// 	for _, field := range table.Fields {
		// 		fmt.Printf("     %s %s\n", field.Name, field.Type)
		// 	}
		// }

		// Fechar a tabela
		fmt.Println("+---------------------+")
	},
}

func getFields(table sqlok.Table, conn *pgx.Conn, ctx context.Context) ([]sqlok.Field, error) {
	sql := fmt.Sprintf(`SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema = '%s' AND table_name = '%s'`, table.Schema, table.Name)
	// fmt.Println()
	// fmt.Println(sql)
	// fmt.Println()
	rows, err := conn.Query(ctx, sql)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to run query : %v\n", err))
	}

	defer rows.Close()

	fields := []sqlok.Field{}
	for rows.Next() {
		field := sqlok.Field{}
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

func getTables(conn *pgx.Conn, ctx context.Context) ([]sqlok.Table, error) {
	sql := `SELECT
			table_schema,
			table_name
		FROM
			information_schema.tables
		WHERE
			table_type = 'BASE TABLE' AND
			table_schema not in ('pg_catalog', 'information_schema');`
	rows, err := conn.Query(ctx, sql)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to run query : %v\n", err))
	}

	defer rows.Close()

	tables := []sqlok.Table{}
	for rows.Next() {
		table := sqlok.Table{}
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

func init() {
	rootCmd.AddCommand(databaseCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// databaseCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// databaseCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
