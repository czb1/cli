package cli

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

)

// CLIName is the tool binary name used in help and examples.
const CLIName = "omres-cli"

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// globalOpts holds runtime overrides bound to root persistent flags.
type globalOpts struct {
	server   string
	timeout  int
	debug    bool
	token    string
	apiKey   string
	username string
	password string
	cookie   string
}

var g globalOpts

// BuildRootCommand assembles the full command tree.
func BuildRootCommand(cfg *Config, sw *Swagger) *Command {
	root := &Command{
		Use:   CLIName,
		Short: "OMResTool AI 友好命令行工具",
		Long: CLIName + " 将 OMResTool 的 HTTP 接口封装为面向 AI Agent / 脚本的命令行。\n" +
			"探索路径: `" + CLIName + " --help` → `" + CLIName + " <group> --help` → `" +
			CLIName + " describe <group> <action>`。\n所有输出均为 JSON-RPC 2.0。",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.server, "server", "", "服务器地址，覆盖配置 (env "+EnvPrefix+"_SERVER)")
	pf.IntVar(&g.timeout, "timeout", 0, "请求超时秒数，覆盖配置")
	pf.BoolVar(&g.debug, "debug", false, "开启调试日志")
	pf.StringVar(&g.token, "token", "", "Bearer Token，覆盖配置 (env "+EnvPrefix+"_AUTH_TOKEN)")
	pf.StringVar(&g.apiKey, "api-key", "", "API Key，覆盖配置 (env "+EnvPrefix+"_AUTH_API_KEY)")
	pf.StringVar(&g.username, "username", "", "Basic Auth 用户名")
	pf.StringVar(&g.password, "password", "", "Basic Auth 密码")
	pf.StringVar(&g.cookie, "cookie", "", "Cookie，覆盖配置 (env "+EnvPrefix+"_AUTH_COOKIE)")
	pf.Bool("no-upgrade", false, "跳过本次自动升级检查 (env "+EnvPrefix+"_NO_UPGRADE)")

	// Stable group ordering for readable help.
	names := make([]string, 0, len(cfg.Groups))
	for name := range cfg.Groups {
		names = append(names, name)
	}
	sort.Strings(names)

	hasAuthGroup := false
	for _, name := range names {
		gcfg := cfg.Groups[name]
		// auth 组由 auth.go 手写接管：它要维护本地会话文件，不只是转发 HTTP。
		if name == authGroupName {
			hasAuthGroup = true
			root.AddCommand(buildAuthCommand(cfg, gcfg))
			continue
		}
		root.AddCommand(buildGroupCommand(cfg, sw, name, gcfg))
	}
	if !hasAuthGroup {
		root.AddCommand(buildAuthCommand(cfg, GroupConfig{}))
	}

	root.AddCommand(buildDescribeCommand(cfg, sw))
	root.AddCommand(buildRawCommand(cfg))
	root.AddCommand(buildVersionCommand())
	return root
}

func buildGroupCommand(cfg *Config, sw *Swagger, name string, gcfg GroupConfig) *Command {
	short := gcfg.Description
	if short == "" {
		short = name + " 命令组"
	}
	group := &Command{
		Use:   name,
		Short: short,
	}
	for i := range gcfg.Resources {
		res := gcfg.Resources[i]
		op, ok := sw.Lookup(res.HTTP.Path, res.HTTP.Method)
		if !ok {
			continue // validation already guaranteed presence
		}
		group.AddCommand(buildActionCommand(cfg, sw, name, res, op))
	}
	return group
}

