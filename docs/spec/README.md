# CommitTree — specification

CommitTree is a desktop git client for people who work across many repositories
and want an assistant inside the tool rather than beside it. It shows a
repository's history, its working tree and its remotes, and it can call a
language model to write a commit message, resolve a merge conflict, or —
from the chat, with the user approving each one — carry out a git operation
itself, but every git operation remains something the user asks for and can
see: the model can propose one, but nothing runs until the user approves
that specific proposal.

These documents describe **what the application does**, not how it is built.
They are written so that someone who has never seen the codebase can rebuild
CommitTree in another language, with another UI toolkit, and end up with an
application that behaves the same. Where a behaviour only makes sense because
of an architectural constraint, the constraint is stated with it.

## How to read this

Start with **Conventions and constraints**: it holds the vocabulary the other
documents assume — how long operations announce themselves, how errors reach
the user, which confirmations are styled as dangerous, and the handful of
architectural facts (a git command line rather than a library, one write lock
per repository, state in JSON files, no server) that the rest depends on. Its
appendix collects the git command-line behaviour that cost this project bugs;
read it before implementing anything that shells out to git.

Then take the area documents in any order.

| Document | What it covers |
|---|---|
| [Repositories and sidebar](01-repositories-and-sidebar.md) | The repository list, groups, and the branch, remote, tag and stash sections |
| [Log and history](02-log-and-history.md) | The commit log and graph, filters, commit details, search |
| [Working tree](03-working-tree.md) | The Changes view, staging, discarding, committing |
| [Conflicts](04-conflicts.md) | One view for a merge, rebase, cherry-pick, revert, mailbox patch or stash conflict |
| [Remote and stash](05-remote-and-stash.md) | Fetch, pull, push, and the full stash lifecycle |
| [AI](06-ai.md) | Providers, the chat panel, generated commit messages, the conflict resolver |
| [Terminal](08-terminal.md) | The embedded shell: tabs per repository, freshness, safety |
| [Command log](09-command-log.md) | Every git command the application ran, who asked for it and how it ended |
| [git-flow](10-git-flow.md) | Starting and finishing feature, release, hotfix and warmfix branches from the Flow button |
| [Notifications](11-notifications.md) | Which events notify, and when as an OS notification or a toast |
| [Conventions and constraints](07-conventions-and-constraints.md) | What is true everywhere, plus the git appendix |

## What this specification is not

- It is not a description of the current code. Package names, components and
  framework APIs are deliberately absent; the one exception is the git
  appendix, which is about git itself.
- It is not the design documents in `docs/superpowers/`. Those were written
  before each piece of work and record what was intended at the time. Where
  they and the built behaviour disagree, this specification follows the built
  behaviour and says so under "Known divergences".
- It does not cover packaging, signing or distribution.

## Status

Every document here was written by reading the code as it stands, after the
working tree, push/pull/stash and sidebar-groups work landed. Parts of the
application have been verified by hand against a real repository; the
outstanding manual checks are listed in the project's own notes, not here —
this specification describes the intended, implemented behaviour, and a
rebuilder should treat every rule in it as something to test.
