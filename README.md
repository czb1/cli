# omres-cli

面向 AI Agent / 脚本的 OMResTool 命令行工具，按《通用 AI 友好型 HTTP-CLI 封装规范》实现，参考 pipeline-x CLI 的架构。

- **配置驱动**：`internal/cli/api_cli_config.json` 决定哪些接口暴露为命令。
- **Swagger 为真相源**：`internal/cli/docs/swagger.json` 提供参数、类型、输出 schema。启动时校验配置的 `path+method` 都存在于 swagger。
- **统一输出**：所有命令输出 JSON-RPC 2.0，后端 JSON 保持原生对象，不做二次字符串化。
- **AI 可探索**：`--help`（分层）+ `describe`（完整语义契约）。
- **零业务逻辑**：CLI 仅作 HTTP 客户端，调用 `https://omtool.rnd.huawei.com`。

覆盖接口文档中全部 **55** 个接口。

## 构建

需要 Go 1.21+。**零第三方依赖**，只用标准库，因此内网无法访问公共 Go 代理时也能直接构建
（`go.mod` 中没有任何 require，`go mod tidy` 不会发起网络请求）。

```bash
go build -o omres-cli ./cmd/cli            # Linux / macOS
GOOS=windows go build -o omres-cli.exe ./cmd/cli   # 交叉编译 Windows
```

也可以用现成脚本，会一并完成冒烟检查与安装到 PATH：

```powershell
.\build.ps1                # Windows
```

```bash
./build.sh                 # Linux / macOS
```

两者都支持 `-NoInstall` / `--no-install`（只构建不安装）。
Windows 上安装后需**重开终端**，`setx` 对已打开的窗口不生效。

`swagger.json` 与 `api_cli_config.json` 通过 `//go:embed` 编译进二进制，运行时不依赖外部文件。

## 使用

```bash
# 分层探索
./omres-cli --help                         # 列出所有 group
./omres-cli moc --help                     # 列出 moc 下所有 action
./omres-cli describe moc add-name          # 完整参数与输出 schema（不发网络请求）

# 执行（POST + JSON body）
./omres-cli validate do --body '{"projectId":123}'

# 路径参数按顺序作为位置参数
./omres-cli task export-struct 123 demo 0
./omres-cli moc generate-script 123 10 BoardInfo add 1   # 二进制，自动存临时文件

# 文件上传（multipart）
./omres-cli upload file --taskId 123 --file ./model.zip

# 从文件读 body
./omres-cli mml-command upsert --body-file ./cmd.json

# 直调尚未收录的接口（自动带会话，输出同样是 JSON-RPC）
./omres-cli raw /api/some/newEndpoint -X POST --body '{}'
```

### 命令分组（55 接口）

| group | actions |
|-------|---------|
| auth | login, status, logout |
| task | create, export-struct, export-result, download, export-validate, include-alarm, delete-one ⚠ |
| upload | file, parse-xml |
| moc | add-name, select-name, insert-info, generate-script |
| moc-field | add-name, select-name, update-info |
| datatype | add, query-all, enum-add, enum-query-all |
| default-record | add |
| method | add-name, update-name, delete-name ⚠, select-info |
| mml-command | upsert, get |
| command-para | upsert, list |
| mml-para | list |
| command-branch | upsert, list |
| validate | do, result |
| errorcode | shield |
| info-code | add, list |
| info-module | query-all |
| overallview | search, micro-service-list |
| resource | auto-gen-id, north-auto-gen-id |
| perf | indicator-group-add, indicator-add, indicator-update |
| alarm-service | upsert, list |
| alarm | upsert, list |
| alarm-enum | upsert, list |
| alarm-enum-value | upsert |
| alarm-para | upsert, list |

标 ⚠ 的是破坏性命令，详见下方「破坏性操作」。
此外还有一个不属于任何 group 的 `raw` 命令，用于直调尚未收录的接口。

### 告警建模：调用顺序

CLI 只提供原子命令，**不做编排**——下面每一步的入参需要调用方自己从上一步的响应里取。

