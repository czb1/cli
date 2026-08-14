package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

)

// authGroupName 是被本文件接管的命令组名；BuildRootCommand 会跳过配置生成的同名组。
const authGroupName = "auth"

// 认证相关的 JSON-RPC 错误码与进程退出码。
const (
	CodeUnauthenticated = -32002 // 本地无有效会话 / 会话被后端拒绝

	ExitOK              = 0
	ExitError           = 1
	ExitUnauthenticated = 3 // auth status 专用：脚本可据此判断「需要重新登录」
)

// exitCode 由 auth 子命令设置，最终由 run() 返回给操作系统。
var exitCode int

func setExitCode(c int) {
	if c > exitCode {
		exitCode = c
	}
}

// ExitCode 返回本次执行应使用的进程退出码。
func ExitCode() int { return exitCode }

// 探活接口默认值：只读、无副作用，用于 `auth status --online`。
const (
	defaultProbePath = "/list/infoModule/queryAll"
	defaultProbeBody = "{}"
)

type loginOpts struct {
	username      string
	password      string
	passwordStdin bool
	body          string
	bodyFile      string
}

// buildAuthCommand 构建 auth 命令组（login / status / logout）。
// 与其它命令组不同，auth 是手写的：它需要维护本地会话文件，而不只是转发 HTTP。
func buildAuthCommand(cfg *Config, gcfg GroupConfig) *Command {
	short := gcfg.Description
	if short == "" {
		short = "登录认证与本地会话管理"
	}
	group := &Command{
		Use:   authGroupName,
		Short: short,
		Long: "登录认证与本地会话管理。\n\n" +
			"登录成功后 Cookie 会写入 " + displaySessionFile() + "（权限 0600），\n" +
			"后续所有命令自动携带，无需再传 --cookie。",
	}
	group.AddCommand(buildAuthLoginCommand(cfg))
	group.AddCommand(buildAuthStatusCommand(cfg))
	group.AddCommand(buildAuthLogoutCommand())
	return group
}

// ---- auth login ----

func buildAuthLoginCommand(cfg *Config) *Command {
	var o loginOpts

	cmd := &Command{
		Use:   "login",
		Short: "域账号登录并建立本地会话",
		Long: "域账号登录。凭证按以下优先级解析（先命中先用）：\n" +
			"  1. --password-stdin（推荐脚本/CI 使用，从标准输入读取密码）\n" +
			"  2. --username / --password\n" +
			"  3. 环境变量 " + EnvPrefix + "_AUTH_USERNAME / " + EnvPrefix + "_AUTH_PASSWORD\n" +
			"  4. --body / --body-file（原始 JSON，兼容旧用法）\n" +
			"  5. 交互式提示（仅当标准输入是终端；密码不回显）\n\n" +
			"登录成功后 Cookie 落盘到 " + displaySessionFile() + "，后续命令自动携带。\n" +
			"输出中的凭证均已打码，密码不会出现在任何输出或文件中。",
		Args: NoArgs,
		Example: "  " + CLIName + " auth login --username zhangsan\n" +
			"  " + CLIName + " auth login --username zhangsan --password-stdin < pass.txt\n" +
			"  echo '$env:PASS' | " + CLIName + " auth login -u zhangsan --password-stdin",
	}

	f := cmd.Flags()
	f.StringVarP(&o.username, "username", "u", "", "域账号用户名")
	f.StringVarP(&o.password, "password", "p", "", "密码（不推荐：会进入命令历史，建议用 --password-stdin）")
	f.BoolVar(&o.passwordStdin, "password-stdin", false, "从标准输入读取密码")
	f.StringVar(&o.body, "body", "", "原始 JSON 请求体，如 {\"userName\":\"x\",\"passwd\":\"y\"}")
	f.StringVar(&o.bodyFile, "body-file", "", "从文件读取 JSON 请求体（优先级高于 --body）")

	cmd.RunE = func(cmd *Command, args []string) error {
		id := newRequestID()
		server, timeout, _, debug := resolveRuntime(cfg)

		body, username, err := buildLoginBody(&o)
		if err != nil {
			PrintError(id, CodeInvalidParams, "Invalid Params", err.Error())
			setExitCode(ExitError)
			return nil
		}

		// 登录请求本身不携带任何历史凭证，避免旧 Cookie 干扰。
		r := &httpRequest{
			Server:  server,
			Method:  "POST",
			Path:    loginPath,
			Body:    body,
			Timeout: timeout,
			Debug:   debug,
		}
		resp, respBody, err := r.execute()
		if err != nil {
			PrintError(id, CodeInternalError, "Request Failed", err.Error())
			setExitCode(ExitError)
			return nil
		}

		payload := decodePayload(resp.Header.Get("Content-Type"), respBody)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			PrintError(id, CodeBackendError, resp.Status, payload)
			setExitCode(ExitError)
			return nil
		}
		if code, msg, ok := businessCode(payload); ok && code != 0 {
			PrintError(id, CodeBackendError, "Login Failed", map[string]interface{}{
				"code": code,
				"msg":  msg,
			})
			setExitCode(ExitError)
			return nil
		}

		cookie, expires := extractCookieWithExpiry(resp)
		if cookie == "" {
			PrintError(id, CodeInternalError, "No Session Cookie",
				"后端返回登录成功，但响应中没有 Set-Cookie，无法建立本地会话")
			setExitCode(ExitError)
			return nil
		}

		sess := &Session{
			Cookie:   cookie,
			Username: username,
			Server:   server,
			SavedAt:  time.Now().Format(time.RFC3339),
		}
		if !expires.IsZero() {
			sess.ExpiresAt = expires.Format(time.RFC3339)
		}
		if err := SaveSession(sess); err != nil {
			PrintError(id, CodeFileSaveError, "Session Save Failed", err.Error())
			setExitCode(ExitError)
			return nil
		}

		result := map[string]interface{}{
			"authenticated": true,
			"username":      username,
			"server":        server,
			"cookie":        maskCookie(cookie),
			"session_file":  sessionFile(),
			"saved_at":      sess.SavedAt,
		}
		if sess.ExpiresAt != "" {
			result["expires_at"] = sess.ExpiresAt
		} else {
			result["expires_at"] = nil
			result["ttl_hours"] = int(sessionTTL().Hours())
		}
		if _, msg, ok := businessCode(payload); ok && msg != "" {
			result["msg"] = msg
		}
		PrintSuccess(id, result)
		return nil
	}

	return cmd
}

