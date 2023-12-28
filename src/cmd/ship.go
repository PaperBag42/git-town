package cmd

import (
	"fmt"

	"github.com/git-town/git-town/v11/src/cli/flags"
	"github.com/git-town/git-town/v11/src/cli/log"
	"github.com/git-town/git-town/v11/src/cmd/cmdhelpers"
	"github.com/git-town/git-town/v11/src/config/configdomain"
	"github.com/git-town/git-town/v11/src/execute"
	"github.com/git-town/git-town/v11/src/git/gitdomain"
	"github.com/git-town/git-town/v11/src/gohacks/slice"
	"github.com/git-town/git-town/v11/src/gohacks/stringslice"
	"github.com/git-town/git-town/v11/src/hosting"
	"github.com/git-town/git-town/v11/src/hosting/github"
	"github.com/git-town/git-town/v11/src/hosting/hostingdomain"
	"github.com/git-town/git-town/v11/src/messages"
	"github.com/git-town/git-town/v11/src/sync"
	"github.com/git-town/git-town/v11/src/validate"
	"github.com/git-town/git-town/v11/src/vm/interpreter"
	"github.com/git-town/git-town/v11/src/vm/opcode"
	"github.com/git-town/git-town/v11/src/vm/program"
	"github.com/git-town/git-town/v11/src/vm/runstate"
	"github.com/spf13/cobra"
)

const shipDesc = "Deliver a completed feature branch"

const shipHelp = `
Squash-merges the current branch, or <branch_name> if given,
into the main branch, resulting in linear history on the main branch.

- syncs the main branch
- pulls updates for <branch_name>
- merges the main branch into <branch_name>
- squash-merges <branch_name> into the main branch
  with commit message specified by the user
- pushes the main branch to the origin repository
- deletes <branch_name> from the local and origin repositories

Ships direct children of the main branch.
To ship a nested child branch, ship or kill all ancestor branches first.

If you use GitHub, this command can squash merge pull requests via the GitHub API. Setup:
1. Get a GitHub personal access token with the "repo" scope
2. Run 'git config %s <token>' (optionally add the '--global' flag)
Now anytime you ship a branch with a pull request on GitHub, it will squash merge via the GitHub API.
It will also update the base branch for any pull requests against that branch.

If your origin server deletes shipped branches, for example
GitHub's feature to automatically delete head branches,
run "git config %s false"
and Git Town will leave it up to your origin server to delete the remote branch.`

func shipCmd() *cobra.Command {
	addVerboseFlag, readVerboseFlag := flags.Verbose()
	addMessageFlag, readMessageFlag := flags.String("message", "m", "", "Specify the commit message for the squash commit")
	addDryRunFlag, readDryRunFlag := flags.DryRun()
	cmd := cobra.Command{
		Use:     "ship",
		GroupID: "basic",
		Args:    cobra.MaximumNArgs(1),
		Short:   shipDesc,
		Long:    cmdhelpers.Long(shipDesc, fmt.Sprintf(shipHelp, configdomain.KeyGithubToken, configdomain.KeyShipDeleteTrackingBranch)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeShip(args, readMessageFlag(cmd), readDryRunFlag(cmd), readVerboseFlag(cmd))
		},
	}
	addDryRunFlag(&cmd)
	addVerboseFlag(&cmd)
	addMessageFlag(&cmd)
	return &cmd
}

func executeShip(args []string, message string, dryRun, verbose bool) error {
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
	config, initialBranchesSnapshot, initialStashSnapshot, exit, err := determineShipConfig(args, repo, dryRun, verbose)
	if err != nil || exit {
		return err
	}
	if config.branchToShip.LocalName == config.branches.Initial {
		repoStatus, err := repo.Runner.Backend.RepoStatus()
		if err != nil {
			return err
		}
		err = validate.NoOpenChanges(repoStatus.OpenChanges)
		if err != nil {
			return err
		}
	}
	runState := runstate.RunState{
		Command:             "ship",
		DryRun:              dryRun,
		InitialActiveBranch: initialBranchesSnapshot.Active,
		RunProgram:          shipProgram(config, message),
	}
	return interpreter.Execute(interpreter.ExecuteArgs{
		RunState:                &runState,
		Run:                     repo.Runner,
		Connector:               config.connector,
		Verbose:                 verbose,
		Lineage:                 config.lineage,
		NoPushHook:              config.pushHook.Negate(),
		RootDir:                 repo.RootDir,
		InitialBranchesSnapshot: initialBranchesSnapshot,
		InitialConfigSnapshot:   repo.ConfigSnapshot,
		InitialStashSnapshot:    initialStashSnapshot,
	})
}

