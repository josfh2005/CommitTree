# Repository settings

A repository's own settings, opened with "Repository settings…" in its row's
context menu (not for a linked worktree, whose remotes are its main
repository's; disabled while the folder is missing). The dialog is titled
"<repository> settings" and has tabs: **Remotes**, **Git-flow** and **AI**. Escape or the
close button closes it, and it closes by itself if the repository leaves the
list.

## Remotes

One row per remote, by name: its name, its URL (cut short; the full URL in
the tooltip) and three buttons.

- **Test** asks the remote for its branches, never prompting for a password
  and giving up after 30 s. The row then says "Connected", "Authentication
  failed", "Timed out" or the first line of git's error. Test does not lock
  the repository.
- **Edit** turns the URL into a field with Save and Cancel (Enter, Escape).
  Pointing a remote elsewhere ends a background-fetch pause of that remote
  (see `05-remote-and-stash.md`).
- **Remove…** asks "Remove remote <name>?" — its remote branches leave the
  log, and branches that track it lose their upstream — and removes it,
  ending its pause too.

"No remotes yet." when there are none. **Add remote** opens a Name and URL
form (Name is `origin` for a repository's first remote). Add stays disabled
while a field is blank; a name with spaces or one already used says why
under the form; anything else git refuses is shown in the dialog.

Edits, removals and additions are git commands like any other operation:
they show the busy label, are refused while another operation runs on the
repository (a background fetch is cancelled instead), and appear in the
Commands panel. A failure is shown at the top of the tab, not as a toast,
and leaves the add or edit form open with what was typed. Results of a Test
belong to the opening of the dialog that asked for them: one that answers
after the dialog was closed or moved to another repository is dropped.
Afterwards the sidebar, the log and the toolbar of the selected
repository refresh.

## Git-flow

The repository's branch roles, as the `gitflow.*` keys of its own git config
(the ones `10-git-flow.md` describes): **Production branch**
(`gitflow.branch.master`), **Development branch** (`gitflow.branch.develop`)
and the **Feature**, **Release**, **Hotfix** and **Warmfix** prefixes
(`gitflow.prefix.*`; the warmfix one is the same field the Initialise dialog
has). Under the fields: "These names decide the main branches pushed by "Push
main branches"." (see `05-remote-and-stash.md`).

Checked before saving, and again by Go (`gitflow.SaveSettings`), which refuses
with the same reasons: both branch names are filled in and valid branch names
(what `git check-ref-format --branch` accepts, minus `@` and anything with
`@{`), production and development differ (ignoring case), and every prefix is
filled in, has no spaces, makes a valid branch name when a name is added to it
and differs from the other three (ignoring case). A prefix need not end in `/`. The first
problem is shown under the fields and Save stays disabled; a refusal from Go
is shown there too, with what was typed kept, until the next edit. Enter in a field saves.

- **Set up**: **Save** (disabled until something changed) rewrites only the
  `gitflow.*` keys, under the repository's write lock and with the busy
  label like any other write. It creates, renames and deletes no branch and
  does not need the named branches to exist; when one does not, the tab says
  "<branch> does not exist." Other keys (`bugfix`, `support`, a branch's
  `.base`) are left alone.
- **Not set up** (a repository without both branch keys): the fields show the
  defaults the app uses — `main` or, failing that, `master` (else the
  checked-out branch), `develop`, `feature/`, `release/`, `hotfix/`,
  `warmfix/` — and a line says git-flow isn't set up and these defaults
  decide the main branches for "Push main branches", and that Set up git-flow
creates the development branch if it is missing. The button reads **Set
  up git-flow** and runs the same action as the Initialise dialog
  (`InitFlow`): production must exist, development is created from it when
  missing, and the keys are written. The repository is then git-flow enabled
  as if initialised from the Flow button.

After a save the tab reloads, and the repository's refs and sidebar are read
again (the selected repository, or an expanded one in the sidebar), so the
"Main branches (N)" count and the Push tooltip follow the new names at once.

## AI

**Use AI in this repository** turns every AI action there on or off; off
dims the rest of the tab. **Models** sets the chat and task provider and
model, and **Automation** the commit message and suggested replies modes;
each select's first option, "Global (…)", shows and keeps the global value
(a mode with the label its select uses, e.g. "Global (Only when I ask)").
Choosing a provider picks its first model; a provider that lists no model
(no key, Ollama stopped) is not chosen and the tab says why. **Your
instructions** — one box for every action and one per action added with
"Add instructions for…" — are saved when a box loses focus and stay on this
computer, and also when the dialog closes (Escape, ✕) with a box still being typed in.

**Repository instructions** lists the `.md` files in the repository's
`.committree/` folder, each foldable to read it and with the reason when it
is ignored. Their state is Approved, Not approved, Ignored, or Changed since
you approved; **Approve** and **Ignore** act on all of them, and are
refused — with the tab showing the new content — if the files changed
since the tab read them. With no files the tab says how to add one.

The tab reloads when the repository's AI settings or approval change
elsewhere (`repo-ai:changed`); a box being typed in keeps its text. A failed
save is shown at the top of the tab and keeps what was entered; the
**Use AI** switch goes back to the stored state. From a linked worktree the
chat's "Repository settings" and "Review" buttons open this dialog for its
main repository, whose settings the worktree shares. If the
overrides file can't be read, its error is shown and the switch is disabled.
