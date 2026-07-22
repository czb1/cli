# omres-cli

面向 AI Agent / 脚本的 OMResTool 命令行工具，按《通用 AI 友好型 HTTP-CLI 封装规范》实现，参考 pipeline-x CLI 的架构。

- **配置驱动**：`internal/cli/api_cli_config.json` 决定哪些接口暴露为命令。
- **Swagger 为真相源**：`internal/cli/docs/swagger.json` 提供参数、类型、输出 schema。启动时校验配置的 `path+method` 都存在于 swagger。
- **统一输出**：所有命令输出 JSON-RPC 2.0，后端 JSON 保持原生对象，不做二次字符串化。
- **AI 可探索**：`--help`（分层）+ `describe`（完整语义契约）。
- **零业务逻辑**：CLI 仅作 HTTP 客户端，调用 `http://10.243.80.228`。

覆盖接口文档中全部 **36** 个接口。

## 构建

本环境无 Go 工具链且无网络，需在你本机构建（Go 1.21+）：

```bash
cd omres-cli
go mod tidy          # 拉取 cobra/pflag 依赖并生成 go.sum
go build -o omres-cli ./cmd/cli
```

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
```

### 命令分组（36 接口）

| group | actions |
|-------|---------|
| auth | login |
| task | create, export-struct, export-result, download |
| upload | file, parse-xml |
| moc | add-name, select-name, insert-info, generate-script |
| moc-field | add-name, select-name, update-info |
| datatype | add, query-all, enum-add, enum-query-all |
| default-record | add |
| method | add-name, update-name, delete-name, select-info |
| mml-command | upsert, get |
| command-para | upsert, list |
| mml-para | list |
| command-branch | upsert, list |
| validate | do, result |
| errorcode | shield |
| info-code | add, list |
| info-module | query-all |

## 鉴权

登录接口 (`auth login`) 使用域账号，后端通常以 **Session Cookie** 维持会话。拿到 Cookie 后，用以下任一方式给后续命令携带：

```bash
# 环境变量（推荐，CI/CD 安全注入，优先级高于配置文件）
export OMRES_AUTH_COOKIE='JSESSIONID=xxxx'
# 或运行时
./omres-cli validate do --body '{"projectId":123}' --cookie 'JSESSIONID=xxxx'
```

其它鉴权方式（若后端改用 Token/API Key/Basic）在 `defaults.auth` 中声明 `type` 即可，对应覆盖环境变量：

| 配置字段 | 环境变量 |
|----------|----------|
| defaults.server | `OMRES_SERVER` |
| defaults.auth.token | `OMRES_AUTH_TOKEN` |
| defaults.auth.api_key | `OMRES_AUTH_API_KEY` |
| defaults.auth.username | `OMRES_AUTH_USERNAME` |
| defaults.auth.password | `OMRES_AUTH_PASSWORD` |
| defaults.auth.cookie | `OMRES_AUTH_COOKIE` |

凭证不会出现在正常输出中；请勿把明文凭证提交到仓库。

## 两个需要你确认的设计决定

1. **路径前缀**：接口文档里前端路径带 `/api`、`/sbbapi`、`/list`、`/longtime` 前缀，Vue 代理会剥掉前缀再转发给后端。基础 URL `http://10.243.80.228` 若是**代理/前端入口**，就用带前缀的完整路径（当前默认）。若你要让 CLI **直连后端**（前缀已被剥掉），重新生成 swagger：
   ```bash
   STRIP_PREFIX=1 python3 tools/build_swagger.py
   go build -o omres-cli ./cmd/cli
   ```
   并把 `api_cli_config.json` 里的 path 同步改成去前缀版本。

2. **接口 33 `info-code list`**：文档注明后端实际方法路径是 `/infoCode/queryAll`，而前端调用的是 `/list/infoCode/list`。当前按前端路径（经代理）配置。若直连后端，改为 `/infoCode/queryAll`。

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
│   ├── builder.go                     # 由配置+swagger 生成 Cobra 命令
│   ├── config.go                      # 配置解析、env 覆盖、校验
│   ├── swagger.go                     # Swagger 解析与操作索引
│   ├── describe.go                    # describe 命令 + 辅助函数
│   ├── httpclient.go                  # HTTP 客户端、鉴权注入、响应封装
│   ├── jsonrpc.go                     # JSON-RPC 2.0 输出
│   ├── types.go                       # 数据模型
│   ├── api_cli_config.json            # CLI 命令配置（go:embed）
│   └── docs/swagger.json              # Swagger（go:embed，由 tools 生成）
├── tools/build_swagger.py             # 由 API 文档生成 swagger.json
└── go.mod
```