| # | 命令 | 依赖上一步的 |
|---|------|------------|
| 0 | `task create` | — ；响应的 `extendData` 即 `taskId`，后续各步都要带（沿用已有工程可跳过） |
| 1 | `alarm-service upsert` | `taskId` |
| 2 | `alarm-service list` | 按 `serviceName` 找到 `id` → 后续 `serviceId` |
| 3 | `alarm upsert` | `serviceId` |
| 4 | `alarm list` | `serviceId`；取响应的 `id` 与 `alarmId` |
| 5 | `alarm upsert` | 回传 `id`/`serviceId`/`alarmId` + 级别、分类等完整配置 |
| 6 | `alarm-enum upsert` | `alarmInternalId` = 第 4 步的 `id` |
| 7 | `alarm-enum list` | 取响应的 `id` → 枚举值的 `enumTypeId` |
| 8 | `alarm-enum-value upsert` | `enumTypeId` |
| 9 | `alarm-para upsert` | `alarmInternalId` = 第 4 步的 `id` |
| 10 | `alarm-para list` | 取响应的 `id` 与 `paramOrder` |
| 11 | `alarm-para upsert` | 回传 `id`/`alarmInternalId`/`paramOrder` + 完整字段 |
| 12 | `validate do` | `projectId` = taskId |
| 13 | `task export-validate` | — |
| 14 | `task include-alarm` | multipart 表单，用 `--taskId` 而不是 `--body` |
| 15 | `task export-result <taskId> <taskName>` | — |
| 16 | `task download <taskId> <taskName>` | 二进制，自动存临时文件 |

> ⚠ **易错点**：`alarm-enum list` / `alarm-para list` 的入参名叫 `alarmId`，
> 但要传的是告警的**内部主键 `id`**（第 4 步响应里的 `id`，如 `96`），
> 不是后端分配的告警号字符串（如 `"100910"`）。`alarmInternalId` 同理。

### 性能指标注册流程（perf / resource / overallview）

新增一个指标组（测量单元）和其下的指标，接口有严格先后顺序：前一步拿到的 ID 是后一步的入参。
以「指定RATTYPE的CGW发送 PDU Session Establishment Reject消息数」为例：

```bash
# 0) 新建工程，拿 taskId（后续每一步的 --taskId 都用它；沿用已有工程可跳过）
omres-cli task create --body '{"taskName":"percreate","neType":"UNC","productType":"0"}'
# → {"status":true,"extendData":48279}   ← extendData 就是 taskId

# 1) 按实际服务列表（如 SmcExecSvc）查出 belongService 服务ID
omres-cli overallview micro-service-list

# 2) 取测量单元ID（muId）
omres-cli resource auto-gen-id --neName UNC --belongService 203 --idType mu --taskId 47754
# → {"status":true,"data":55}

# 3) 取网管测量单元ID（nmMuId）；注意 belongService 用网管侧服务ID
omres-cli resource north-auto-gen-id --neName UNC --belongService 114 --idType mu --checkDeleted false
# → {"status":true,"data":1929445469}

# 4) 注册指标组（测量单元）
omres-cli perf indicator-group-add --taskId 47754 --body-file ./mu.json

# 5) 在该指标组下取指标ID（muId 必填）
omres-cli resource auto-gen-id --neName UNC --belongService 203 --idType metric --taskId 47754 --muId 55
# → {"status":true,"data":103856}

# 6) 登记指标ID与名称
omres-cli perf indicator-add --taskId 47754 --belongService 203 \
  --body '{"metricId":"103856","metricName":"指定RATTYPE的CGW发送 PDU Session Establishment Reject消息数","meType":0,"belongService":203,"muId":55}'

# 7) 取网管指标ID（nmMetricId）
omres-cli resource north-auto-gen-id --neName UNC --belongService 114 --idType metric --checkDeleted false
# → {"status":true,"data":1929446634}

# 8) 补齐指标完整属性（算法、值类型、语言资源、测量点、nmMetricId 等）
omres-cli perf indicator-update --taskId 47754 --metricId 103856 --belongService 203 --body-file ./metric.json
```

几点约定：

- 除第 1 步（`overallview micro-service-list`）和第 3/7 步（网管北向取号）外，其余每一步都要带 `--taskId`，
  值取自第 0 步 `task create` 响应的 `extendData`。
