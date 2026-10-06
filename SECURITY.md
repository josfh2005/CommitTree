# Security policy

## Supported versions

Only the [latest release](https://github.com/josfh2005/CommitTree/releases/latest)
receives security fixes.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub instead:
go to the [Security tab](https://github.com/josfh2005/CommitTree/security) and choose
**Report a vulnerability**. Include what an attacker could do, the steps to reproduce
it and the CommitTree version.

You will get an answer as soon as possible. Once a fix is released, the advisory is
published and you are credited unless you prefer otherwise.

## What is in scope

CommitTree runs git and a shell on your computer and can send repository content to
an AI provider, so these matter most:

- a way for a repository's content (files, commit messages, branch names) to make
  CommitTree run a command or change the repository without the user approving it
- an AI write action that runs without its confirmation card
- API keys leaking out of the system's secret store, into files or logs
- repository content sent to a hosted provider when only a local model is configured
