# Agent 能力管理验收记录

日期：2026-09-18。验证由 Agent 执行，尚无人类 QA 确认。

## 环境

- 从 `origin/main` 的 `860382510` 创建独立 worktree，分支 `codex/agent-capability-management`。原 checkout 的未提交内容未改动。
- 使用仓库的 `scripts/dev-compose.sh` / `devenv/docker-compose.yml` 启动隔离 Compose 项目 `memoh-capability`；Web 为 `http://localhost:21482`，Server 为 `http://localhost:21480`。
- 已核对 Server 的 `/workspace` 挂载指向本分支 worktree。Server、Channel、PostgreSQL、pgvector、Connect-It 健康；Web 实际可访问，Server `HEAD /health` 返回 200。
- 测试覆盖真实 Web、Go Server、审批持久化、工作区容器、Supermarket 下载及文件发布。模型使用本地确定性 OpenAI 兼容测试服务，以稳定触发具体工具调用；MCP OAuth 使用本地测试授权服务。未使用真实第三方账号或付费模型凭据。
- 本地 Compose override 修正开发配置中 pgvector 的容器内地址，并设置 `MEMOH_SERVER_PUBLIC_URL=http://localhost:21482`；这些环境文件不纳入提交。

## 真实 UI 与运行时结果

| 场景 | 操作与观察 | 截图 |
| --- | --- | --- |
| 目录发现 | 聊天 `app_search` 查询真实 Supermarket 的 `openai/build-web-data-visualization` | [01](screenshots/agent-capability-management/01-supermarket-search.png) |
| 固定安装内容 | 审批展示 App revision 和依赖范围，允许后执行安装 | [02](screenshots/agent-capability-management/02-frozen-install-approval.png) |
| 同任务使用 | 安装后同一 Agent turn 调用 `list_skills`、`use_skill`，并正常结束 | [03](screenshots/agent-capability-management/03-same-task-skill.png) |
| 卸载 | 聊天确认后返回 `removed`，随后可重新安装 | [04](screenshots/agent-capability-management/04-app-uninstall.png) |
| MCP 授权入口 | 聊天创建 MCP 并发起 OAuth，返回供用户点击的授权链接 | [05](screenshots/agent-capability-management/05-oauth-link.png) |
| OAuth 回调 | 浏览器完成本地测试授权，Memoh 回调页显示连接成功 | [06](screenshots/agent-capability-management/06-oauth-callback.png) |
| 授权后使用 | 聊天探测并在同一任务调用新发现的 `qa_oauth_mcp_qa_echo` | [07](screenshots/agent-capability-management/07-authorized-mcp-call.png) |
| 停用持久化 | 聊天停用连接，Settings 显示开关关闭及授权状态；后续模型工具表移除该 MCP 工具 | [08](screenshots/agent-capability-management/08-mcp-settings.png) |
| App 持久化 | Settings 显示已安装 App 的 18 个 Skills | [09](screenshots/agent-capability-management/09-installed-app-settings.png) |
| 更新 | 聊天更新到当前发布，结果保持指定 revision、`installed`，任务正常结束 | [10](screenshots/agent-capability-management/10-app-update.png) |
| 失败反馈 | 在隔离 Bot 的空暂存目录注入文件阻塞，安装返回 `registry.app_install_failed`，API 记录为 `failed`，未输出内部路径或凭据 | [11](screenshots/agent-capability-management/11-install-failure.png) |
| 失败恢复 | 移除测试阻塞后，聊天 `resume` 恢复同一 installation 为 `installed` | [12](screenshots/agent-capability-management/12-app-resume.png) |
| 权限拒绝 | 非管理用户调用 MCP 管理返回 `capability.access_denied`，无审批、无连接写入 | [13](screenshots/agent-capability-management/13-manage-permission-denied.png) |
| App 权限拒绝 | 同一非管理用户可读取目录详情，但安装返回 `capability.access_denied` | [14](screenshots/agent-capability-management/14-app-permission-denied.png) |

安装的不可变 revision 为 `85f4bff97329c74133cd37e503cfe2ba4135a24af851c26b65d5be61fc42aa5c`。故障注入仅影响测试 Bot 的暂存目录，完成后已恢复。

