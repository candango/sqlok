package cli

import (
	"context"
	"fmt"
	"log"

	sqlok "github.com/candango/sqlok/internal"
	"github.com/spf13/cobra"
)

type PgsqlRegistyBase struct {
}

func (r *PgsqlRegistyBase) FazAlgumaCoisa() {
}

type PgsqlRegisty struct {
	PgsqlRegistyBase
}

func (r *PgsqlRegistyBase) FazOutraCoisa() {
}

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
		loader := sqlok.NewPostgresLoader("postgres://nessie:DominioProjenv2020AdmDBshhh@127.0.0.1:5432/motl", ctx)
		err := loader.Connect()
		if err != nil {
			log.Fatalf("Unable to connect to the database: %v\n", err)
		}
		defer loader.Disconnect()
		err = loader.Load()
		if err != nil {
			log.Fatal(err)
		}

		template := `type %sRegistry struct{
}

func (r *%sRegistry) string {
	return "%s"
}
		`

		for _, table := range loader.Tables() {
			source := fmt.Sprintf(template, table.Name, table.Name, table.Schema)
			fmt.Printf(source)
			for _, field := range table.Fields {
				fmt.Printf("     %s %s\n", field.Name, field.Type)
			}
		}

	},
}