type shipConfig struct {
	branches                 configdomain.Branches
	branchToShip             gitdomain.BranchInfo
	connector                hostingdomain.Connector
	dryRun                   bool
	targetBranch             gitdomain.BranchInfo
	canShipViaAPI            bool
	childBranches            gitdomain.LocalBranchNames
	proposalMessage          string
	deleteTrackingBranch     configdomain.ShipDeleteTrackingBranch
	hasOpenChanges           bool
	remotes                  gitdomain.Remotes
	isShippingInitialBranch  bool
	isOnline                 configdomain.Online
	lineage                  configdomain.Lineage
	mainBranch               gitdomain.LocalBranchName
	previousBranch           gitdomain.LocalBranchName
	proposal                 *hostingdomain.Proposal
	proposalsOfChildBranches []hostingdomain.Proposal
	syncPerennialStrategy    configdomain.SyncPerennialStrategy
	pushHook                 configdomain.PushHook
	syncUpstream             configdomain.SyncUpstream
	syncFeatureStrategy      configdomain.SyncFeatureStrategy
	syncBeforeShip           configdomain.SyncBeforeShip
}

func determineShipConfig(args []string, repo *execute.OpenRepoResult, dryRun, verbose bool) (*shipConfig, gitdomain.BranchesStatus, gitdomain.StashSize, bool, error) {
	lineage := repo.Runner.GitTown.Lineage
	pushHook := repo.Runner.GitTown.PushHook
	branches, branchesSnapshot, stashSnapshot, exit, err := execute.LoadBranches(execute.LoadBranchesArgs{
		Repo:                  repo,
		Verbose:               verbose,
		Fetch:                 true,
		HandleUnfinishedState: true,
		Lineage:               lineage,
		PushHook:              pushHook,
		ValidateIsConfigured:  true,
		ValidateNoOpenChanges: len(args) == 0,
	})
	if err != nil || exit {
		return nil, branchesSnapshot, stashSnapshot, exit, err
	}
	previousBranch := repo.Runner.Backend.PreviouslyCheckedOutBranch()
	repoStatus, err := repo.Runner.Backend.RepoStatus()
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	remotes, err := repo.Runner.Backend.Remotes()
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	deleteTrackingBranch := repo.Runner.GitTown.ShipDeleteTrackingBranch
	mainBranch := repo.Runner.GitTown.MainBranch
	branchNameToShip := gitdomain.NewLocalBranchName(slice.FirstElementOr(args, branches.Initial.String()))
	branchToShip := branches.All.FindByLocalName(branchNameToShip)
	if branchToShip != nil && branchToShip.SyncStatus == gitdomain.SyncStatusOtherWorktree {
		return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.ShipBranchOtherWorktree, branchNameToShip)
	}
	isShippingInitialBranch := branchNameToShip == branches.Initial
	syncFeatureStrategy := repo.Runner.GitTown.SyncFeatureStrategy
	syncPerennialStrategy := repo.Runner.GitTown.SyncPerennialStrategy
	syncUpstream := repo.Runner.GitTown.SyncUpstream
	syncBeforeShip := repo.Runner.GitTown.SyncBeforeShip
	if !isShippingInitialBranch {
		if branchToShip == nil {
			return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.BranchDoesntExist, branchNameToShip)
		}
	}
	if !branches.Types.IsFeatureBranch(branchNameToShip) {
		return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.ShipNoFeatureBranch, branchNameToShip)
	}
	branches.Types, lineage, err = execute.EnsureKnownBranchAncestry(branchNameToShip, execute.EnsureKnownBranchAncestryArgs{
		AllBranches:   branches.All,
		BranchTypes:   branches.Types,
		DefaultBranch: mainBranch,
		Lineage:       lineage,
		MainBranch:    mainBranch,
		Runner:        repo.Runner,
	})
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	err = ensureParentBranchIsMainOrPerennialBranch(branchNameToShip, branches.Types, lineage)
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	targetBranchName := lineage.Parent(branchNameToShip)
	targetBranch := branches.All.FindByLocalName(targetBranchName)
	if targetBranch == nil {
		return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.BranchDoesntExist, targetBranchName)
	}
	canShipViaAPI := false
	proposalMessage := ""
	var proposal *hostingdomain.Proposal
	childBranches := lineage.Children(branchNameToShip)
	proposalsOfChildBranches := []hostingdomain.Proposal{}
	pushHook = repo.Runner.GitTown.PushHook
	originURL := repo.Runner.GitTown.OriginURL()
	hostingService, err := repo.Runner.GitTown.HostingService()
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	connector, err := hosting.NewConnector(hosting.NewConnectorArgs{
		HostingService:  hostingService,
		GetSHAForBranch: repo.Runner.Backend.SHAForBranch,
		OriginURL:       originURL,
		GiteaAPIToken:   repo.Runner.GitTown.GiteaToken,
		GithubAPIToken:  github.GetAPIToken(repo.Runner.GitTown.GitHubToken),
		GitlabAPIToken:  repo.Runner.GitTown.GitLabToken,
		MainBranch:      mainBranch,
		Log:             log.Printing{},
	})
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	if !repo.IsOffline && connector != nil {
		if branchToShip.HasTrackingBranch() {
			proposal, err = connector.FindProposal(branchNameToShip, targetBranchName)
			if err != nil {
				return nil, branchesSnapshot, stashSnapshot, false, err
			}
			if proposal != nil {
				canShipViaAPI = true
				proposalMessage = connector.DefaultProposalMessage(*proposal)
			}
		}
		for _, childBranch := range childBranches {
			childProposal, err := connector.FindProposal(childBranch, branchNameToShip)
			if err != nil {
				return nil, branchesSnapshot, stashSnapshot, false, fmt.Errorf(messages.ProposalNotFoundForBranch, branchNameToShip, err)
			}
			if childProposal != nil {
				proposalsOfChildBranches = append(proposalsOfChildBranches, *childProposal)
			}
		}
	}
	return &shipConfig{
		branches:                 branches,
		connector:                connector,
		dryRun:                   dryRun,
		targetBranch:             *targetBranch,
		branchToShip:             *branchToShip,
		canShipViaAPI:            canShipViaAPI,
		childBranches:            childBranches,
		proposalMessage:          proposalMessage,
		deleteTrackingBranch:     deleteTrackingBranch,
		hasOpenChanges:           repoStatus.OpenChanges,
		remotes:                  remotes,
		isOnline:                 repo.IsOffline.ToOnline(),
		isShippingInitialBranch:  isShippingInitialBranch,
		lineage:                  lineage,
		mainBranch:               mainBranch,
		previousBranch:           previousBranch,
		proposal:                 proposal,
		proposalsOfChildBranches: proposalsOfChildBranches,
		syncPerennialStrategy:    syncPerennialStrategy,
		pushHook:                 pushHook,
		syncUpstream:             syncUpstream,
		syncFeatureStrategy:      syncFeatureStrategy,
		syncBeforeShip:           syncBeforeShip,
	}, branchesSnapshot, stashSnapshot, false, nil
}

