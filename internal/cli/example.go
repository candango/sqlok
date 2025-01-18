package cli

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	sqlok "github.com/candango/sqlok/internal"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
)

type ExampleResult struct {
}

func LastInsertId() (int64, error) {
	return 0, nil
}

func RowsAffected() (int64, error) {
	return 0, nil
}

type User struct {
	Id          int64
	Name        string
	Description string
}

// exampleCmd represents the database command
var exampleCmd = &cobra.Command{
	Use:   "example",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("please inform the connection string")
			os.Exit(1)
		}
		cstr := args[0]
		ctx := context.Background()
		db, err := sql.Open("pgx", cstr)
		if err != nil {
			log.Fatalf("unable to connect to the database: %v\n", err)
		}
		defer db.Close()

		user := &User{
			Name:        "name",
			Description: "description",
		}

		ibu := sqlok.NewInsertBuiler()
		dbu := sqlok.NewDeleteBuiler()
		sbu := sqlok.NewSelectBuiler()
		ubu := sqlok.NewUpdateBuilder()

		ibu.InsertInto(
			"auser",
		).Columns(
			"name",
			"description",
		).Values(user.Name, user.Description)

		res, err := ibu.Execute(ctx, db)

		if err != nil {
			log.Fatalf("fail to insert: %v\n", err)
		}

		id, _ := res.LastInsertId()
		ra, _ := res.RowsAffected()
		user.Id = id
		fmt.Printf("Last Insert Id %d\n", id)
		fmt.Printf("Rows Affected %d\n", ra)

		user.Description = "Description com D maiúsculo"

		ubu.Update(
			"auser",
		).Set(
			"description", user.Description,
		).Where(
			"id = $2", user.Id,
		)

		res, err = ubu.Execute(ctx, db)
		if err != nil {
			log.Fatalf("fail to update: %v\n", err)
		}

		recUser := &User{}

		sbu.Select("*").From("auser").Where("id = $1", user.Id)
		rows, err := sbu.Execute(ctx, db)
		if err != nil {
			log.Fatalf("fail to select: %v\n", err)
		}

		for rows.Next() {
			err := rows.Scan(&recUser.Id, &recUser.Name, &recUser.Description)
			if err != nil {
				log.Fatalf("fail reading row after the insert operation: %v", err)
			}
		}
		// recUser := &User{}
		fmt.Println("Usuário recuperado:")
		fmt.Println("Id:")

		fmt.Println(recUser)

		res, err = dbu.Delete("auser").Where("id = $1", recUser.Id).Execute(ctx, db)
		if err != nil {
			log.Fatalf("fail to delete: %v\n", err)
		}

		fmt.Printf("Rows Affected %d\n", ra)
		fmt.Println("Usuário deletado com sucesso")

	},
}
