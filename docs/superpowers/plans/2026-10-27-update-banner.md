# Update banner and in-app update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CommitTree learns the latest GitHub release from `latest.json`, shows a card and a status-bar pill when it is newer, and installs it in place with one click (macOS + Linux), falling back to the release page.

**Architecture:** A new Go package `internal/update` does everything that touches the network or the disk: the manifest check, the download, the unpack, the swap and the relaunch. `internal/app/update.go` binds it to Wails as four methods and emits progress events. The frontend owns *when* to check and *when* to show the card, in a pure module `lib/update.ts`. The UI is a new `UpdateCard.svelte`, a pill in `StatusBar.svelte`, a version highlight in `Sidebar.svelte`, and an "Updates" section in Settings → General. A tiny Go command writes `latest.json` at release time.

**Tech Stack:** Go 1.x stdlib (`net/http`, `archive/tar`, `compress/gzip`, `crypto/sha256`), `/usr/bin/ditto` and `/usr/bin/plutil` on macOS, Wails v2 runtime, Svelte 5 + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-27-update-banner-design.md` (read it first; it holds the owner's rules and the reasons).

## Global Constraints

- git is `/usr/bin/git`. Commits never carry a `Co-Authored-By` line.
- Every task that changes behaviour updates `docs/spec/` **in the same commit**. Design docs do not count. The file each task touches is named in that task.
- Manifest URL: `https://github.com/josfh2005/CommitTree/releases/latest/download/latest.json`. Download URL: `https://github.com/josfh2005/CommitTree/releases/download/v<version>/<name>`.
- Test hook env var: `COMMITTREE_UPDATE_MANIFEST` (`http(s)://` or `file://`). When set, the asset URL is `<name>` resolved against the manifest URL's folder, and the host allow-list is off.
- Host allow-list (hook unset): https only; host `github.com` or ending in `.githubusercontent.com`, redirects included.
- Size caps: manifest ≤ 64 KiB, download ≤ 200 MiB. Manifest request timeout 15 s; the download aborts after 30 s without receiving a byte (stall), with no total timeout.
- Version format: `MAJOR.MINOR.PATCH`, digits only, no `v`. Anything else is invalid and that manifest is ignored.
- Asset keys: `darwin-arm64`, `darwin-amd64`, `linux-amd64` (`GOOS-GOARCH` of the running binary).
- Focus checks: at most once per 6 h (`6 * 60 * 60 * 1000` ms) since the last check; in memory only.
- UI copy is English. Card: title "CommitTree {latest} is available", muted "You have {current}", buttons "Update and restart" / "What's new" / ✕; when it cannot install in place, the primary button is "Download" (opens the release page). Settings: section "Updates", checkbox "Check for updates automatically" (default on), line "CommitTree {current}", button "Check now".
- The running version's only source is `wails.json` `info.productVersion` (embedded in `main.go` for Go; `__APP_VERSION__` for the frontend).
- `make build` regenerates `frontend/wailsjs` bindings. Commit the new `frontend/wailsjs/go/app/App.{js,d.ts}` / `models.ts` entries this feature needs, as earlier features did. Do not commit unrelated churn (`git checkout -- frontend/wailsjs/runtime` is in the Makefile already).
- Frontend commands run under Node 22: `source ~/.nvm/nvm.sh && nvm use 22`.

## Review Focus

1. **No network, or GitHub slow at start.** The start check must never delay or block the window or any other startup work. It runs after the UI is up, its errors are silent, and a 15 s timeout ends it. → Task 5 test `runCheck swallows errors`; Task 1 test `TestLatestTimeout`.
2. **App launched from a translocated or read-only place** (opened from Downloads, so macOS runs it from `/private/var/folders/…/AppTranslocation/…`; a mounted image; `/Applications` not writable). It must offer "Download", never a half-done swap. → Task 3 test `TestCanInstallTranslocatedOrReadOnly`.
3. **Double click on "Update and restart"**, or Prepare while a Prepare is running, must not run two downloads into the same folder. → Task 4 test `TestPrepareBusy`.
4. **Cancel or failure mid-download** leaves the installed app untouched and the temp folder removed; a later retry works. → Task 2 test `TestDownloadCancelCleansUp`; Task 4 test `TestPrepareRetryAfterFailure`.
5. **A manifest that is not newer** (same version, older, or a bundle whose Info.plist version differs from the manifest) must never be offered or installed. → Task 4 test `TestParseManifestNotNewer`; Task 2 test `TestUnpackDarwinVersionMismatch`.

