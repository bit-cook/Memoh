# Agent CLI storage implementation validation

Date: 2026-09-14. Status: implementation and validation in progress.

This record accompanies the [implementation plan](agent-cli-storage-and-single-install-plan.md). Results below describe completed checks, not release approval. Human QA has not been performed.

## Local environment

- OSS implementation baseline: `58741b308`; branch `codex/agent-cli-local-store`.
- Development stack: `devenv/docker-compose.yml`, with an ignored local configuration selecting `container.dependency_store_root = "/var/lib/memoh/deps"` and the local Supermarket at `http://host.docker.internal:5175`.
- Server: `http://localhost:18080`; Web: `http://localhost:18082`; local Supermarket: `http://localhost:5175`.
- Workspace: Linux ARM64, glibc, containerd, image `docker.io/memohai/workspace:debian`, image digest `sha256:36bb5415bd4228aac81b114dd3a33d00ac3d4dd9b1d1077c85adbc83ad7b04d5`.
- Disposable Bot: `agent-cli-storage-qa-20260914` (`2fe80839-9793-455a-bcea-4f5b740fb4a9`). Existing Bots and their volumes were not rebuilt.

## Installation and rootfs reconstruction

The actual Web flow prepared and confirmed Codex `0.154.0` and Claude Code `2.1.270`, including their frozen recipe revisions and automatic-repair consent. Both App progress dialogs reached completion; the Claude run included blank npm output without a false disconnect. The backend committed a new payload under `/var/lib/memoh/deps/codex/installs/<operation-id>` and durable metadata under `/data/.memoh/deps`.

The Node App linked the existing image Node without granting new recovery authority. An explicit management operation then installed Node `24.4.1`, distinct from the image's `24.14.0`, and pnpm `12.4.1`.

The reconstruction check called the real container DELETE API with `preserve_data=true`, verified the container no longer existed in containerd, and recreated it through the real API with data restoration. A marker outside `/data` disappeared. No install or retry request was sent after recreation; the workspace-ready worker restored the authorized dependencies.

| Dependency | Before deletion | After automatic repair | Result |
| --- | --- | --- | --- |
| Codex | `0.154.0`, local payload | `0.154.0`, new installation ID | CLI version command succeeded |
| Node | `24.4.1`, overriding image `24.14.0` | `24.4.1`, new installation ID | No substitution with image version |
| pnpm | `12.4.1`, local payload | `12.4.1`, new installation ID | CLI version command succeeded |
| Claude Code | `2.1.270`, local payload | `2.1.270`, new installation ID | CLI version command succeeded |

The final four-dependency run used the rebuilt local-control image. All four desired versions, recipe revisions, authorization revisions, and original authorization times remained unchanged. A Server hot reload interrupted a GET observation; reconnecting observation confirmed all four automatic operations completed, without another install or retry request. The final managed Codex launcher uses `/run/memoh/deps/.leases`. A test-only ordinary `auth.json` under `/data/.codex/agents/storage-qa` retained its SHA-256 hash and file mode. This demonstrates file persistence; it does not establish successful OAuth refresh or authenticated model calls.

The earlier three-dependency run observed all dependencies ready 48.8 seconds after container recreation returned. This is one local sample, not a performance distribution or an E2B/NFS benchmark. It excludes authenticated initialization and a model turn.

The first run exposed a legacy pnpm failure: archive restoration preserved metadata and directories but omitted package symlinks. The old frozen recipe was not silently replaced. After an explicit management operation selected a newly published isolated recipe, the second full reconstruction succeeded. The shared generated recipes now support isolated publication and custom command/import health probes; bundle resolution metadata remains persistent and must match its requested package-map digest.

## Defects found during real validation

- A real Cloud E2B workspace mounted `/data` over NFSv3. A dependency kernel lock on that volume hung in `rpc_wait`, while a lock on local storage completed immediately. Linux transaction, lease, and startup-window locks now use the fixed local `/run/memoh/deps` control root, independent of the configured payload root. Durable receipts and state remain under `/data`. E2B replaces `/run` during boot, so its template startup must create the control directory before declaring readiness. The final OSS four-dependency reconstruction passed with this control layout; integrated Cloud repair is tracked below. OSS bind-mounts its host runtime directory at `/run/memoh`, so the actual control filesystem must be local and writable, and these files can outlive a rootfs. Safety depends on kernel locks and the complete workspace epoch, not on deleting the control files.
- The first live same-version replacement kept an initialized Codex app-server responsive and retained its old payload, but stop/start did not invoke maintenance. After correcting the ordinary restart path, the final live check passed: the old app-server answered `config/read` after reinstall, the old payload remained both during execution and after process exit, and a complete stop/start collected only the retired payload while the current launcher remained usable.
- Blank npm output was serialized without an SSE `data` string, causing the App client to report a connection interruption despite a successful backend install. Empty log lines now retain the field.
- Readiness used a different version parser from installation and rejected Node's `v` prefix. Both boundaries now use the frozen recipe's probe, or the standard executable version probe when no custom probe exists.
- Recipes without custom probes previously lacked a Server-side candidate execution check. Candidate validation now checks the executable before committing.
- Recovery previously trusted mutable workspace receipt fields for the requested version, action and authorization identity. A database-owned operation intent now binds these fields when the operation is claimed; an unmatched or legacy receipt cannot create authorization.
- Frozen repair graphs now use stored approved definitions; an empty or changed current catalog cannot substitute another prerequisite graph. Preparation failures compare the originally observed status so an old observer cannot overwrite a peer's running operation.
- The Apps detail query could retain an old repair failure after the worker recovered the dependency. Visible-page polling and activation/navigation refresh now read actual backend state without authorizing installation.

