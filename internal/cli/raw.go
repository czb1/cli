package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

)

// CodeConfirmRequired 表示命令被确认门禁拦下，不是执行失败。
const CodeConfirmRequired = -32004

// destructiveHints 是被视为「破坏性」的路径特征。
// 命中任意一项即要求显式确认，避免误删。
var destructiveHints = []string{
	"delete", "remove", "del", "drop", "clear", "clean", "purge", "reset",
}

// destructiveMethods 是被视为「破坏性」的 HTTP 方法。
var destructiveMethods = map[string]bool{
	"DELETE": true,
}

// isDestructive 判断一次调用是否需要二次确认。
// 注意：POST 也可能是删除（本后端的删除接口就是 POST），所以路径特征同样参与判断。
func isDestructive(method, path string) bool {
	if destructiveMethods[strings.ToUpper(method)] {
		return true
	}
	lower := strings.ToLower(path)
	for _, h := range destructiveHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

// confirmDestructive 在执行破坏性操作前要求确认。
// 返回 true 表示可以继续。
//
// 设计要点：非交互环境（脚本 / AI Agent）**不会**挂起等待输入，
// 而是直接拒绝并提示加 --yes，避免命令卡死或被静默执行。
func confirmDestructive(id, method, path string, assumeYes bool) bool {
	if assumeYes {
		return true
	}

	if !stdinIsTerminal() {
		PrintError(id, CodeConfirmRequired, "Confirmation Required", map[string]interface{}{
			"method": strings.ToUpper(method),
			"path":   path,
			"reason": "destructive_operation",
			"hint":   "这是破坏性操作。确认无误后请加 --yes 重新执行。",
		})
		return false
	}

	fmt.Fprintf(os.Stderr, "即将执行破坏性操作: %s %s\n", strings.ToUpper(method), path)
	fmt.Fprint(os.Stderr, "确认继续？输入 yes 回车: ")
	line, err := promptLine("")
	if err != nil || strings.TrimSpace(strings.ToLower(line)) != "yes" {
		PrintError(id, CodeConfirmRequired, "Confirmation Declined", map[string]interface{}{
			"method": strings.ToUpper(method),
			"path":   path,
			"hint":   "已取消，未发送任何请求。",
		})
		return false
	}
	return true
}

// buildRawCommand 构建 `raw` 命令：直调任意后端接口。
//
// 存在意义：当某个接口尚未收录进 api_cli_config.json / swagger.json 时，
// 调用方仍能通过本命令访问，而**不必**去读 ~/.omres-cli/session.json 里的
// Cookie 自己拼 curl。裸调 curl 会绕开会话管理、错误码规范与确认门禁，
// 是需要极力避免的做法。
func buildRawCommand(cfg *Config) *Command {
	var (
		method    string
		body      string
		bodyFile  string
		queryArgs []string
		headers   []string
		assumeYes bool
		dryRun    bool
	)

	cmd := &Command{
		Use:   "raw <path>",
		Short: "直调任意后端接口（用于尚未收录的接口）",
		Long: "直调任意后端接口，自动携带当前会话认证，输出同样遵循 JSON-RPC 2.0。\n\n" +
			"用途：某接口还没写进配置时的临时通道。\n" +
			"**不要**为此去读会话文件拼 curl —— 那样会绕开会话管理、\n" +
			"统一错误码与破坏性操作确认。\n\n" +
			"路径以 / 开头，不含服务器地址。\n" +
			"命中删除类特征（路径含 delete/remove 等，或方法为 DELETE）时，\n" +
			"需要 --yes 才会真正发送。",
		Args: ExactArgs(1),
		Example: "  " + CLIName + " raw /api/task/list -X POST --body '{}'\n" +
			"  " + CLIName + " raw /api/task/delete -X POST --body '{\"taskId\":123}' --yes\n" +
			"  " + CLIName + " raw /api/task/delete -X POST --body '{\"taskId\":123}' --dry-run",
	}

	f := cmd.Flags()
	f.StringVarP(&method, "method", "X", "POST", "HTTP 方法 (GET/POST/PUT/DELETE)")
	f.StringVar(&body, "body", "", "JSON 请求体")
	f.StringVar(&bodyFile, "body-file", "", "从文件读取 JSON 请求体（优先级高于 --body）")
	f.StringArrayVar(&queryArgs, "query", nil, "查询参数，形如 key=value，可重复")
	f.StringArrayVarP(&headers, "header", "H", nil, "额外请求头，形如 'Name: value'，可重复")
	f.BoolVar(&assumeYes, "yes", false, "跳过破坏性操作确认")
	f.BoolVar(&dryRun, "dry-run", false, "只打印将要发送的请求，不实际发送")

	cmd.RunE = func(cmd *Command, args []string) error {
		id := newRequestID()
		reqPath := args[0]

		if !strings.HasPrefix(reqPath, "/") {
			PrintError(id, CodeInvalidParams, "Invalid Params",
				"路径必须以 / 开头，且不包含服务器地址")
			setExitCode(ExitError)
			return nil
		}

		// 请求体
		var payload []byte
		if bodyFile != "" {
			data, err := readFile(bodyFile)
			if err != nil {
				PrintError(id, CodeInvalidParams, "Invalid Params", err.Error())
				setExitCode(ExitError)
				return nil
			}
			payload = data
		} else if body != "" {
			payload = []byte(body)
		}
		if len(payload) > 0 && !validJSON(payload) {
			PrintError(id, CodeParseError, "Parse Error", "请求体不是合法 JSON")
			setExitCode(ExitError)
			return nil
		}

		// 查询参数
		query := url.Values{}
		for _, kv := range queryArgs {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				PrintError(id, CodeInvalidParams, "Invalid Params",
					"查询参数格式应为 key=value: "+kv)
				setExitCode(ExitError)
				return nil
			}
			query.Add(parts[0], parts[1])
		}

		// 额外请求头
		extraHeaders := map[string]string{}
		for _, h := range headers {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) != 2 {
				PrintError(id, CodeInvalidParams, "Invalid Params",
					"请求头格式应为 'Name: value': "+h)
				setExitCode(ExitError)
				return nil
			}
			extraHeaders[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}

		server, timeout, auth, debug := resolveRuntime(cfg)

		if dryRun {
			printDryRun(id, method, server, reqPath, query, payload)
			return nil
		}

		if isDestructive(method, reqPath) && !confirmDestructive(id, method, reqPath, assumeYes) {
			setExitCode(ExitError)
			return nil
		}

		r := &httpRequest{
			Server:  server,
			Method:  strings.ToUpper(method),
			Path:    reqPath,
			Query:   query,
			Body:    payload,
			Headers: extraHeaders,
			Auth:    auth,
			Timeout: timeout,
			Debug:   debug,
		}
		resp, respBody, err := r.execute()
		formatResponse(id, server, reqPath, nil, resp, respBody, err)
		return nil
	}

	return cmd
}

// printDryRun 输出将要发送的请求，不实际发送。
// raw 命令与配置驱动的破坏性命令共用。
func printDryRun(id, method, server, reqPath string, query url.Values, body []byte) {
	preview := map[string]interface{}{
		"dry_run":     true,
		"method":      strings.ToUpper(method),
		"url":         strings.TrimRight(server, "/") + reqPath,
		"destructive": isDestructive(method, reqPath),
	}
	if len(query) > 0 {
		preview["query"] = query.Encode()
	}
	if len(body) > 0 {
		var pretty interface{}
		if json.Unmarshal(body, &pretty) == nil {
			preview["body"] = pretty
		} else {
			preview["body"] = string(body)
		}
	}
	PrintSuccess(id, preview)
}