// buildLoginBody 解析出登录请求体，并返回用于展示的用户名。
func buildLoginBody(o *loginOpts) ([]byte, string, error) {
	// 原始 JSON 优先（兼容旧用法）。
	raw := ""
	if o.bodyFile != "" {
		data, err := readFile(o.bodyFile)
		if err != nil {
			return nil, "", err
		}
		raw = string(data)
	} else if o.body != "" {
		raw = o.body
	}
	if raw != "" {
		if !validJSON([]byte(raw)) {
			return nil, "", fmt.Errorf("请求体不是合法 JSON")
		}
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &m)
		name, _ := m["userName"].(string)
		return []byte(raw), name, nil
	}

	username := firstNonEmpty(o.username, os.Getenv(EnvPrefix+"_AUTH_USERNAME"), g.username)
	if username == "" {
		if !stdinIsTerminal() {
			return nil, "", fmt.Errorf("缺少用户名：请使用 --username，或设置环境变量 " + EnvPrefix + "_AUTH_USERNAME")
		}
		v, err := promptLine("域账号用户名: ")
		if err != nil {
			return nil, "", err
		}
		username = strings.TrimSpace(v)
	}
	if username == "" {
		return nil, "", fmt.Errorf("用户名不能为空")
	}

	password, err := resolvePassword(o)
	if err != nil {
		return nil, "", err
	}
	if password == "" {
		return nil, "", fmt.Errorf("密码不能为空")
	}

	body, err := json.Marshal(map[string]string{
		"userName": username,
		"passwd":   password,
	})
	if err != nil {
		return nil, "", err
	}
	return body, username, nil
}

func errMissingPassword() error {
	return fmt.Errorf("缺少密码：请使用 --password-stdin 从标准输入传入，"+
		"或设置环境变量 %s_AUTH_PASSWORD，或在交互式终端中直接运行 %s auth login",
		EnvPrefix, CLIName)
}

