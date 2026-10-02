# Repository settings

A repository's own settings, opened with "Repository settings…" in its row's
context menu (not for a linked worktree, whose remotes are its main
repository's; disabled while the folder is missing). The dialog is titled
"<repository> settings" and has tabs; today only **Remotes**. Escape or the
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