## 自动化检查

- 全仓 `go test ./...` 通过；以下重点范围也单独通过：`go test ./internal/agent/... ./internal/apps/... ./internal/config/... ./internal/connectors/... ./internal/mcp/... ./internal/workspacedeps/... ./internal/apperror/... ./internal/server/... ./internal/handlers/... ./cmd/internal/core/...` 通过。
- 对 Tool、approval、native runtime、session runtime、Apps、federation 包执行 race 检查并通过。
- 针对本次变更的 `golangci-lint run --new-from-rev=860382510` 通过。全仓 lint 另报既有问题，涉及 40 个文件（例如旧测试的 `httptest.NewRequest`、原有 gosec/govet 告警）；本次修改覆盖的 `config_test.go` 告警位于未修改的既有行，差异检查无新增告警。
- Web API/SSE 错误测试共 82 项通过；`pnpm lint` 无错误，保留 3 个原有测试文件警告。
- `git diff --check` 通过。
- UI contract 检查受基线问题影响：未修改的 `apps/web/src/pages/home/components/tool-call-diff-panel.vue:6` 使用 `animate-spin`，超过既有 loader baseline。该文件与基准提交一致；本次未扩展修复范围。

新增回归覆盖：审批后权限撤销；拒绝模型注入 Bot/目标/秘密字段；敏感配置脱敏；MCP 部分更新与目标变更；非交互审批拒绝；OAuth 状态；安装 revision 和依赖快照在审批等待期间保持固定；目标/卸载范围变化拒绝；恢复版本锁校验；缓存 MCP 路由撤销；同任务工具刷新；审批不重复执行；inline 与 deferred 决策并行；丢失的冻结审批不重放。

## 验证边界

- 本地确定性模型用于验证调度与工具契约，不代表所有模型的自然语言工具选择效果。
- OAuth 页面和令牌端点为测试服务；真实服务商的 OAuth、刷新令牌、App Connector 授权及外部 MCP 客户端尚未进行真实账号验收。Connector 引用与授权生命周期由现有 Apps 测试覆盖。
- 本地 OAuth 链接最初被 Web 的 localhost 规则识别为工作区浏览器地址；实际验收将同一链接在宿主浏览器打开，再完成真实后端回调。公开外部授权域名未复现该本地地址行为。
- 现有 WebSocket 入口要求 `workspace_exec` 或 `manage`。纯 Chat 身份在入口收到 403，因此真实聊天权限测试使用 `chat + workspace_exec`、不含 `manage` 的用户；本次未改变既有 WebSocket 策略。
- 早期开发迭代留下一个旧会话的生成标记；修正后的安装、更新、卸载、恢复与 MCP 操作均验证正常终止。旧侧栏标记在重新登录后已消失；早期截图中的标记不属于被验证的活动 turn。


## 分类浏览补充验收

`app_search` 增加 `categories` action，仍保持三个工具。分类返回稳定 ID、多语言名称、非空分类数量及各 Registry 的 App 数量；`search` 传入分类 ID、不提供关键词即可浏览项目。两种查询均支持分页，App 摘要现在包含分类 ID 和名称。

- 新增自动化测试覆盖 Registry 筛选后分页、数量与名称保留、末页与未知 Registry、无关键词分类查询及上游异常不能伪装为空目录。
- `go test ./internal/agent/tool ./internal/agent/runtime/native ./internal/supermarket` 通过；本次差异 Go lint 通过。
- 复用当前 worktree 的真实开发环境 `http://localhost:21482`，重新核对 Server 挂载及健康。通过确定性模型调用真实 Supermarket API，分类列表返回 21 个非空分类，开发工具分类返回 24 个 App。
- [分类列表截图](screenshots/agent-capability-management/15-app-categories.png) 展示分类 ID、中文名称及数量；连续浏览开发工具分类的 [第一页](screenshots/agent-capability-management/16-category-apps-page-1.png) 和 [第二页](screenshots/agent-capability-management/17-category-apps-page-2.png)，每页 3 项，并确认项目不重复。第一页为 algolia / cloudflare / git，第二页为 github / gitlab / postman。