func ensureParentBranchIsMainOrPerennialBranch(branch gitdomain.LocalBranchName, branchTypes configdomain.BranchTypes, lineage configdomain.Lineage) error {
	parentBranch := lineage.Parent(branch)
	if !branchTypes.IsMainBranch(parentBranch) && !branchTypes.IsPerennialBranch(parentBranch) {
		ancestors := lineage.Ancestors(branch)
		ancestorsWithoutMainOrPerennial := ancestors[1:]
		oldestAncestor := ancestorsWithoutMainOrPerennial[0]
		return fmt.Errorf(`shipping this branch would ship %s as well,
please ship %q first`, stringslice.Connect(ancestorsWithoutMainOrPerennial.Strings()), oldestAncestor)
	}
	return nil
}

func shipProgram(config *shipConfig, commitMessage string) program.Program {
	prog := program.Program{}
	if config.syncBeforeShip {
		// sync the parent branch
		sync.BranchProgram(config.targetBranch, sync.BranchProgramArgs{
			BranchInfos:           config.branches.All,
			BranchTypes:           config.branches.Types,
			Remotes:               config.remotes,
			IsOnline:              config.isOnline,
			Lineage:               config.lineage,
			Program:               &prog,
			MainBranch:            config.mainBranch,
			SyncPerennialStrategy: config.syncPerennialStrategy,
			PushBranch:            true,
			PushHook:              config.pushHook,
			SyncUpstream:          config.syncUpstream,
			SyncFeatureStrategy:   config.syncFeatureStrategy,
		})
		// sync the branch to ship (local sync only)
		sync.BranchProgram(config.branchToShip, sync.BranchProgramArgs{
			BranchInfos:           config.branches.All,
			BranchTypes:           config.branches.Types,
			Remotes:               config.remotes,
			IsOnline:              config.isOnline,
			Lineage:               config.lineage,
			Program:               &prog,
			MainBranch:            config.mainBranch,
			SyncPerennialStrategy: config.syncPerennialStrategy,
			PushBranch:            false,
			PushHook:              config.pushHook,
			SyncUpstream:          config.syncUpstream,
			SyncFeatureStrategy:   config.syncFeatureStrategy,
		})
	}
	prog.Add(&opcode.EnsureHasShippableChanges{Branch: config.branchToShip.LocalName, Parent: config.mainBranch})
	prog.Add(&opcode.Checkout{Branch: config.targetBranch.LocalName})
	if config.canShipViaAPI {
		// update the proposals of child branches
		for _, childProposal := range config.proposalsOfChildBranches {
			prog.Add(&opcode.UpdateProposalTarget{
				ProposalNumber: childProposal.Number,
				NewTarget:      config.targetBranch.LocalName,
			})
		}
		// push
		prog.Add(&opcode.PushCurrentBranch{CurrentBranch: config.branchToShip.LocalName, NoPushHook: config.pushHook.Negate()})
		prog.Add(&opcode.ConnectorMergeProposal{
			Branch:          config.branchToShip.LocalName,
			ProposalNumber:  config.proposal.Number,
			CommitMessage:   commitMessage,
			ProposalMessage: config.proposalMessage,
		})
		prog.Add(&opcode.PullCurrentBranch{})
	} else {
		prog.Add(&opcode.SquashMerge{Branch: config.branchToShip.LocalName, CommitMessage: commitMessage, Parent: config.targetBranch.LocalName})
	}
	if config.remotes.HasOrigin() && config.isOnline.Bool() {
		prog.Add(&opcode.PushCurrentBranch{CurrentBranch: config.targetBranch.LocalName, NoPushHook: config.pushHook.Negate()})
	}
	// NOTE: when shipping via API, we can always delete the remote branch because:
	// - we know we have a tracking branch (otherwise there would be no PR to ship via API)
	// - we have updated the PRs of all child branches (because we have API access)
	// - we know we are online
	if config.canShipViaAPI || (config.branchToShip.HasTrackingBranch() && len(config.childBranches) == 0 && config.isOnline.Bool()) {
		if config.deleteTrackingBranch {
			prog.Add(&opcode.DeleteTrackingBranch{Branch: config.branchToShip.RemoteName})
		}
	}
	prog.Add(&opcode.DeleteLocalBranch{Branch: config.branchToShip.LocalName, Force: false})
	if !config.dryRun {
		prog.Add(&opcode.DeleteParentBranch{Branch: config.branchToShip.LocalName})
	}
	for _, child := range config.childBranches {
		prog.Add(&opcode.ChangeParent{Branch: child, Parent: config.targetBranch.LocalName})
	}
	if !config.isShippingInitialBranch {
		prog.Add(&opcode.Checkout{Branch: config.branches.Initial})
	}
	cmdhelpers.Wrap(&prog, cmdhelpers.WrapOptions{
		DryRun:                   config.dryRun,
		RunInGitRoot:             true,
		StashOpenChanges:         !config.isShippingInitialBranch && config.hasOpenChanges,
		PreviousBranchCandidates: gitdomain.LocalBranchNames{config.previousBranch},
	})
	return prog
}