func buildActionCommand(cfg *Config, sw *Swagger, group string, res Resource, op *Operation) *Command {
	c := classify(op)
	pathParamNames := extractPathParams(res.HTTP.Path)

	use := res.CLI.Action
	for _, pn := range pathParamNames {
		use += " <" + pn + ">"
	}

	short := res.CLI.Description
	if short == "" {
		short = op.Summary
	}

	cmd := &Command{
		Use:   use,
		Short: short,
		Long:  buildActionLong(group, res, op, c, pathParamNames),
		Args:  ExactArgs(len(pathParamNames)),
	}

	// Register query flags.
	for _, p := range c.query {
		if p.Type == "array" {
			cmd.Flags().StringArray(p.Name, nil, flagHelp(p))
		} else {
			cmd.Flags().String(p.Name, "", flagHelp(p))
		}
	}
	// Register body flags.
	if c.body != nil {
		cmd.Flags().String("body", "", "JSON 请求体")
		cmd.Flags().String("body-file", "", "从文件读取 JSON 请求体（优先级高于 --body）")
	}
	// Register formData flags.
	for _, p := range c.form {
		cmd.Flags().String(p.Name, "", flagHelp(p))
	}
	// 破坏性命令（删除类）额外提供确认开关，避免误操作。
	destructive := isDestructive(res.HTTP.Method, res.HTTP.Path)
	if destructive {
		cmd.Flags().Bool("yes", false, "跳过破坏性操作确认")
		cmd.Flags().Bool("dry-run", false, "只打印将要发送的请求，不实际发送")
	}

	cmd.RunE = func(cmd *Command, args []string) error {
		id := newRequestID()

		// Substitute path params.
		reqPath := res.HTTP.Path
		for i, pn := range pathParamNames {
			reqPath = strings.Replace(reqPath, "{"+pn+"}", url.PathEscape(args[i]), 1)
		}

		// Query params.
		query := url.Values{}
		for _, p := range c.query {
			if !cmd.Flags().Changed(p.Name) {
				continue
			}
			if p.Type == "array" {
				vals, _ := cmd.Flags().GetStringArray(p.Name)
				for _, v := range vals {
					query.Add(p.Name, v)
				}
			} else {
				v, _ := cmd.Flags().GetString(p.Name)
				query.Set(p.Name, v)
			}
		}

		// Body.
		var body []byte
		if c.body != nil {
			bf, _ := cmd.Flags().GetString("body-file")
			bstr, _ := cmd.Flags().GetString("body")
			if bf != "" {
				data, err := readFile(bf)
				if err != nil {
					PrintError(id, CodeInvalidParams, "Invalid Params", err.Error())
					return nil
				}
				body = data
			} else if bstr != "" {
				body = []byte(bstr)
			}
			if len(body) > 0 && !validJSON(body) {
				PrintError(id, CodeParseError, "Parse Error", "请求体不是合法 JSON")
				return nil
			}
		}

		// Form fields.
		form := map[string]string{}
		files := map[string]string{}
		for _, p := range c.form {
			if !cmd.Flags().Changed(p.Name) {
				continue
			}
			v, _ := cmd.Flags().GetString(p.Name)
			if p.Type == "file" {
				files[p.Name] = v
			} else {
				form[p.Name] = v
			}
		}

		server, timeout, auth, debug := resolveRuntime(cfg)

		if destructive {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				printDryRun(id, res.HTTP.Method, server, reqPath, query, body)
				return nil
			}
			assumeYes, _ := cmd.Flags().GetBool("yes")
			if !confirmDestructive(id, res.HTTP.Method, reqPath, assumeYes) {
				setExitCode(ExitError)
				return nil
			}
		}

		r := &httpRequest{
			Server:  server,
			Method:  res.HTTP.Method,
			Path:    reqPath,
			Query:   query,
			Body:    body,
			Form:    form,
			Files:   files,
			Auth:    auth,
			Timeout: timeout,
			Debug:   debug,
		}
		resp, respBody, err := r.execute()
		formatResponse(id, server, reqPath, op, resp, respBody, err)
		return nil
	}

	return cmd
}

// resolveRuntime merges config defaults with runtime global flags.
func resolveRuntime(cfg *Config) (server string, timeout int, auth *AuthConfig, debug bool) {
	server = cfg.Defaults.Server
	if g.server != "" {
		server = g.server
	}
	timeout = cfg.Defaults.Timeout
	if g.timeout > 0 {
		timeout = g.timeout
	}
	debug = g.debug
	auth = mergeAuth(cfg.Defaults.Auth)
	return
}

func mergeAuth(base *AuthConfig) *AuthConfig {
	var a AuthConfig
	if base != nil {
		a = *base
	}
	switch {
	case g.token != "":
		a.Token = g.token
		a.Type = "bearer"
	case g.apiKey != "":
		a.APIKey = g.apiKey
		a.Type = "api_key"
	case g.username != "" || g.password != "":
		if g.username != "" {
			a.Username = g.username
		}
		if g.password != "" {
			a.Password = g.password
		}
		a.Type = "basic"
	case g.cookie != "":
		a.Cookie = g.cookie
		a.Type = "cookie"
	default:
		// No explicit auth flag: auto-load persisted session cookie.
		if a.Cookie == "" {
			if cookie, err := LoadSessionCookie(); err == nil && cookie != "" {
				a.Cookie = cookie
				a.Type = "cookie"
			}
		}
	}
	if a.Type == "" && base == nil {
		return nil
	}
	return &a
}

func extractPathParams(path string) []string {
	var out []string
	for _, m := range pathParamRe.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

func flagHelp(p Parameter) string {
	s := p.Description
	if p.Required {
		s = "[必填] " + s
	}
	if p.Type != "" && p.Type != "string" {
		s = s + " (" + p.Type + ")"
	}
	return s
}

func buildActionLong(group string, res Resource, op *Operation, c classifiedParams, pathParamNames []string) string {
	var b strings.Builder
	desc := res.CLI.Description
	if desc == "" {
		desc = op.Summary
	}
	b.WriteString(desc + "\n\n")
	b.WriteString(fmt.Sprintf("HTTP: %s %s\n", strings.ToUpper(res.HTTP.Method), res.HTTP.Path))
	if isDestructive(res.HTTP.Method, res.HTTP.Path) {
		b.WriteString("\n⚠ 破坏性操作：需要 --yes 确认；可用 --dry-run 预览请求。\n")
	}
	if len(pathParamNames) > 0 {
		b.WriteString("\n路径参数(按顺序):\n")
		for _, pn := range pathParamNames {
			b.WriteString("  " + pn + "\n")
		}
	}
	if res.CLI.Example != "" {
		b.WriteString("\n示例:\n  " + res.CLI.Example + "\n")
	}
	b.WriteString("\n完整参数请运行: " + CLIName + " describe " + group + " " + res.CLI.Action)
	return b.String()
}