---

### Task 1: `internal/update` — versions, manifest, check

**Files:**
- Create: `internal/update/version.go`, `internal/update/manifest.go`, `internal/update/check.go`
- Test: `internal/update/version_test.go`, `internal/update/manifest_test.go`, `internal/update/check_test.go`
- Create spec: `docs/spec/15-updates.md` (sections "Where the latest version comes from" and "Checking"); add it to `docs/spec/README.md`'s list.

**Interfaces:**
- Produces:
  - `type Version struct{ Major, Minor, Patch int }`; `func ParseVersion(s string) (Version, error)`; `func (v Version) Less(o Version) bool`; `func (v Version) String() string`.
  - `type Asset struct { Name string `json:"name"`; SHA256 string `json:"sha256"` }`
  - `type Manifest struct { Version string `json:"version"`; URL string `json:"url"`; Assets map[string]Asset `json:"assets"` }`
  - `type Release struct { Version string; PageURL string; AssetName string; AssetURL string; AssetSHA256 string }`. The three asset fields are `""` when the manifest has no asset for this platform.
  - `const DefaultManifestURL`, `const ManifestEnv = "COMMITTREE_UPDATE_MANIFEST"`, `const Repo = "josfh2005/CommitTree"`.
  - `func ParseManifest(data []byte, platform, manifestURL string, override bool) (Release, error)` (pure).
  - `type Checker struct { ManifestURL string; Override bool; Platform string; HTTP *http.Client }`
  - `func NewChecker(getenv func(string) string) *Checker` sets `Platform = runtime.GOOS + "-" + runtime.GOARCH`; ManifestURL and Override come from the env var.
  - `func (c *Checker) Latest(ctx context.Context) (Release, error)`
  - `func AllowedHost(u *url.URL) bool`
  - `var ErrInvalidManifest`

- [ ] **Step 1: Write failing tests**
  - `TestParseVersion`: `"0.9.0"` → `{0,9,0}`; `"10.2.33"` ok; errors for `"v0.9.0"`, `"0.9"`, `"0.9.0-beta"`, `""`, `"a.b.c"`.
  - `TestVersionLess`: `0.9.0 < 0.10.0`, `0.10.0 < 1.0.0`, `!(0.9.0 < 0.9.0)`.
  - `TestParseManifest`: a valid manifest with platform `darwin-arm64` gives `AssetURL == "https://github.com/josfh2005/CommitTree/releases/download/v0.10.0/CommitTree-macos-arm64.zip"` and the given sha256 lowercased. The `url` field is kept when it is https on `github.com`; otherwise (e.g. `http://evil.example/x`) `PageURL == "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0"`.
  - `TestParseManifestOverride`: with `manifestURL = "http://127.0.0.1:9/dir/latest.json"` and `override = true`, `AssetURL == "http://127.0.0.1:9/dir/CommitTree-macos-arm64.zip"`. With `file:///tmp/x/latest.json`, it is `file:///tmp/x/CommitTree-macos-arm64.zip`.
  - `TestParseManifestMissingAsset`: platform `linux-arm64` → no error, empty asset fields.
  - `TestParseManifestInvalid`: bad JSON, bad version, sha256 not 64 hex chars, or asset name containing `/` or `..` → `ErrInvalidManifest`.
  - `TestAllowedHost`: `https://github.com/x`, `https://objects.githubusercontent.com/x` and `https://release-assets.githubusercontent.com/x` are allowed. `http://github.com/x`, `https://github.com.evil.io/x` and `https://evilgithubusercontent.com/x` are not.
  - `TestLatestHTTP` (httptest server, `Override: true`): serves a manifest → a `Release`. A 404 → error. A body over 64 KiB → error.
  - `TestLatestFile`: `Override: true` with a `file://` URL to a temp `latest.json` → a `Release`.
  - `TestLatestRedirectBlocked`: `Override: false`, client pointed at an httptest TLS server that redirects to a non-allowed host → error. Inject the server's client via `HTTP`; the allow-list is enforced in `CheckRedirect` on whatever client is used, so wrap it.
  - `TestLatestTimeout`: a handler that sleeps longer than a context deadline of 50 ms → returns `context.DeadlineExceeded` (wrapped) promptly.
