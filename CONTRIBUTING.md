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

Contributors can submit an issue or PR before format checks run. GitHub Actions validates the description, synchronizes labels, and requests corrections. PR lint, test, and build jobs run independently of description format. A separate controller approves pending first-time contributor workflow runs automatically. Workflow approval does not approve a code review, merge, or release.

### Label Configuration

`.github/labels.json` is the source of truth for label names, colors, and descriptions. New descriptions default to Chinese. GitHub does not read this file natively. The `Sync labels` workflow applies changes from main through the GitHub API. Routine synchronization creates or updates labels; it never deletes them.

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

The PR template requires exactly one Author and Type choice, Summary, Validation, Screenshots / Recordings, and Human QA. Related Issues is optional. PR title enforcement is outside this workflow. Newly authored content defaults to Chinese, including contribution titles and free-form responses, as described in [Working Language](AGENTS.md#working-language). Other languages are welcome; quoted code, error logs, and upstream English material stay as-is. Only section names and choices must match the current template; the validator does not enforce a language for user-written content.

Empty sections, placeholders (including bare OK/done/passed), missing selections, and multiple selections receive format feedback. Subheadings inside a template section remain part of that section; only recognized template headings start another field. Fenced examples cannot supply outer section headings or choices. Content quality is not judged by a minimum word count.

Upload screenshots as GitHub-accessible attachments. For visible behavior, agents should use browser tools or Computer Use to exercise the change and capture evidence. If capture or upload is unavailable or not applicable, explain why and describe alternative verification. Local file paths are not uploaded evidence.

Human QA 的勾选项是唯一状态声明，不需要额外警告行。尚无真人确认时只选 `Not yet verified by a human`；获得明确确认后只选 `Confirmed by a human`，并注明验收人和确认记录。选项和确认记录必须是可见正文，注释或代码块不算。Agent 测试和截图不能代替真人 QA；自动校验只检查声明结构，不证明验收实际发生。

### Workflow Behavior

`Contribution governance` uses trusted default-branch scripts. PR events use `pull_request_target`; issues use `issues`. The controller does not check out or execute PR code. It reads the current API description, validates it, synchronizes the type label, and calculates PR size/scope labels.

An invalid description receives `needs:format` and one identifiable bot comment mentioning the author and listing corrections. Editing the description triggers another check. Once corrected, the label is removed and the existing comment is updated instead of posting another one. An initially valid submission does not receive an extra success comment.

#### 独立格式提示与工作流审批

格式反馈通过 `needs:format` 和已有机器人评论呈现。`PR Format` 状态始终为 success，并在描述中区分“通过”和“有待补充”，避免已有 required-status 配置因描述格式阻塞代码检查或合并。该状态只表示反馈已处理，不表示代码检查通过。控制器仍从可信默认分支加载规则，不执行贡献者代码。

代码 CI 不依赖格式任务，也不等待控制器。控制器不会因描述变化取消、重跑任何 CI；测试失败和人工取消仍由贡献者或维护者处理。现有路径过滤、只读 token、GitHub 托管 runner 和发布权限边界保持不变。Docker PR 仍固定 `publish: false`，不接收发布 secrets。

外部贡献者待审批的工作流仍自动审批，审批检查仓库、PR、当前 head 和既有工作流白名单，不以描述格式为条件。发布、部署和文档维护工作流不在自动审批范围内。`workflow_run` 事件及五分钟定时扫描继续协调审批；授权失败会明确报错，不伪装成功。

旧 PR 分支可能仍引用 `contribution-format.yml`，因此保留兼容入口；该入口直接返回，不校验描述、不轮询状态。旧的 `PR Format cancellation / <run id>` 状态仅在当前 head 上、最新记录由 `github-actions[bot]` 创建且为 failure 时标记为已停用。不会改写真实测试状态、其他账号的状态或自动重跑历史任务；已经跳过或取消的旧 CI 需要手动重跑或通过新提交触发。

定时扫描继续跳过治理上线前、从未参与治理的旧 PR，避免批量打扰。由 `GITHUB_TOKEN` 创建的 PR 可能不触发普通 PR CI；此控制器不会另造运行，需要能产生正常 PR 事件的 GitHub App 或后续提交。

API 操作保留有限重试。文件列表不完整时保留已有 size/scope 标签并报告分类错误；不会把分类故障说成描述不合格。格式提示不判断代码是否可信，也不代替代码 review、合并授权或真人 QA。

#### Maintenance

Maintainers can supply a PR number to `Contribution governance` through workflow_dispatch to reconcile it. An empty input scans eligible open PRs. `Sync labels` also supports manual dispatch and writes only from main in the primary repository.

控制器在默认分支合并后才采用新行为。PR 自身的测试可验证新解析器、独立 CI 配置和模拟 API 行为，但不能证明默认分支控制器已更新。合并后应使用真实外部贡献者的 fork PR 验证自动审批、描述编辑和旧状态清理。Workflow YAML 仍需正常 review。

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

测试覆盖 Issue 表单、身份/类型选择、章节内小标题、QA 声明与附注、代码块和注释、重复字段、占位文本、代码量分类、幂等标签、评论复用、当前 head、独立工作流审批及旧取消状态清理。遍历所有普通 PR 工作流，检查不存在格式任务依赖。

合并后的线上验收需确认：无效描述只产生提示，代码 CI 照常运行；编辑描述不会取消或重跑 CI；真实首次贡献者的白名单工作流仍能自动审批；旧取消标记被停用但真实失败结果保留。本地模拟测试不替代这些验收。
