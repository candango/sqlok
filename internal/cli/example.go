package cli

import (
	"fmt"

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
		fmt.Println("sqlok core examples no longer open driver connections. Create a *sql.DB in your application, then pass it to sqlok APIs from there.")
	},
}