func resolvePassword(o *loginOpts) (string, error) {
	if o.passwordStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("从标准输入读取密码失败: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if p := firstNonEmpty(o.password, os.Getenv(EnvPrefix+"_AUTH_PASSWORD"), g.password); p != "" {
		return p, nil
	}
	if !stdinIsTerminal() {
		return "", errMissingPassword()
	}
	pw, err := promptPassword("密码（输入不回显）: ")
	if err != nil {
		// 交互式读取失败（例如 stdin 被重定向到 /dev/null）→ 给出可执行的建议。
		return "", errMissingPassword()
	}
	return pw, nil
}

// ---- auth status ----

func buildAuthStatusCommand(cfg *Config) *Command {
	var online bool

	cmd := &Command{
		Use:   "status",
		Short: "查看当前认证状态",
		Long: "查看当前认证状态。\n\n" +
			"退出码约定（便于脚本 / Agent 分支判断）：\n" +
			"  0  已认证\n" +
			"  3  未认证或会话已过期，需要重新执行 " + CLIName + " auth login\n" +
			"  1  其它错误（如探活请求失败）\n\n" +
			"默认只做本地检查（不发网络请求）；加 --online 会用一个只读接口验证会话在后端是否仍然有效。",
		Args: NoArgs,
	}
	cmd.Flags().BoolVar(&online, "online", false, "额外向后端发起一次只读探活请求，验证会话真实有效性")

	cmd.RunE = func(cmd *Command, args []string) error {
		id := newRequestID()
		server, timeout, _, debug := resolveRuntime(cfg)

		cookie, source, sess := resolveCookieSource(cfg)
		if cookie == "" {
			PrintError(id, CodeUnauthenticated, "Not Authenticated", map[string]interface{}{
				"authenticated": false,
				"reason":        "no_session",
				"session_file":  sessionFile(),
				"hint":          "请执行: " + CLIName + " auth login --username <域账号>",
			})
			setExitCode(ExitUnauthenticated)
			return nil
		}

		result := map[string]interface{}{
			"authenticated": true,
			"source":        source,
			"server":        server,
			"cookie":        maskCookie(cookie),
			"session_file":  sessionFile(),
		}
		if sess != nil {
			if expired, reason := sess.Expired(); expired {
				PrintError(id, CodeUnauthenticated, "Session Expired", map[string]interface{}{
					"authenticated": false,
					"reason":        reason,
					"username":      sess.Username,
					"saved_at":      sess.SavedAt,
					"expires_at":    nilIfEmpty(sess.ExpiresAt),
					"session_file":  sessionFile(),
					"hint":          "会话已过期，请重新执行: " + CLIName + " auth login --username <域账号>",
				})
				setExitCode(ExitUnauthenticated)
				return nil
			}
			result["username"] = nilIfEmpty(sess.Username)
			result["saved_at"] = sess.SavedAt
			result["expires_at"] = nilIfEmpty(sess.ExpiresAt)
			result["age_seconds"] = sess.AgeSeconds()
		}

		if online {
			outcome, detail := probeSession(cfg, server, timeout, cookie, debug)
			result["online_check"] = detail
			switch outcome {
			case probeUnauthenticated:
				PrintError(id, CodeUnauthenticated, "Session Rejected", map[string]interface{}{
					"authenticated": false,
					"reason":        "rejected_by_server",
					"online_check":  detail,
					"session_file":  sessionFile(),
					"hint":          "本地会话存在但后端不认，请重新执行: " + CLIName + " auth login --username <域账号>",
				})
				setExitCode(ExitUnauthenticated)
				return nil
			case probeError:
				// 后端不可达：本地会话仍视为存在，但无法确认，按「其它错误」返回 1。
				PrintError(id, CodeInternalError, "Probe Failed", map[string]interface{}{
					"authenticated": nil,
					"reason":        "backend_unreachable",
					"online_check":  detail,
					"hint":          "无法连通后端，请检查网络或 --server 配置；这不代表会话已失效",
				})
				setExitCode(ExitError)
				return nil
			}
		}

		PrintSuccess(id, result)
		return nil
	}

	return cmd
}

// resolveCookieSource 返回当前生效的 Cookie 及其来源。
// 优先级与实际请求时保持一致：命令行 > 环境变量 > 配置 > 会话文件。
func resolveCookieSource(cfg *Config) (cookie string, source string, sess *Session) {
	if g.cookie != "" {
		return g.cookie, "flag:--cookie", nil
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_COOKIE"); v != "" {
		return v, "env:" + EnvPrefix + "_AUTH_COOKIE", nil
	}
	if cfg.Defaults.Auth != nil && cfg.Defaults.Auth.Cookie != "" {
		return cfg.Defaults.Auth.Cookie, "config", nil
	}
	s, err := LoadSession()
	if err != nil || s == nil {
		return "", "", nil
	}
	return s.Cookie, "session_file", s
}

