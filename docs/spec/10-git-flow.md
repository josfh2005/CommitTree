# git-flow

CommitTree starts and finishes git-flow branches with plain git. It reads
and writes the same `gitflow.*` keys in the repository's git config that
SourceTree and the git-flow tools use, so a branch started in one can be
finished in the other. The `git flow` command-line tool is not needed.

## Types

| Type | Starts from | Finish merges into, in order | Leaves you on |
|---|---|---|---|
| Feature | develop | develop | develop |
| Release | develop | master, develop | develop |
| Hotfix | master | master, any local releases ticked in the dialog, develop | develop |
| Warmfix | a local release branch | that release | that release |

"master" and "develop" are whatever `gitflow.branch.master` and
`gitflow.branch.develop` name. Prefixes come from `gitflow.prefix.*`,
defaulting to `feature/`, `release/`, `hotfix/` and `warmfix/` (SourceTree
never writes a warmfix prefix). A branch's type is found by its prefix,
ignoring case, so `Warmfix/NEXO-39` is a warmfix. `bugfix` and `support`
keys are kept but not used.

## The Flow button

The toolbar's Flow button (next to Merge) is disabled while an operation
runs or a conflict is being resolved. When the repository has no git-flow
config, or the config names a branch that does not exist, it opens the
Initialise dialog: production branch (an existing local branch), development
branch (created from production if missing) and the four prefixes.

The same names can be edited later, without creating or renaming a branch,
in the Git-flow tab of Repository settings (`12-repository-settings.md`);
both the dialog and the tab refuse a prefix with spaces, one that cannot
make a branch name and one equal to another prefix (ignoring case), and a
production and development branch that differ only in case. The branch keys
are written last, so a failed write never leaves git-flow half set up.

Otherwise it opens a menu:
- **Finish <type> <name>…** when the checked-out branch is a git-flow
  branch; otherwise **Finish…**, a list of the local git-flow branches with
  an "In progress" group first — branches a finish already merged into
  their first target (a merge commit there whose second parent is the
  branch tip; a merge into a later target, such as master back-merged into
  develop, does not count).
- **Start feature… / release… / hotfix… / warmfix…**. Warmfix is disabled
  with "No local release branch" when there is none.

## Start

A name, shown after its prefix and base. A warmfix with several local
releases also asks which one. Start fetches all remotes (a failure is
reported and the local branches are used), fast-forwards the base to its
upstream when it is only behind, refuses when the base has diverged ("develop
has diverged from origin/develop; pull it first"), creates and checks out
the branch — uncommitted changes come along — and records
`gitflow.branch.<branch>.base`.

## Finish

The dialog spells out the plan: the targets in order, each marked "already
there" when it contains the branch, then that the branch is deleted and
which branch is left checked out. A hotfix lists one unticked checkbox per
local release; a warmfix without a recorded base, or whose recorded release
no longer exists, asks for its release. Opened again after a conflict, the
dialog keeps the releases chosen before and ticks any release that already
has the branch.

Finish refuses while a conflict is in progress or tracked files have
uncommitted changes (untracked files do not block). It fetches, brings every
target that still needs the branch up to date (fast-forward only; a
diverged target stops everything before any merge), then for each target
that does not contain the branch yet checks it out and merges with
`--no-ff` and git's own message ("Merge branch 'hotfix/X' into master").
When every target has it, it checks out the ending branch, deletes the local
branch and its `.base` key. Nothing is pushed, tagged or deleted remotely.

## Conflicts

A conflicting merge stops the finish on that target with the merge in
progress, and the Merge view opens as for any merge, with the line "Part of
finishing hotfix/X (into develop)". When that merge ends — committed here or
in a terminal — and the target now has the branch, CommitTree offers
"Continue finishing hotfix/X", which runs Finish again without asking.
Finish skips targets that already contain the branch, so running it again —
from that toast or from the menu — always carries on where it stopped.
Nothing about the interrupted finish is stored on disk. Aborting the merge
drops the offer quietly; the branch is under "In progress" only if an
earlier target already has it (a conflict on the first target leaves
nothing merged).

Before anything moves, Finish also refuses when the branch, or a target that
still needs it, is checked out in another worktree ("develop is checked out
in another worktree (<path>); switch away from it there first"). An error
part-way (a hook rejecting a merge) is reported with the targets already
merged; Finish again continues.

Branch names and their `.base` keys are matched ignoring case, because on a
case-insensitive filesystem a branch created as `warmfix/X` next to
SourceTree's `Warmfix/…` branches is listed as `Warmfix/X`.