- `resource auto-gen-id` 与 `resource north-auto-gen-id` 是两套 ID 空间：前者是本地（`belongService` 如 203），
  后者是网管北向（`belongService` 如 114），不要混用。
- `indicator-update` 的查询参数 `--metricId` 必须与请求体里的 `metricId` 一致。
- 这几个接口都以 `{"status":false}` 表达业务失败，CLI 会把它转成 `Operation Failed` 错误，不会当成成功。
- 请求体字段较多，建议用 `--body-file`；未在 swagger 中列出的后端字段会原样透传，不做裁剪。
  完整字段清单见 `omres-cli describe perf indicator-update`。

## 破坏性操作

删除类命令（路径含 `delete`/`remove`/`drop`/`clear`/`purge` 等，或方法为 `DELETE`）
默认**不会**直接执行，需要显式确认：

```bash
# 预览将要发送的请求，不实际发送
omres-cli task delete-one --body '{"taskId":48050}' --dry-run

# 确认无误后执行
omres-cli task delete-one --body '{"taskId":48050}' --yes
```

| 场景 | 行为 |
|------|------|
| 交互式终端 | 提示 `确认继续？输入 yes 回车` |
| 脚本 / AI Agent（stdin 非终端） | **直接拒绝**，返回 `-32004 Confirmation Required` |
| 加了 `--yes` | 直接执行 |
| 加了 `--dry-run` | 只打印请求预览 |

非交互环境刻意设计为「拒绝」而非「等待输入」——否则调用方会卡在等待上直到超时。

### 业务失败识别

本后端存在 **HTTP 200 但业务失败**的接口，例如 `/api/task/deleteOne`
删除失败时返回 `200 + {"status":false}`。若只看 HTTP 状态码，删除失败会被当成成功。

因此 HTTP 2xx 响应还会按接口契约二次判定：

| 响应形态 | 判定 |
|----------|------|
| `status` 为布尔 `false` | 失败 → `Operation Failed` |
| `code` 存在且非 0 | 失败 → `Business Error` |
| 其它 | 成功 |

## raw：直调未收录接口

当某接口还没写进 `api_cli_config.json` / `docs/swagger.json` 时，用 `raw` 访问：

```bash
omres-cli raw /api/task/list -X POST --body '{}'
omres-cli raw /api/task/deleteOne -X POST --body '{"taskId":123}' --yes
omres-cli raw /some/path --query page=1 --query size=20 -H 'X-Trace: abc'
```

支持 `-X/--method`、`--body`、`--body-file`、`--query k=v`、`-H/--header`、
`--yes`、`--dry-run`。破坏性判定与上一节一致。

> **不要**为了调用未收录接口去读 `~/.omres-cli/session.json` 里的 Cookie 拼 curl。
> 那样会绕开会话管理、统一错误码与破坏性确认。`raw` 就是为这个场景准备的。

## 鉴权

登录、查看状态、登出都由 `auth` 组承担，登录态自动落盘，**后续命令无需再传 `--cookie`**。

```bash
# 1) 登录（推荐：密码走标准输入，不进命令历史）
omres-cli auth login --username zhangsan --password-stdin < pass.txt
# 交互式（密码不回显）
omres-cli auth login --username zhangsan
# CI/CD
$env:OMRES_AUTH_USERNAME="zhangsan"; $env:OMRES_AUTH_PASSWORD="******"
omres-cli auth login

# 2) 查看状态（本地检查，不发网络请求）
omres-cli auth status
# 额外向后端发一次只读探活，确认会话真的没失效
omres-cli auth status --online

# 3) 登出
omres-cli auth logout
```

登录成功后 Cookie 写入 `~/.omres-cli/session.json`（Windows 为 `%USERPROFILE%\.omres-cli\session.json`），
文件权限 `0600`。**密码不会出现在任何输出或文件中**，输出里的 Cookie 一律打码（`JSESSIONID=ABC******XYZ`）。

### auth status 的退出码

`auth status` 是唯一带语义退出码的命令，便于脚本 / AI Agent 直接分支判断：