// probeResult 是探活的三态结果。网络不通 ≠ 未认证，必须区分开，
// 否则一次网络抖动就会让上层误判为「需要重新登录」。
type probeResult int

const (
	probeOK probeResult = iota
	probeUnauthenticated
	probeError
)

// probeSession 用一个只读接口验证会话在后端是否仍然有效。
func probeSession(cfg *Config, server string, timeout int, cookie string, debug bool) (probeResult, map[string]interface{}) {
	path, body := defaultProbePath, defaultProbeBody
	if a := cfg.Defaults.Auth; a != nil {
		if a.ProbePath != "" {
			path = a.ProbePath
		}
		if a.ProbeBody != "" {
			body = a.ProbeBody
		}
	}

	r := &httpRequest{
		Server:  server,
		Method:  "POST",
		Path:    path,
		Body:    []byte(body),
		Auth:    &AuthConfig{Type: "cookie", Cookie: cookie},
		Timeout: timeout,
		Debug:   debug,
	}
	resp, respBody, err := r.execute()
	detail := map[string]interface{}{"probe": path}
	if err != nil {
		detail["ok"] = false
		detail["error"] = err.Error()
		return probeError, detail
	}

	detail["http_status"] = resp.StatusCode
	payload := decodePayload(resp.Header.Get("Content-Type"), respBody)

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		detail["ok"] = false
		detail["reason"] = "http_" + strconv.Itoa(resp.StatusCode)
		return probeUnauthenticated, detail
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail["ok"] = false
		detail["reason"] = resp.Status
		return probeError, detail
	}
	// 后端常以 2xx + 业务码表达「未登录」，需要额外识别。
	if code, msg, ok := businessCode(payload); ok && code != 0 && looksUnauthenticated(msg) {
		detail["ok"] = false
		detail["reason"] = "backend_says_unauthenticated"
		detail["code"] = code
		detail["msg"] = msg
		return probeUnauthenticated, detail
	}
	// 会话失效时部分网关会重定向到 HTML 登录页。
	if s, isStr := payload.(string); isStr && looksLikeLoginPage(s) {
		detail["ok"] = false
		detail["reason"] = "redirected_to_login_page"
		return probeUnauthenticated, detail
	}

	detail["ok"] = true
	return probeOK, detail
}

func looksUnauthenticated(msg string) bool {
	m := strings.ToLower(msg)
	for _, kw := range []string{"未登录", "请登录", "登录超时", "会话", "无权限", "unauthorized", "not login", "session", "token"} {
		if strings.Contains(m, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func looksLikeLoginPage(s string) bool {
	head := strings.ToLower(s)
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.Contains(head, "<html") && strings.Contains(head, "login")
}

// ---- auth logout ----

func buildAuthLogoutCommand() *Command {
	return &Command{
		Use:   "logout",
		Short: "清除本地会话",
		Long:  "删除本地会话文件。该操作是幂等的：没有会话时同样返回成功。",
		Args:  NoArgs,
		RunE: func(cmd *Command, args []string) error {
			id := newRequestID()
			removed, err := ClearSession()
			if err != nil {
				PrintError(id, CodeInternalError, "Logout Failed", err.Error())
				setExitCode(ExitError)
				return nil
			}
			PrintSuccess(id, map[string]interface{}{
				"authenticated": false,
				"cleared":       removed,
				"session_file":  sessionFile(),
			})
			return nil
		},
	}
}

// ---- helpers ----

// businessCode 提取后端统一响应中的 code / msg。
func businessCode(payload interface{}) (float64, string, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return 0, "", false
	}
	msg, _ := m["msg"].(string)
	if msg == "" {
		msg, _ = m["message"].(string)
	}
	raw, ok := m["code"]
	if !ok {
		return 0, msg, false
	}
	switch v := raw.(type) {
	case float64:
		return v, msg, true
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n, msg, true
		}
	}
	return 0, msg, false
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// displaySessionFile 用于帮助文本，取不到主目录时给出通用写法。
func displaySessionFile() string {
	if p := sessionFile(); p != "" {
		return p
	}
	return "~/.omres-cli/" + sessionFileName
}
