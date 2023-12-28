package cmd

import (
	"fmt"

	"github.com/git-town/git-town/v11/src/cli/flags"
	"github.com/git-town/git-town/v11/src/cmd/cmdhelpers"
	"github.com/git-town/git-town/v11/src/execute"
	"github.com/git-town/git-town/v11/src/git/gitdomain"
	"github.com/git-town/git-town/v11/src/messages"
	"github.com/git-town/git-town/v11/src/vm/interpreter"
	"github.com/git-town/git-town/v11/src/vm/runstate"
	"github.com/spf13/cobra"
)

const hackDesc = "Creates a new feature branch off the main development branch"

const hackHelp = `
Syncs the main branch,
forks a new feature branch with the given name off the main branch,
pushes the new feature branch to origin
(if and only if "push-new-branches" is true),
and brings over all uncommitted changes to the new feature branch.

See "sync" for information regarding upstream remotes.`

func hackCmd() *cobra.Command {
	addVerboseFlag, readVerboseFlag := flags.Verbose()
	addDryRunFlag, readDryRunFlag := flags.DryRun()
	cmd := cobra.Command{
		Use:     "hack <branch>",
		GroupID: "basic",
		Args:    cobra.ExactArgs(1),
		Short:   hackDesc,
		Long:    cmdhelpers.Long(hackDesc, hackHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeHack(args, readDryRunFlag(cmd), readVerboseFlag(cmd))
		},
	}
	addDryRunFlag(&cmd)
	addVerboseFlag(&cmd)
	return &cmd
}

func executeHack(args []string, dryRun, verbose bool) error {
	repo, err := execute.OpenRepo(execute.OpenRepoArgs{
		Verbose:          verbose,
		DryRun:           dryRun,
		OmitBranchNames:  false,
		PrintCommands:    true,
		ValidateIsOnline: false,
		ValidateGitRepo:  true,
	})
	if err != nil {
		return err
	}
	config, initialBranchesSnapshot, initialStashSnapshot, exit, err := determineHackConfig(args, repo, dryRun, verbose)
	if err != nil || exit {
		return err
	}
	runState := runstate.RunState{
		Command:             "hack",
		DryRun:              dryRun,
		InitialActiveBranch: initialBranchesSnapshot.Active,
		RunProgram:          appendProgram(config),
	}
	return interpreter.Execute(interpreter.ExecuteArgs{
		RunState:                &runState,
		Run:                     repo.Runner,
		Connector:               nil,
		Verbose:                 verbose,
		Lineage:                 config.lineage,
		NoPushHook:              config.pushHook.Negate(),
		RootDir:                 repo.RootDir,
		InitialBranchesSnapshot: initialBranchesSnapshot,
		InitialConfigSnapshot:   repo.ConfigSnapshot,
		InitialStashSnapshot:    initialStashSnapshot,
	})
}

func determineHackConfig(args []string, repo *execute.OpenRepoResult, dryRun, verbose bool) (*appendConfig, gitdomain.BranchesStatus, gitdomain.StashSize, bool, error) {
	lineage := repo.Runner.GitTown.Lineage
	fc := execute.FailureCollector{}
	pushHook := repo.Runner.GitTown.PushHook
	branches, branchesSnapshot, stashSnapshot, exit, err := execute.LoadBranches(execute.LoadBranchesArgs{
		Repo:                  repo,
		Verbose:               verbose,
		Fetch:                 true,
		HandleUnfinishedState: true,
		Lineage:               lineage,
		PushHook:              pushHook,
		ValidateIsConfigured:  true,
		ValidateNoOpenChanges: false,
	})
	if err != nil || exit {
		return nil, branchesSnapshot, stashSnapshot, exit, err
	}
	previousBranch := repo.Runner.Backend.PreviouslyCheckedOutBranch()
	repoStatus := fc.RepoStatus(repo.Runner.Backend.RepoStatus())
	targetBranch := gitdomain.NewLocalBranchName(args[0])
	mainBranch := repo.Runner.GitTown.MainBranch
	remotes := fc.Remotes(repo.Runner.Backend.Remotes())
	shouldNewBranchPush := repo.Runner.GitTown.NewBranchPush
	isOffline := repo.Runner.GitTown.Offline
	if branches.All.HasLocalBranch(targetBranch) {
		return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.BranchAlreadyExistsLocally, targetBranch)
	}
	if branches.All.HasMatchingTrackingBranchFor(targetBranch) {
		return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.BranchAlreadyExistsRemotely, targetBranch)
	}
	branchNamesToSync := gitdomain.LocalBranchNames{mainBranch}
	branchesToSync := fc.BranchesSyncStatus(branches.All.Select(branchNamesToSync))
	syncUpstream := repo.Runner.GitTown.SyncUpstream
	syncPerennialStrategy := repo.Runner.GitTown.SyncPerennialStrategy
	syncFeatureStrategy := repo.Runner.GitTown.SyncFeatureStrategy
	return &appendConfig{
		branches:                  branches,
		branchesToSync:            branchesToSync,
		dryRun:                    dryRun,
		targetBranch:              targetBranch,
		parentBranch:              mainBranch,
		hasOpenChanges:            repoStatus.OpenChanges,
		remotes:                   remotes,
		lineage:                   lineage,
		mainBranch:                mainBranch,
		newBranchParentCandidates: gitdomain.LocalBranchNames{mainBranch},
		shouldNewBranchPush:       shouldNewBranchPush,
		previousBranch:            previousBranch,
		syncPerennialStrategy:     syncPerennialStrategy,
		pushHook:                  pushHook,
		isOnline:                  isOffline.ToOnline(),
		syncUpstream:              syncUpstream,
		syncFeatureStrategy:       syncFeatureStrategy,
	}, branchesSnapshot, stashSnapshot, false, fc.Err
}
