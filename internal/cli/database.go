package cli

import (
	"fmt"

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
		fmt.Println("sqlok core is driver-agnostic now. Schema loading CLI requires an application-provided *sql.DB or an external adapter project.")
	},
}
