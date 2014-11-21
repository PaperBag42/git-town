Feature: git-extract handling cherry-pick conflicts without open changes

  Background:
    Given I have a feature branch named "feature"
    And the following commits exist in my repository
      | branch  | location | message            | file name        | file content     |
      | main    | local    | conflicting commit | conflicting_file | main content     |
      | feature | local    | feature commit     | feature_file     |                  |
      | feature | local    | refactor commit    | conflicting_file | refactor content |
    And I am on the "feature" branch
    When I run `git extract refactor` with the last commit sha while allowing errors


  @finishes-with-non-empty-stash
  Scenario: result
    Then I end up on the "refactor" branch
    And my repo has a cherry-pick in progress
    And there is an abort script for "git extract"


  Scenario: aborting
    When I run `git extract --abort`
    Then I end up on the "feature" branch
    And there is no "refactor" branch
    And I have the following commits
      | branch   | location | message            | files            |
      | main     | local    | conflicting commit | conflicting_file |
      | feature  | local    | feature commit     | feature_file     |
      | feature  | local    | refactor commit    | conflicting_file |
    And my repo has no cherry-pick in progress
    And there is no abort script for "git extract" anymore
