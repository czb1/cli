package cli

import (
	_ "embed"
	"fmt"
	"os"
	"time"
)

//go:embed api_cli_config.json
var configData []byte

//go:embed docs/swagger.json
var swaggerData []byte

// upgradeWaitBudget 是命令跑完后为升级检查预留的等待时间。
// 如果此时正在写入新二进制，WaitForUpgradeChecker 会继续等到写完。
const upgradeWaitBudget = 2 * time.Second

// Run is the CLI entrypoint. It loads embedded config + swagger, validates the
// two against each other, builds the command tree, and executes.
func Run() {
	// 后台检查升级；日志只走 stderr，不影响 stdout 的 JSON-RPC 契约。
	StartUpgradeChecker()

	code := run()

	// 注意：不能用 defer + os.Exit（os.Exit 不执行 defer），所以显式在这里等待。
	WaitForUpgradeChecker(upgradeWaitBudget)
	os.Exit(code)
}

func run() int {
	cfg, err := LoadConfig(configData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置错误:", err)
		return 1
	}
	sw, err := ParseSwagger(swaggerData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Swagger 错误:", err)
		return 1
	}
	if err := ValidateConfig(cfg, sw); err != nil {
		fmt.Fprintln(os.Stderr, "校验错误:", err)
		return 1
	}
	root := BuildRootCommand(cfg, sw)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return ExitError
	}
	// auth 子命令会通过 setExitCode 表达认证状态（3 = 未认证）。
	return ExitCode()
}