| 退出码 | 含义 | 建议动作 |
|--------|------|----------|
| 0 | 已认证 | 继续后续流程 |
| 3 | 未认证或会话已过期 | 引导用户执行 `omres-cli auth login` |
| 1 | 其它错误（如 `--online` 时后端不可达） | 排查网络 / `--server`，**不要**误判为需要重新登录 |

其余命令仍保持「永远退出 0，结果看 JSON-RPC」的既有约定，不影响已有脚本。

### 会话有效期

- 后端 Cookie 带 `Expires` / `Max-Age` → 以后端为准。
- 只下发会话 Cookie（无过期时间）→ 本地按软 TTL **8 小时**判定，可用 `OMRES_SESSION_TTL_HOURS` 覆盖。

### 凭证来源优先级

请求携带的 Cookie 按此顺序解析，先命中先用：

```
--cookie  >  OMRES_AUTH_COOKIE  >  api_cli_config.json  >  ~/.omres-cli/session.json
```

`auth status` 输出的 `source` 字段会告诉你当前用的是哪一个。

其它鉴权方式（若后端改用 Token/API Key/Basic）在 `defaults.auth` 中声明 `type` 即可，对应覆盖环境变量：

| 配置字段 | 环境变量 |
|----------|----------|
| defaults.server | `OMRES_SERVER` |
| defaults.auth.token | `OMRES_AUTH_TOKEN` |
| defaults.auth.api_key | `OMRES_AUTH_API_KEY` |
| defaults.auth.username | `OMRES_AUTH_USERNAME` |
| defaults.auth.password | `OMRES_AUTH_PASSWORD` |
| defaults.auth.cookie | `OMRES_AUTH_COOKIE` |
| defaults.auth.probe_path / probe_body | 仅配置文件（`auth status --online` 用的只读探活接口） |

凭证不会出现在正常输出中；请勿把明文凭证提交到仓库。

## 输出示例

成功：
```json
{ "jsonrpc": "2.0", "result": { "code": 0, "msg": "操作成功" }, "id": "req-a1b2c3d4" }
```

后端非 2xx（JSON）：
```json
{ "jsonrpc": "2.0", "error": { "code": -32000, "message": "400 Bad Request",
  "data": { "code": 10001, "msg": "名称已存在" } }, "id": "req-..." }
```

业务失败（HTTP 200 但 `status:false`）：
```json
{ "jsonrpc": "2.0", "error": { "code": -32000, "message": "Operation Failed",
  "data": { "status": false } }, "id": "req-..." }
```

破坏性操作未确认：
```json
{ "jsonrpc": "2.0", "error": { "code": -32004, "message": "Confirmation Required",
  "data": { "method": "POST", "path": "/api/task/deleteOne",
  "hint": "这是破坏性操作。确认无误后请加 --yes 重新执行。" } }, "id": "req-..." }
```

二进制下载：
```json
{ "jsonrpc": "2.0", "result": { "file": "/tmp/download-...", "content_type": "application/octet-stream", "size": 20480 }, "id": "req-..." }
```

## 目录结构

```
omres-cli/
├── cmd/cli/main.go                    # 入口
├── internal/cli/
│   ├── cli.go                         # 加载配置+swagger，校验，构建命令树
│   ├── command.go                     # 命令树与 flag 解析（标准库实现，替代 cobra）
│   ├── builder.go                     # 由配置+swagger 生成命令
│   ├── config.go                      # 配置解析、env 覆盖、校验
│   ├── swagger.go                     # Swagger 解析与操作索引
│   ├── describe.go                    # describe 命令 + 辅助函数
│   ├── auth.go                        # auth login / status / logout
│   ├── raw.go                         # raw 直调命令 + 破坏性操作确认
│   ├── session.go                     # 会话落盘、过期判定、Cookie 打码
│   ├── prompt.go / prompt_*.go        # 交互式输入（密码不回显，零外部依赖）
│   ├── httpclient.go                  # HTTP 客户端、鉴权注入、响应封装
│   ├── jsonrpc.go                     # JSON-RPC 2.0 输出
│   ├── types.go                       # 数据模型
│   ├── api_cli_config.json            # CLI 命令配置（go:embed）
│   └── docs/swagger.json              # Swagger（go:embed，由 tools 生成）
├── tools/build_swagger.py             # 由 API 文档生成 swagger.json
└── go.mod
```
