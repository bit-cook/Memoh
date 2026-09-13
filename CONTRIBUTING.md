# Contributing Guide

For issue and PR templates, labels, and automated checks, see [Issues and Pull Requests](#issues-and-pull-requests).

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) for the containerized dev environment
- [mise](https://mise.jdx.dev/) for toolchain and task management

### Install mise

```bash
# macOS / Linux
curl https://mise.run | sh
# or
brew install mise

# Windows
winget install jdx.mise
```

## Quick Start

```bash
mise install       # Install toolchains (Go, Node, pnpm, sqlc, golangci-lint)
mise run setup     # Install dependencies and run code generation
mise run dev       # Start the full containerized dev environment
```

`mise run dev` launches the development stack in Docker:

1. PostgreSQL infrastructure
2. Database migrations, run automatically on startup
3. Connect-It API and admin UI
4. Go server with the in-process AI agent and containerd workspace backend
5. Web frontend with Vite hot reload

The dev stack uses `devenv/app.dev.toml` directly and does not overwrite the repo root `config.toml`.
Default host ports are shifted away from the production compose stack: Web `18082`, API `18080`, Connect-It `18421`, Postgres `15432`.

### Connect-It

The dev Compose files pull the pinned multi-architecture image
`ghcr.io/felinics/connect-it:0.3.0`; no Connect-It source checkout is required.
The image serves the API, MCP endpoint, and admin UI from one container.
Connect-It uses the same `memoh` PostgreSQL database as Memoh, with
its independently migrated tables isolated in the `connect_it` schema. The
schema is owned, created, and migrated by the Connect-It server itself on
startup; Memoh's migrations never touch it.

`mise run dev` shares a fixed development bootstrap token between both sides:
the Connect-It container seeds it as an API token at startup
(`CONNECT_IT_BOOTSTRAP_API_TOKEN`) and the Memoh server presents it as its
bearer token. The Connectors tab is therefore available from the first
startup, with no token minting or caching involved. The admin UI remains
available at `http://localhost:18421` with the development credentials
`admin` / `admin123`. The Connect-It port binds to loopback only: the token
is public, so the deployment must not be reachable from the LAN. If the
bootstrap token was revoked through the admin UI, rotate it once with
`MEMOH_CONNECT_IT_API_TOKEN=cit_<64 hex chars> mise run dev` — the new value
is seeded as a fresh token and the revoked one stays revoked.

Override the host UI port with `MEMOH_DEV_CONNECT_IT_PORT`, the public OAuth callback
address with `MEMOH_DEV_CONNECT_IT_BASE_URL`, or point at an external Connect-It
deployment with `MEMOH_CONNECT_IT_BASE_URL` and `MEMOH_CONNECT_IT_API_TOKEN`. For
image testing, override `MEMOH_DEV_CONNECT_IT_IMAGE`.

## Daily Development

```bash
mise run dev                              # Start all services
mise run dev:selinux                      # Start all services on SELinux hosts
mise run dev:down                         # Stop the dev stack
mise run dev:logs                         # View dev logs
mise run dev:restart -- server            # Restart a specific service
mise run bridge:build                     # Rebuild the bridge binary in the dev container
```

## More Commands

| Command | Description |
| ------- | ----------- |
| `mise run setup` | Install dependencies and prepare local tooling |
| `mise run dev` | Start the containerized PostgreSQL dev environment |
| `mise run dev:down` | Stop the dev environment |
| `mise run dev:logs` | View dev logs |
| `mise run dev:restart -- server` | Restart a specific dev service |
| `mise run bridge:build` | Rebuild the workspace bridge binary in the dev container |
| `mise run db-up` | Run database migrations |
| `mise run db-down` | Roll back database migrations |
| `mise run swagger-generate` | Generate Swagger/OpenAPI documentation |
| `mise run sdk-generate` | Generate the TypeScript SDK |
| `mise run sqlc-generate` | Generate Go SQL code |
| `mise run lint` | Run Go and TypeScript linters |

## Project Layout

```
conf/       - Configuration templates (app.example.toml, app.docker.toml)
devenv/     - Dev environment (docker-compose, dev Dockerfiles, app.dev.toml, bridge-build.sh)
docker/     - Production Docker build and runtime (Dockerfiles, entrypoints)
cmd/        - Go application entry points
internal/   - Go backend core code
apps/       - Application services
  web/      - Vue 3 management UI
  desktop/  - Electron desktop shell
packages/   - Frontend monorepo packages (ui, sdk, icons, config)
db/         - Database migrations and queries
scripts/    - Utility scripts
```

## Testing

Run focused tests before opening a pull request:

```bash
go test ./internal/channel/adapters/dingtalk
go test ./internal/...
pnpm test --run
pnpm --filter @memohai/desktop typecheck
mise run lint
```

For frontend-only changes, `pnpm lint` and `pnpm test --run` are usually enough. For desktop changes, also run `pnpm --filter @memohai/desktop typecheck`.

## SQL, API, and SDK Changes

Database changes target PostgreSQL:

1. Update `db/postgres/...`.
2. Add the next PostgreSQL incremental migration pair when the schema changes.
3. Update the canonical PostgreSQL baseline migration.
4. Run `mise run sqlc-generate`.

API handler changes should update the OpenAPI spec and generated SDK:

```bash
mise run swagger-generate
mise run sdk-generate
```

Generated files under `internal/db/postgres/sqlc/` and `packages/sdk/` should be committed only when they result from the matching SQL or API source changes.

## Windows Notes

The repository supports Windows development, but many `mise` tasks use bash-style scripts. Running them from Git Bash, WSL, or a Docker-backed shell is the smoothest path. Plain PowerShell is still useful for Go package tests and file inspection, but tasks that invoke shell scripts may need a POSIX-compatible shell.

If you have local work in progress that should not be included in a pull request, stage only the intended files:

```bash
git add path/to/intended-file.go path/to/intended-test.go
git status --short
```

## Issues and Pull Requests

Contributors can submit an issue or PR before format checks run. GitHub Actions validates the description, synchronizes labels, and requests corrections. Eligible PR lint, test, and build jobs run only after the format check passes. A separate controller approves pending first-time contributor workflow runs automatically. Workflow approval does not approve a code review, merge, or release.

### Label Configuration

`.github/labels.json` is the source of truth for label names, colors, and English descriptions. GitHub does not read this file natively. The `Sync labels` workflow applies changes from main through the GitHub API. Routine synchronization creates or updates labels; it never deletes them.

```sh
# Preview the configuration without changing GitHub
node .github/scripts/sync-labels.mjs felinics/Memoh
# Apply the configuration
node .github/scripts/sync-labels.mjs felinics/Memoh --apply
```

The repository maintains these 14 labels:

| Group | Labels | Color |
| --- | --- | --- |
| Type | `bug` | `D73A4A` |
| Type | `feat` | `2DA44E` |
| Type | `test` | `8250DF` |
| Type | `help` | `0E8A8A` |
| Size | `size:XS`, `size:S`, `size:M`, `size:L`, `size:XL` | `0969DA` for all sizes |
| Scope | `change:web`, `change:desktop`, `change:migrations`, `change:server` | `D4C5F9` for all scopes |
| Correction | `needs:format` | `D97706` |

Issues use `bug`, `feat`, or `help`. PRs select exactly one primary type from `bug`, `feat`, or `test`. Classify documentation, configuration, and dependency changes as bug or feat according to their purpose; use test for changes dedicated to tests. Author identity belongs in the description, not in a label.

#### Size Calculation

Use the complete PR diff against its target branch. After excluding generated files, total additions as A and deletions as D. Classify by `max(A, D)`, never A+D. Each PR has exactly one size label.

| Label | `max(A, D)` |
| --- | --- |
| `size:XS` | 0–49 |
| `size:S` | 50–499 |
| `size:M` | 500–999 |
| `size:L` | 1000–3000 |
| `size:XL` | 3001 or more |

For example, 400 additions and 400 deletions is S; 80 additions and 1200 deletions is L. A change containing only excluded files or no text lines is XS. Binary files use the line counts returned by GitHub; the classifier does not invent line counts.

The shared policy excludes:

- `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`, `Cargo.lock`, `go.sum`, and `skills-lock.json` in any directory.
- `spec/docs.go`, `spec/swagger.json`, and `spec/swagger.yaml`.
- `packages/sdk/src/**`, `internal/db/postgres/sqlc/**`, and `**/*.pb.go`.
- `packages/icons/src/**`, except the handwritten `icons/Codex.vue`, `icons/CodexColor.vue`, and `icons/Misskey.vue` files.
- `apps/web/src/components/file-manager/seti/vs-seti-icon-theme.json`.

Migration SQL, tests, documentation, configuration, and icon source files count normally. A renamed file is excluded only when both its old and new paths are excluded, so moving handwritten code into a generated directory does not hide its size. The run summary reports original and filtered additions/deletions, the number of excluded files, and the resulting labels.

#### Scope Classification

Scope labels reflect changed paths and can coexist. Added, modified, and deleted files participate; renames check both paths. Generated files still participate in scope classification.

| Label | Paths |
| --- | --- |
| `change:web` | `apps/web/**`, the `packages/ui` gitlink or its contents, `packages/icons/**`, `packages/config/**`, `packages/sdk/**`, `patches/**` |
| `change:desktop` | `apps/desktop/**`, `packages/config/**` |
| `change:migrations` | `db/**/migrations/**` |
| `change:server` | `cmd/**`, `internal/**`, `conf/**`, `db/**`, `spec/**`, and root `go.mod`, `go.sum`, `sqlc.yaml`, `openapi-ts.config.ts` |

Root `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `eslint.config.mjs`, `tsconfig.json`, and `vitest.config.ts` trigger both Web and Desktop. Migrations trigger both migrations and server. Unmatched documentation or governance files may have no scope label. The classifier does not infer indirect dependencies, and labels are not CI authorization credentials.

### Templates and Evidence

Issue forms include Bug Report, Feature Request, and Help. The blank issue entry remains available. Each form starts with required Human/Agent identity and its corresponding type. The former Area and Channel fields are removed. CLI, API, and blank submissions must preserve the corresponding sections.

- Bug requires Bug Description, Steps to Reproduce, Expected and Actual Behavior, and Version.
- Feature requires Feature Description and Use Case and Motivation.
- Help requires Problem, Desired Outcome, What You Have Tried, and Version and Environment.
- All forms offer Screenshots / Recordings and Additional Context. Bug and Help also offer Logs.

The PR template requires exactly one Author and Type choice, Summary, Validation, Screenshots / Recordings, and Human QA. Related Issues is optional. PR title enforcement is outside this workflow. Templates, documentation, and automated messages are written in English. Contribution titles and free-form responses may use any language. Only section names and choices must match the current template; the validator does not enforce a language for user-written content.

Empty sections, placeholders, missing selections, and multiple selections fail validation. Fenced examples cannot supply outer section headings or choices. Content quality is not judged by a minimum word count.

Upload screenshots as GitHub-accessible attachments. For visible behavior, agents should use browser tools or Computer Use to exercise the change and capture evidence. If capture or upload is unavailable or not applicable, explain why and describe alternative verification. Local file paths are not uploaded evidence.

Until a human confirms QA, select `Not yet verified by a human` and keep the existing No human QA disclosure at the end of the PR description. After confirmation, select `Confirmed by a human`, identify the reviewer and confirmation record, and remove the disclosure. Agent tests and screenshots do not count as human QA. Automation validates the declaration's structure, not whether human verification actually occurred.

### Workflow Behavior

`Contribution governance` uses trusted default-branch scripts. PR events use `pull_request_target`; issues use `issues`. The controller does not check out or execute PR code. It reads the current API description, validates it, synchronizes the type label, and calculates PR size/scope labels.

An invalid description receives `needs:format` and one identifiable bot comment mentioning the author and listing corrections. Editing the description triggers another check. Once corrected, the label is removed and the existing comment is updated instead of posting another one. An initially valid submission does not receive an extra success comment.

#### CI Gate and Automatic Approval

The controller records `PR Format` on the current PR head SHA, including a fingerprint of the head, target branch, and description. The read-only PR CI gate loads default-branch rules, validates the latest description and current head, and waits for a successful controller status with the matching fingerprint. Code jobs depend on this gate. Both the gate and privileged controller use default-branch code; PR changes cannot substitute a different validator.

Invalid format prevents dependency installation, lint, tests, and builds. GitHub may still create a workflow run or execute the lightweight gate. Existing CI path filters remain in effect. Push, release, and maintenance workflow_dispatch behavior does not use the PR gate. Docker shares build logic between read-only PR and publishing entry points; the PR caller passes no publishing secrets and fixes publishing to false.

The controller retains the repository's `first_time_contributors` approval setting and approves eligible pending fork PR runs. It checks the repository, PR, current head, and workflow allowlist: ESLint, Go, Rust, Runtime, Migrations, Installer, Electron, Docker PR, and contribution policy tests. Publishing, deployment, and documentation maintenance workflows are not automatically approved.

- `workflow_run` requested/completed events reconcile timing differences, with a five-minute scheduled reconciliation as a fallback.
- After a description is corrected, only runs blocked by format with no executed code jobs are retried. Actual test failures are not retried automatically.
- New commits require a new check; an old head's result is not reused.
- If the description becomes invalid, the controller cancels active CI and records the run and attempt. It can recover that attempt after correction; manually cancelled runs are not automatically resumed.
- Scheduled scans include PRs created after governance was introduced and older PRs whose current head already has a governance status. Untouched older PRs are not flooded with comments.
- PRs created with GITHUB_TOKEN may not trigger other workflows. Reconciliation can still validate and label them, but cannot create a missing ordinary pull_request CI run. To start all CI automatically, those PRs need an authoring GitHub App that produces normal PR events, or a subsequent maintainer push. Model-sync and documentation-update PR bodies follow the template without exemptions.

Control jobs may write comments, labels, statuses, and workflow approvals, but never execute contributor code. Code CI uses GitHub-hosted runners and read-only tokens. PR callers receive no secrets, and checkout does not persist credentials. Format compliance does not establish that external code is trustworthy; automatic workflow approval authorizes CI resource use only.

API operations have bounded retries. An incomplete file list preserves existing size/scope labels and reports a classification error instead of claiming the description is invalid. Exceeding GitHub's file-list limit has the same behavior. Approval permission failures fail the controller visibly rather than claiming CI was released.

#### Maintenance

Maintainers can supply a PR number to `Contribution governance` through workflow_dispatch to reconcile it. An empty input scans eligible open PRs. `Sync labels` also supports manual dispatch and writes only from main in the primary repository.

Both the format gate and label/approval controller require their scripts on the default branch. During the initial rollout, pre-merge checks cannot load these scripts until they are merged into main. After deployment, verify the controller and fork approval behavior live; local tests do not establish that automatic approval works. Changes to workflow YAML still require normal code review; format validation is not an isolation mechanism for malicious workflow changes.

### Migration and Verification

The initial migration script is read-only by default; `--apply` explicitly enables writes:

```sh
node .github/scripts/migrate-labels.mjs felinics/Memoh /absolute/backup/directory
node .github/scripts/migrate-labels.mjs felinics/Memoh /absolute/backup/directory --apply
```

Before writing, the script saves label definitions, historical associations, and open-PR labels/classifications. It renames old size labels, applies the configuration, adds base bug/feat labels to historical regional classifications, and removes explicitly listed obsolete labels. It does not guess historical types from question/documentation labels or post comments. Finally, it recalculates size/scope labels for open PRs, skipping any PR whose head changed. Closed PR sizes are not recalculated.

The backup supports manual inspection and restoration of associations. Recreating a deleted label does not preserve its original GitHub label ID. Routine configuration synchronization does not invoke deletion logic.

Local verification:

```sh
node --test .github/scripts/*.test.mjs
# Use actionlint to validate workflow configuration.
```

Tests cover rendered forms, identity/type choices, screenshot explanations, QA declarations, fenced examples, duplicate sections, size boundaries/exclusions, renames, submodules, idempotent labels, comment reuse, current-head checks, automatic approval, format failure recovery, and rejection of missing, stale, failed, or untrusted controller statuses.

Live rollout verification must also use a real first-time contributor's fork PR. Invalid format should produce only format feedback; correcting the description should release eligible CI without a maintainer clicking Approve. Verify new commits, description edits, controller cancellation/recovery, and actual test failures separately. Mocked API tests and static validation do not replace this acceptance check.
