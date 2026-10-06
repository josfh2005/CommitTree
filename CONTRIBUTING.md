# Contributing to CommitTree

Thanks for your interest! Bug reports, ideas and pull requests are all welcome.

## Reporting a bug or asking for a feature

Open an [issue](https://github.com/josfh2005/CommitTree/issues/new/choose) and pick
the bug report or feature request template. For a bug, the CommitTree version (shown
next to Settings in the sidebar), your operating system and the steps to reproduce it
help the most. The **Commands** panel shows the git commands CommitTree ran — paste
the relevant ones if git is involved.

Security problems are not reported in issues: see [SECURITY.md](SECURITY.md).

## Working on the code

[docs/development.md](docs/development.md) explains how to set up, run, test and
build the app. Before changing behaviour, read the part of the
[specification](docs/spec/README.md) it touches — the specification describes what
the app does, and a change in behaviour updates it in the same pull request.

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests: `go test ./...` and, in `frontend/`,
   `npm test && npm run check` must pass.
3. Write commit messages in the style of the history:
   `feat(area): …`, `fix(area): …`, `docs(spec): …`, `chore: …`.
4. Open a pull request against `main` and fill in its template.

Small, focused pull requests are reviewed fastest. For a larger change, open an
issue first so we can agree on the approach.

## Code of conduct

Everyone taking part is expected to follow the [code of conduct](CODE_OF_CONDUCT.md).

## License

CommitTree is licensed under the [GNU General Public License v3.0](LICENSE). By
contributing, you agree that your contributions are licensed under the same terms.