- [ ] **Step 2: Run `cd /Users/josfh/playground/git-ui/.claude/worktrees/reverent-roentgen-fd1129 && go test ./internal/update/` → FAIL (undefined).**
- [ ] **Step 3: Implement.** `Latest` does a GET with a 15 s timeout (context, or the client's `Timeout` when `HTTP` is nil), reads at most 64 KiB + 1 byte to detect overflow, and calls `ParseManifest`. For `file://` it reads the file directly (same cap). A nil `HTTP` uses a package client whose `CheckRedirect` refuses non-allowed hosts unless `Override`. When `Override` is false and the manifest URL itself is not allowed, return an error.
- [ ] **Step 4: Run `go test ./internal/update/` → PASS. `go vet ./internal/update/` clean.**
- [ ] **Step 5: Write `docs/spec/15-updates.md`'s first two sections.** Cover the manifest URL, its JSON format, the asset keys, the URL rules, the caps, the version format, the env var, and "no token or identifying data is sent". Add a README entry.
- [ ] **Step 6: Commit** `feat(update): read latest.json and resolve this platform's asset; spec 15`.

### Task 2: `internal/update` — download and unpack

**Files:**
- Create: `internal/update/download.go`, `internal/update/unpack.go`
- Test: `internal/update/download_test.go`, `internal/update/unpack_test.go` (darwin-only parts behind `runtime.GOOS == "darwin"` skips)
- Modify spec: `docs/spec/15-updates.md` (section "Downloading")

**Interfaces:**
- Consumes: `Release`, `Checker` (Task 1).
- Produces:
  - `func (c *Checker) Download(ctx context.Context, r Release, dir string, progress func(done, total int64)) (string, error)`. Returns the archive path `dir/<AssetName>`. `total` is `-1` when unknown. The partial file is removed on any error.
  - `var ErrChecksum, ErrTooLarge, ErrStalled, ErrNoAsset`
  - `func Unpack(archive, dir, goos string) (string, error)`. On darwin it returns the path of `dir/CommitTree.app`; on linux, the path of the extracted `dir/CommitTree` binary (mode 0755).
  - `func BundleVersion(app string) (string, error)` (darwin: `/usr/bin/plutil -extract CFBundleShortVersionString raw -o - <app>/Contents/Info.plist`)
  - `var ErrBadArchive, ErrVersionMismatch`
  - `func UnpackChecked(archive, dir, goos, want string) (string, error)`: `Unpack` plus, on darwin, `BundleVersion == want` or `ErrVersionMismatch`.

- [ ] **Step 1: Write failing tests**
  - `TestDownloadOK`: an httptest server serves bytes whose sha256 is in the `Release`. The file exists with those bytes, and `progress` was called with a final `done == len`.
  - `TestDownloadChecksumMismatch` → `ErrChecksum`, no file left in `dir`.
  - `TestDownloadTooLarge`: set a package-level `maxDownload` var to 1 KiB for the test, serve 2 KiB → `ErrTooLarge`, no file left.
  - `TestDownloadStalled`: set a package-level `stallTimeout` var to 100 ms; the handler writes a few bytes, flushes, then sleeps 1 s → `ErrStalled`.
  - `TestDownloadCancelCleansUp`: cancel ctx mid-body → `context.Canceled`, no file left.
  - `TestDownloadNoAsset`: empty `AssetURL` → `ErrNoAsset`.
  - `TestDownloadFile`: `file://` asset (override) works.
  - `TestUnpackLinux`: a tar.gz built in the test holding `CommitTree` → the extracted file has the same bytes and mode 0755.
  - `TestUnpackLinuxEscape`: a tar entry named `../CommitTree` or `/etc/x`, a symlink entry, or no `CommitTree` entry → `ErrBadArchive`.
  - `TestUnpackDarwin` (darwin only): build a fake `CommitTree.app/Contents/Info.plist` (with `CFBundleShortVersionString` 0.10.0) in a temp dir, zip it with `/usr/bin/ditto -c -k --keepParent`, then `UnpackChecked(..., "0.10.0")` → the path ends in `CommitTree.app`.
  - `TestUnpackDarwinVersionMismatch` (darwin only): the same, with want `0.11.0` → `ErrVersionMismatch`.
  - `TestUnpackDarwinNoBundle` (darwin only): a zip without `CommitTree.app` → `ErrBadArchive`.
- [ ] **Step 2: Run `go test ./internal/update/` → FAIL.**
- [ ] **Step 3: Implement.** Download with a client without a total `Timeout`. Use a reader wrapper that resets a timer on each read and cancels the request context when it fires; map that to `ErrStalled`. Hash while writing (`io.MultiWriter`). Compare hex case-insensitively. On darwin, unpack with `exec.Command("/usr/bin/ditto", "-x", "-k", archive, dir)`. On linux use `archive/tar` + `compress/gzip`, take only a regular-file entry whose cleaned name is exactly `CommitTree`, and cap its size at `maxDownload`.
- [ ] **Step 4: Run `go test ./internal/update/` → PASS.**
- [ ] **Step 5: Spec section "Downloading"**: checksum, caps, stall rule, cleanup, and the Info.plist check.
- [ ] **Step 6: Commit** `feat(update): download with checksum and stall guard, unpack macOS bundle and Linux binary; spec 15`.

### Task 3: `internal/update` — where to install, swap, relaunch

**Files:**
- Create: `internal/update/install.go`, `internal/update/devbuild.go` (`//go:build dev`, `const DevBuild = true`), `internal/update/devbuild_off.go` (`//go:build !dev`, `const DevBuild = false`)
- Test: `internal/update/install_test.go`
- Modify spec: `docs/spec/15-updates.md` (sections "Installing in place" and "When it cannot install"; include the keychain note from the design)

**Interfaces:**
- Produces:
  - `func Target(exe, goos string) (string, error)`. On darwin it returns the nearest ancestor named `*.app` that contains `Contents/MacOS/` on the path to `exe`, or `ErrNotBundle`. On linux it returns `exe`.
  - `type Installability struct { Target string; CanInstall bool; Reason string }`
  - `func Check(exe, goos string, dev bool, hasAsset bool) Installability`. `Reason` is one of: `"development build"`, `"not running from an app bundle"`, `"no download for this platform"`, `"CommitTree can't write to <dir>"`, `"running from a temporary location — move CommitTree to Applications first"` (path contains `/AppTranslocation/`), or `""`.
  - `func Swap(target, staged, goos string) error`
  - `func RelaunchCommand(target, goos string, pid int) (name string, args []string)`
  - `func Relaunch(target, goos string) error` (starts `RelaunchCommand` detached with `SysProcAttr{Setsid: true}` and returns without waiting)
  - `var ErrNotBundle`

- [ ] **Step 1: Write failing tests**
  - `TestTargetDarwin`: `/Applications/CommitTree.app/Contents/MacOS/CommitTree` → `/Applications/CommitTree.app`. `/usr/local/bin/CommitTree` → `ErrNotBundle`.
  - `TestTargetLinux`: returns `exe` unchanged.
  - `TestCanInstallTranslocatedOrReadOnly`:
    - A path under `/private/var/folders/x/AppTranslocation/ABC/d/CommitTree.app/...` → `CanInstall == false` with the temporary-location reason.
    - A target whose parent dir is `chmod 0555` (temp dir; skip when running as root) → false with the "can't write" reason naming the dir.
    - `dev == true` → false, `"development build"`.
    - `hasAsset == false` → false.
    - A writable temp bundle → true, `Reason == ""`.
  - `TestSwapLinux`: target file "old", staged file "new" → the target holds "new" with mode 0755 and no leftover files in its dir.
  - `TestSwapDarwin` (works on any OS; it only uses renames): temp `X/CommitTree.app` (holding `old`) plus a staged `S/CommitTree.app` (holding `new`). After the swap, `X/CommitTree.app/new` exists, `old` is gone, and no `.CommitTree.app.old-*` remains.
  - `TestSwapDarwinRollback`: the staged path does not exist → error, and `X/CommitTree.app/old` is still there.
  - `TestRelaunchCommand`: darwin → `name == "/bin/sh"`, and the args contain `kill -0 <pid>` and `/usr/bin/open -n '<target>'` (single-quote escaped). linux → `/bin/sh`, waiting on the pid, then exec of `'<target>'`.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement.**
  - The writability probe is `os.CreateTemp(dir, ".committree-write-*")` followed by removal.
  - The darwin swap renames the old bundle to `<dir>/.CommitTree.app.old-<pid>`, then renames staged → target. If that rename fails with `EXDEV`, it copies with `/usr/bin/ditto staged <dir>/.CommitTree.app.new-<pid>` and renames that. On failure it renames the old bundle back. On success it runs `os.RemoveAll` on the old bundle and ignores that error.
  - The linux swap copies staged to `<dir>/.CommitTree.new-<pid>`, runs `chmod 0755`, then `os.Rename` over the target.
- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Spec sections.**
- [ ] **Step 6: Commit** `feat(update): find the install target, swap in place with rollback, relaunch; spec 15`.

### Task 4: Wails bindings in `internal/app/update.go`, version from `wails.json`

**Files:**
- Create: `internal/app/update.go`, `internal/app/update_test.go`
- Modify: `internal/app/app.go` (an `upd *updateState` field on `App`), `main.go` (`//go:embed wails.json`, parse `info.productVersion`, call `app.WithUpdates`)
- Regenerate + commit: `frontend/wailsjs/go/app/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts` (via `make build` or `wails generate module`)
- Modify spec: `docs/spec/15-updates.md` (section "Installing": the Prepare/Apply split, errors as the user reads them)

**Interfaces:**
- Consumes: everything from Tasks 1–3.
- Produces (bound to the frontend):
  - `type UpdateInfo struct { Current string `json:"current"`; Latest string `json:"latest"`; Newer bool `json:"newer"`; PageURL string `json:"pageURL"`; CanInstall bool `json:"canInstall"`; Reason string `json:"reason"` }`
  - `func (a *App) CheckForUpdate() (UpdateInfo, error)`
  - `func (a *App) PrepareUpdate() error`. It blocks until download + unpack are done and emits `update:progress` with `UpdateProgress{ Phase string `json:"phase"` ("download" | "install"); Done, Total int64 }`.
  - `func (a *App) CancelUpdate()`
  - `func (a *App) ApplyUpdate() error`. It swaps, relaunches, then calls `runtime.Quit(a.ctx)`.
  - `func WithUpdates(a *App, o UpdateOptions)`; `type UpdateOptions struct { Version string; Source updateSource; Exe string; GOOS string; Dev bool }`
  - The unexported `type updateSource interface { Latest(ctx) (update.Release, error); Download(ctx, update.Release, dir string, progress func(int64, int64)) (string, error) }` is satisfied by `*update.Checker`.
  - `var ErrUpdateBusy, ErrNothingPrepared, ErrUpdatesOff` (the last when `WithUpdates` was never called).

- [ ] **Step 1: Write failing tests** (fake `updateSource`; `Exe` pointing into a temp bundle; `GOOS` set to `runtime.GOOS` for the unpack path; `Unpack` and `Swap` injectable via fields on `updateState` so the tests don't need ditto)
  - `TestCheckForUpdateNewer`: current `0.9.0`, latest `0.10.0` → `Newer == true`, `CanInstall == true`.
  - `TestParseManifestNotNewer` (named as in the Review Focus): latest `0.9.0` or `0.8.5` with current `0.9.0` → `Newer == false`. An invalid current version → error.
  - `TestCheckForUpdateCannotInstall`: `Dev: true` → `CanInstall == false`, `Reason == "development build"`.
  - `TestPrepareBusy`: the first Prepare blocks inside a fake Download; a second call returns `ErrUpdateBusy`.
  - `TestPrepareRetryAfterFailure`: the first Download fails, then Prepare succeeds. The staging temp dir from the failure is gone.
  - `TestPrepareEmitsProgress`: the events are captured through `a.emit`. Use the existing test emitter pattern in `internal/app`; see `cmdlog_test.go` for how events are captured before/after Startup.
  - `TestApplyWithoutPrepare` → `ErrNothingPrepared`.
  - `TestCancelUpdate`: Cancel during the fake Download → Prepare returns `context.Canceled`.
  - User-facing error text: wrap `ErrChecksum` as "The download didn't match the expected checksum", `ErrStalled`/network errors as "Couldn't download the update", and `ErrVersionMismatch` / `ErrBadArchive` as "The downloaded update isn't valid". Assert via `err.Error()`.
- [ ] **Step 2: Run `go test ./internal/app/ -run Update` → FAIL.**
- [ ] **Step 3: Implement.**
  - `CheckForUpdate` uses a 15 s context.
  - Prepare creates `os.MkdirTemp("", "committree-update-*")` and remembers the staged path. It removes the dir on failure and also when a later Prepare replaces it.
  - `ApplyUpdate` checks the version again against the current one before swapping.
  - In `main.go`, the embedded `wails.json` is unmarshalled into `struct{ Info struct{ ProductVersion string `json:"productVersion"` } `json:"info"` }`. `Exe` comes from `os.Executable` + `EvalSymlinks` (reuse the value main already computes for credentials). `Dev` is `update.DevBuild`.
- [ ] **Step 4: Run `go test ./...` → PASS. Regenerate the bindings and check that `frontend/wailsjs/go/app/App.d.ts` lists the four methods.**
- [ ] **Step 5: Spec section.**
- [ ] **Step 6: Commit** `feat(app): CheckForUpdate / PrepareUpdate / CancelUpdate / ApplyUpdate bindings, version from wails.json; spec 15`.

### Task 5: Frontend logic — `lib/update.ts`

**Files:**
- Create: `frontend/src/lib/update.ts`, `frontend/src/lib/update.test.ts`
- Modify: `frontend/src/lib/types.ts` (add `UpdateInfo`, `UpdateProgress` matching Task 4's JSON), `frontend/src/lib/api.ts` (`checkForUpdate`, `prepareUpdate`, `cancelUpdate`, `applyUpdate`), `frontend/src/lib/stores.ts` (add `autoUpdateCheck = persisted('autoUpdateCheck', true, isBool)` and `commitDraft = writable('')`)
- Modify spec: `docs/spec/15-updates.md` (section "When the card and the pill show")

**Interfaces:**
- Consumes: `api.checkForUpdate(): Promise<UpdateInfo>` etc.; `taskState` and `visibleTasks` from `lib/tasks.ts`; `chatRunRepo` from `lib/stores.ts`.
- Produces:
  - `export type CheckTrigger = 'start' | 'focus' | 'manual'`
  - `export const FOCUS_SPACING_MS = 6 * 60 * 60 * 1000`
  - `export function shouldCheck(trigger: CheckTrigger, auto: boolean, lastCheck: number | null, now: number): boolean`
  - `export function shouldShowCard(trigger: CheckTrigger, info: UpdateInfo, shown: readonly string[]): boolean`
  - `export function restartBlockers(tasks: { label: string }[], chatRunRepo: string, commitDraft: string): string[]` returns one reason per blocker: `"<task label> is running"`, `"The chat is answering"`, `"The commit message has unsaved text"`.
  - Stores: `updateInfo = writable<UpdateInfo | null>(null)`, `updateCardOpen = writable(false)`, `updatePhase = writable<{ phase: 'idle' | 'download' | 'install' | 'ready' | 'error'; done: number; total: number; error: string }>(…idle)`, `manualCheckResult = writable<string>('')`.
  - `export function createUpdateController(deps: { check: () => Promise<UpdateInfo>; now: () => number; auto: () => boolean }): { run(trigger: CheckTrigger): Promise<void> }`. It keeps `lastCheck` and `shown` in closure memory, and sets `updateInfo`, `updateCardOpen` and (for manual) `manualCheckResult`.
  - `export async function startUpdate(deps: { prepare; apply; confirm: (reasons: string[]) => Promise<boolean>; blockers: () => string[] }): Promise<void>`. It drives `updatePhase` and calls `confirm` only when `blockers()` is non-empty, after prepare and before apply.

- [ ] **Step 1: Write failing tests** (`update.test.ts`)
  - `shouldCheck`:
    - start → `auto`.
    - focus → false when `!auto`; true when `lastCheck == null`; false at `now - lastCheck = FOCUS_SPACING_MS - 1`; true at `== FOCUS_SPACING_MS`.
    - manual → true even when `!auto`.
  - `shouldShowCard`:
    - `!info.newer` → false for every trigger.
    - start → true even when `shown` has the version.
    - focus → false when `shown` includes `info.latest`, true otherwise.
    - manual → true.
  - Controller:
    - Start with newer → `updateCardOpen` true and `updateInfo` set.
    - A following focus within 6 h → no `check` call.
    - A focus after 6 h finding the same version → card not reopened (close it first).
    - A focus finding `0.11.0` → card opens.
    - A manual check showing `0.10.0`, then a focus finding `0.10.0` → not reopened ("Check now counts as a showing").
    - Manual results: `"You're up to date"` when not newer, `"{latest} is available"` when newer, and the error's message when it throws.
  - `runCheck swallows errors`: `check` rejects on start/focus → no throw, `updateInfo` unchanged, `lastCheck` still advances.
  - `restartBlockers`:
    - `[]` when all are empty.
    - A whitespace-only draft is not a blocker.
    - All three reasons, in that order.
  - `startUpdate`:
    - With no blockers it calls apply without confirm.
    - With blockers and confirm false, it never calls apply and `updatePhase` goes back to `'ready'`.
    - A rejected prepare sets `phase 'error'` with the message.
- [ ] **Step 2: Run `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/update.test.ts` → FAIL.**
- [ ] **Step 3: Implement.** No Svelte imports beyond `svelte/store`.
- [ ] **Step 4: Run the test → PASS; run `npx vitest run` (whole suite) and `npx svelte-check` → no new errors.**
- [ ] **Step 5: Spec section** with the owner's rules, word for word in meaning (always at start; on focus each version once per run; pill always; ✕ only closes; Check now counts).
- [ ] **Step 6: Commit** `feat(ui): update check and card rules; spec 15`.

### Task 6: Frontend UI — card, pill, sidebar, Settings, wiring

**Files:**
- Create: `frontend/src/components/UpdateCard.svelte`
- Modify:
  - `frontend/src/App.svelte`: mount `<UpdateCard />` next to `<Toasts />`. In `onMount`, create the controller, `run('start')`, add `window.addEventListener('focus', …)` → `run('focus')` (removed on destroy), and subscribe to `EventsOn('update:progress', …)` → `updatePhase`.
  - `frontend/src/components/StatusBar.svelte`: the pill at the far right, after "+N". It shows when `$updateInfo?.newer`; a click sets `updateCardOpen` true; `title="CommitTree {latest} is available"`.
  - `frontend/src/components/Sidebar.svelte:247`: when `$updateInfo?.newer`, the `.version` span reads `v{current} → {latest}` with accent colour.
  - `frontend/src/components/SettingsDialog.svelte`: in the General tab, a new `<section>` "Updates" after the existing sections, holding the checkbox bound to `$autoUpdateCheck`, the "CommitTree {__APP_VERSION__}" line, the "Check now" button (disabled while checking) and `$manualCheckResult`.
  - `frontend/src/components/CommitBox.svelte`: `$: commitDraft.set(message)`.
- Modify spec: `docs/spec/15-updates.md` (section "What the user sees"); `docs/spec/13-status-bar.md` (the pill in "Layout", as a new last item); `docs/spec/07-conventions-and-constraints.md` (the version-label highlight at the paragraph "The sidebar's Settings row shows the app's version", ~line 171, and "Updates" in the General tab's list, ~line 184).

**Interfaces:**
- Consumes: the Task 5 stores and functions, plus `confirmDialog` from `lib/ui.ts` (`{ title: 'Restart CommitTree?', message: reasons.join('\n'), confirmLabel: 'Restart anyway' }`) and `BrowserOpenURL` from `wailsjs/runtime/runtime`.
- Card look: fixed at the bottom right like `.toasts` (`right: 16px; bottom: 38px`), z-index just above toasts. Use the toasts' surface, border and shadow, a 4 px `var(--accent)` left border, and `max-width: 340px`. While toasts are visible, stack the card above them; if that is hard, put the card at a higher `bottom`. Builder's call, as long as neither covers the other.
- Card states: idle (the two buttons), downloading ("Downloading 42 %", or a byte count when the total is unknown, plus Cancel), installing ("Installing…"), error (the one-line message + "Open release page"). When `!canInstall`, the primary button is "Download" with a muted line giving `reason`.
- Pill: accent background, white text, `⬆ Update`, rounded, the status bar's font size; it follows Dark and High contrast through tokens. Add a token pair if `--accent` on white fails contrast in the dark theme. Check `theme.css` dark values.

- [ ] **Step 1: Write a failing vitest** for any new pure helper the components need, e.g. `progressText(done, total): string` in `update.ts` → `"Downloading 42 %"`, `"Downloading 3.1 MB"` when `total < 0`. Leave component markup untested, as elsewhere in the repo.
- [ ] **Step 2: Implement the components and wiring.**
- [ ] **Step 3: Run `npx vitest run` and `npx svelte-check` → pass with no new warnings.**
- [ ] **Step 4: `make build` (Node 22) → OK. Launch `build/bin/CommitTree.app` with `COMMITTREE_UPDATE_MANIFEST` pointing to a `file://` manifest claiming `99.0.0` (no asset needed). Confirm that the card shows at start, the pill shows, the sidebar is highlighted, ✕ closes the card and the pill stays, focusing again does not reopen it, and "What's new" / "Download" open the page. Report what was checked. Revert unrelated `frontend/wailsjs` churn.**
- [ ] **Step 5: Spec sections.**
- [ ] **Step 6: Commit** `feat(ui): update card, status-bar pill, sidebar highlight and Settings Updates section; spec 07, 13, 15`.

### Task 7: Release manifest generator

**Files:**
- Create: `cmd/release-manifest/main.go`, `cmd/release-manifest/main_test.go`
- Modify: `Makefile` (target `release-manifest: ; go run ./cmd/release-manifest -dir build/bin -wails wails.json`, added to `.PHONY`), `docs/development.md` ("Releases": the hand recipe and the new step "attach `build/bin/latest.json`"), `docs/spec/15-updates.md` (one line on how releases publish it)

**Interfaces:**
- Consumes: `update.Manifest`, `update.Asset`, `update.ParseVersion`, `update.Repo`.
- Produces: `func build(dir, wailsPath string) (update.Manifest, error)` (in package main, tested), which writes `dir/latest.json` (indented JSON, trailing newline).

- [ ] **Step 1: Write failing tests**
  - Temp dir with the three files and a `wails.json` at `0.10.0` → manifest with the three keys, correct sha256s, `url == "https://github.com/josfh2005/CommitTree/releases/tag/v0.10.0"`.
  - One file missing → error naming it.
  - A bad version in `wails.json` → error.
  - The result passes `update.ParseManifest` for each platform.
- [ ] **Step 2: Run `go test ./cmd/release-manifest/` → FAIL.**
- [ ] **Step 3: Implement** with the asset names fixed: `darwin-arm64: CommitTree-macos-arm64.zip`, `darwin-amd64: CommitTree-macos-amd64.zip`, `linux-amd64: CommitTree-linux-amd64.tar.gz`.
- [ ] **Step 4: Run → PASS; `go test ./...` → PASS.**
- [ ] **Step 5: Docs.** In `docs/development.md`, keep the workflow paragraph but say it has never run and that releases are made by hand. Add the hand steps: Intel build + zip, `make build` + zip, `make build-linux`, `make release-manifest`, then `gh release create … build/bin/latest.json` with the three assets.
- [ ] **Step 6: Commit** `build: make release-manifest writes latest.json for the update check; spec 15`.
