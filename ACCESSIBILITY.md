# Accessibility

CommitTree should be usable by everyone who uses git. This page says what works
today, what does not yet, and how to tell us about a barrier.

## Supported environments

- macOS 26 or later on Apple silicon
- Linux (x86_64) with webkit2gtk-4.1

The interface is a web view inside a native window, so the system's screen reader
(VoiceOver on macOS, Orca on Linux) and zoom settings apply to it.

## What CommitTree does today

- **Themes:** Auto (follows the system), Light and Dark, plus a **High contrast**
  option that works with each of them (Settings → General).
- **Labels:** toolbar buttons have text labels and accessible names, also when the
  window is too narrow to show the text.
- **Keyboard:** dialogs close with Escape, the terminal opens and closes with ⌘J
  (Ctrl+J on Linux), Settings opens with ⌘, (Ctrl+, on Linux), and the commit message
  box commits with ⌘↵ (Ctrl+Enter).
- **Motion:** animated spinners stop when the system asks to reduce motion.
- **Plain language:** errors say what happened and what to do, in words rather than
  codes.

## Known limitations

- The commit log, the sidebar and the file lists are navigated with the mouse; arrow
  keys do not move the selection yet.
- Context menus open with a right-click only; there is no keyboard shortcut for them.
- CommitTree has not been tested end to end with a screen reader.
- Branch lanes in the graph are told apart by colour; the badges on each commit name
  the branch in text.

## Reporting a barrier

Open an [issue](https://github.com/josfh2005/CommitTree/issues/new/choose) with the
**Accessibility barrier** template: what you were trying to do, what got in the way,
and the assistive technology you use. Barriers are treated as bugs.