Screenshots and a sanitized machine-readable outcome are available in [the evidence directory](../evidence/agent-cli-storage/README.md).

## HTTP authorization and query contracts

A separate local test principal received only `workspace_read` on the disposable Bot. The final 47-scenario real HTTP run passed after the lifecycle and control changes: unauthenticated requests returned 401; management operations returned 403 for the read-only principal; List, refresh and Preflight remained readable; malformed or incomplete confirmation targets returned 400; an unavailable immutable publication returned 503. Valid exact preparation and repair preparation returned 200 without starting operations. The removed Rollback endpoint returned 404 and script preview rejected the removed action.

Installation operation fields, the desired targets and authorization-event counts were compared before and after each request and remained unchanged. The temporary grant was removed and its membership deactivated. Launcher resolution has no public HTTP endpoint, so its non-installing behavior is tested at the Go boundary rather than claimed as HTTP coverage.

## Native state decision

The [Codex native-state evaluation](agent-cli-native-state-evaluation.md) contains the completed directory and state-loss experiments. Moving all native SQLite state to ephemeral storage lost goal state after rootfs replacement. Therefore this implementation retains the persistent Codex Home and ordinary `auth.json`; it does not enable ephemeral `sqlite_home` globally.

## Cloud and E2B

A dedicated E2B template (`1rjub6rv8z1t6sax5nug`, alias `memoh-agent-cli-storage-test-20260914`) was built without changing production defaults. The gated volume-rebuild mechanism test passed in 21.85 seconds and deleted its temporary sandboxes and volume. That test uses a synthetic payload; it is not evidence of integrated Memoh repair or authenticated CLI performance on E2B. The later integrated Cloud run installed Codex successfully, but native app-server startup on NFS is still blocked by Codex 0.154.0 taking a lock under `CODEX_HOME/tmp/arg0`. The same installed binary initializes with a disposable local Home. This is not fixed by the dependency control-root change; the persistent-Home runtime remains unverified on E2B. Full Cloud acceptance is tracked separately until completed.

## Remaining acceptance work

- Validate authenticated Codex and Claude turns with authorized test credentials, including persistence and resume.
- Complete Cloud adapter tests and real development API/UI verification.
- Submit and check the related draft PRs, keeping Human QA unchecked. Current screenshots and sanitized results are versioned with the implementation.

## Repository checks

The seven benchmark protocol tests pass. The full `mise run lint` currently stops at the pre-existing `animate-spin` UI-contract violation in `apps/web/src/pages/home/components/tool-call-diff-panel.vue:6`; that file matches the implementation baseline and is unchanged by this task. Full ESLint initially scanned an unrelated nested `.claude/worktrees` checkout and crashed because that checkout uses an older parser. The main repository passed `pnpm exec eslint . --ignore-pattern '.claude/**'`: zero errors and three unchanged test warnings. The 34 changed Web files passed scoped lint without warnings. No dependency reinstall or unrelated source change was needed. The changed Web test suite passed 99 tests across 11 files after regenerating the SDK; SDK type checking passed. Full Web type checking remains blocked by pre-existing UI package alias errors. The final affected Go runtime and workspace suites passed. The final old-intent migration additions also passed scoped Linux lint. Linux race tests covered dependency execution and bridge admission; full PostgreSQL integration passed with 256 passing test events, zero failures and zero skips, and the desired-state race run also passed. `GOOS=linux CGO_ENABLED=0 mise run lint:go` passed in 216.82 seconds for the complete Linux source tree.

The first commit hook run hit the unchanged `TestIdleTimeoutToolCallRearmsCurrentWindow` timing test (a 50 ms sleep against an 80 ms deadline) under parallel checks. The isolated test then passed 20 consecutive runs; the complete commit hook then passed without changing that unrelated test.

## Related contributions

Supermarket draft PR [#26](https://github.com/felinics/supermarket/pull/26) has passed both CI checks and contains the 32 recipe updates, generated lock and versioned screenshots from the real Memoh installation flow. It does not deploy the registry.
