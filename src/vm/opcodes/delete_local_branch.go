package opcodes

import (
	"github.com/git-town/git-town/v12/src/git/gitdomain"
	"github.com/git-town/git-town/v12/src/vm/shared"
)

// DeleteLocalBranch deletes the branch with the given name.
type DeleteLocalBranch struct {
	Branch gitdomain.LocalBranchName
	undeclaredOpcodeMethods
}

func (self *DeleteLocalBranch) Run(args shared.RunArgs) error {
	return args.Runner.Frontend.DeleteLocalBranch(self.Branch)
}
