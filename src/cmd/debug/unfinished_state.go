package debug

import (
	"os"
	"time"

	"github.com/git-town/git-town/v12/src/cli/dialog"
	"github.com/git-town/git-town/v12/src/cli/dialog/components"
	"github.com/git-town/git-town/v12/src/git/gitdomain"
	"github.com/spf13/cobra"
)

func unfinishedStateCommitAuthorCmd() *cobra.Command {
	return &cobra.Command{
		Use: "unfinished-state",
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := gitdomain.NewLocalBranchName("feature-branch")
			dialogTestInputs := components.LoadTestInputs(os.Environ())
			_, _, err := dialog.AskHowToHandleUnfinishedRunState("sync", branch, time.Now().Add(time.Second*-1), true, dialogTestInputs.Next())
			return err
		},
	}
}
