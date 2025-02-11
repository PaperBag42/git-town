package debug

import (
	"os"

	"github.com/git-town/git-town/v12/src/cli/dialog"
	"github.com/git-town/git-town/v12/src/cli/dialog/components"
	"github.com/spf13/cobra"
)

func welcome() *cobra.Command {
	return &cobra.Command{
		Use: "welcome",
		RunE: func(cmd *cobra.Command, args []string) error {
			dialogTestInputs := components.LoadTestInputs(os.Environ())
			_, err := dialog.Welcome(dialogTestInputs.Next())
			return err
		},
	}
}
