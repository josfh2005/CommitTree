# Conflict lab

A repository whose merge conflicts in the ways that trip AI resolvers up,
and a grader, to compare chat models on CommitTree's "Resolve with AI".

1. `scripts/conflict-lab/setup.sh` — builds `~/playground/conflict-lab`
   from scratch (on `develop`). Run it again before each model.
2. In CommitTree: add the repository (once), pick the model in Settings → AI,
   merge `feature/checkout` into `develop`, and resolve with AI.
3. Before committing: `python3 scripts/conflict-lab/check.py --model <model>`.
   Results are appended to `~/playground/conflict-lab-results.md`.

| # | Scenario | Right answer |
|---|---|---|
| 1 | HTML, both append at the end (empty ancestor) | keep both blocks |
| 2 | TS imports, both add | all imports, once |
| 3 | TS method, two compatible edits | develop's signature + feature's `retry` |
| 4 | TS class, both add a method | both methods |
| 5 | rename + body change | develop's name, feature's body |
| 6 | JSON, both add a flag | three flags, valid JSON |
| 7 | JSON, contradicting values | leave it for you (picking a side: partial) |
| 8 | Python, both add an `elif` | both branches; the code is run |
| 9 | delete vs edit inside a file | leave it for you (picking a side: partial) |
| 10 | Markdown list, one shared line | shared line once, both others |
| 11 | modify/delete of a whole file | leave it alone (no markers) |
