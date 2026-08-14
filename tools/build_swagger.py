package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

)

// ---- 构建信息 ----
//
// 由流水线通过 ldflags 注入到 package main，再由 main 调用 SetBuildInfo 传进来。
// 不直接在本包声明 -X 目标，是为了让流水线里的
//   -X 'main.BuildTime=...' -X 'main.CommitID=...' -X 'main.BuildFlavor=...'
// 保持原样可用（-X 的 key 是 importpath.varname，写 internal 包要用全路径，易错）。
var (
	BuildTime   = "unknown"
	CommitID    = "unknown"
	BuildFlavor = "" // product | gray | hlt；空 = 本地开发构建，不参与自动升级
)

// DisableUpgrade 可在编译期彻底关闭自动升级：
//
//	-X 'omres-cli/internal/cli.DisableUpgrade=true'
var DisableUpgrade = "false"

// SetBuildInfo 由 package main 在启动时调用。
func SetBuildInfo(buildTime, commitID, flavor string) {
	if buildTime != "" {
		BuildTime = buildTime
	}
	if commitID != "" {
		CommitID = commitID
	}
	BuildFlavor = flavor
}

const (
	// 与流水线中的 FILESERVER 保持一致：
	//   http://7.183.28.77:9155/omres-cli-arti/omres-cli-${PUBLISH_TYPE}
	upgradeURLBase = "http://7.183.28.77:9155/omres-cli-arti/omres-cli-"

	maxRetryTimes   = 3
	infoTimeout     = 3 * time.Second   // 版本探测：必须短，别拖慢每次调用
	downloadTimeout = 120 * time.Second // 真正下载新二进制
	checkInterval   = time.Hour         // 节流：AI Agent 可能高频调用，别每次都打文件服务器
)

var (
	upgradeDone = make(chan struct{})
	applying    atomic.Bool // 正在写入新二进制，此时不能提前退出进程
	upgradeOnce atomic.Bool
)

// StartUpgradeChecker 在后台检查并应用升级。非阻塞。
func StartUpgradeChecker() {
	if upgradeOnce.Swap(true) {
		return
	}
	cleanupOldBinary() // 清理上次升级遗留的 .old（Windows）
	if skipUpgrade() {
		close(upgradeDone)
		return
	}
	go func() {
		defer close(upgradeDone)
		defer func() {
			if r := recover(); r != nil {
				upgradeDebug("panic: %v", r)
			}
		}()
		checkAndUpgrade()
	}()
}

// WaitForUpgradeChecker 在主流程结束时调用。
// 正常情况下最多等 max；但如果此刻正在写入新二进制，必须等它写完，
// 否则进程退出会留下一个残缺的可执行文件。
func WaitForUpgradeChecker(max time.Duration) {
	select {
	case <-upgradeDone:
		return
	case <-time.After(max):
		if applying.Load() {
			upgradeLog("正在下载新版本，请稍候…")
			<-upgradeDone
		}
		return
	}
}

// skipUpgrade 判断是否跳过自动升级。
func skipUpgrade() bool {
	if DisableUpgrade == "true" {
		return true
	}
	// 本地开发构建（没有 BuildFlavor）绝不自动覆盖，避免调试时二进制被线上版本顶掉。
	if BuildFlavor == "" {
		return true
	}
	switch strings.ToLower(os.Getenv(EnvPrefix + "_NO_UPGRADE")) {
	case "1", "true", "yes", "on":
		return true
	}
	for _, a := range os.Args[1:] {
		switch a {
		case "--no-upgrade":
			return true
		// 纯本地命令（AI 探索路径）不触发任何网络行为。
		case "describe", "version", "help", "--help", "-h", "completion":
			return true
		case "--":
			return false
		}
	}
	return false
}

func binaryName() string {
	switch {
	case runtime.GOOS == "windows":
		return "omres-cli.exe"
	case runtime.GOARCH != "amd64":
		return "omres-cli-arm"
	default:
		return "omres-cli"
	}
}

func getUpgradeURL() string {
	return fmt.Sprintf("%s%s/%s", upgradeURLBase, BuildFlavor, binaryName())
}

func checkAndUpgrade() {
	if recentlyChecked() {
		upgradeDebug("距上次检查不足 %s，跳过", checkInterval)
		return
	}

	url := getUpgradeURL()
	remoteMtime, err := getRemoteMtime(url)
	if err != nil {
		upgradeDebug("获取远端版本失败: %v", err)
		return
	}
	// 探测成功即记录，避免下载连续失败时每次调用都重试。
	markChecked()

	localMtime := getLocalMtime()
	if localMtime >= remoteMtime {
		upgradeDebug("已是最新 (local=%d remote=%d)", localMtime, remoteMtime)
		return
	}

	applying.Store(true)
	defer applying.Store(false)

	var lastErr error
	for i := 0; i < maxRetryTimes; i++ {
		if err := downloadAndUpgrade(url); err != nil {
			lastErr = err
			upgradeDebug("第 %d 次升级失败: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		// 注意：不 os.Exit。当前命令继续用旧的内存镜像跑完，
		// 新二进制已落盘，下一次调用即生效。这样不会打断正在进行的 Agent 调用。
		upgradeLog("已升级到最新版本，下次执行生效")
		return
	}
	upgradeLog("自动升级失败（不影响本次命令）: %v", lastErr)
}

func getLocalMtime() int64 {
	execPath, err := executablePath()
	if err != nil {
		return 0
	}
	info, err := os.Stat(execPath)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixMilli()
}

func getRemoteMtime(url string) (int64, error) {
	client := &http.Client{Timeout: infoTimeout}
	resp, err := client.Get(url + "?op=info")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("http status: %d", resp.StatusCode)
	}
	var body struct {
		Mtime int64 `json:"mtime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.Mtime == 0 {
		return 0, fmt.Errorf("远端未返回 mtime")
	}
	return body.Mtime, nil
}

func downloadAndUpgrade(url string) error {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status: %d", resp.StatusCode)
	}
	return applyUpdate(resp.Body) // 见 selfupdate.go：纯标准库的原子替换
}

// ---- 检查节流：~/.omres-cli/upgrade.json ----

type upgradeState struct {
	LastCheck string `json:"last_check"`
}

func upgradeStateFile() string {
	d := sessionDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "upgrade.json")
}

func recentlyChecked() bool {
	p := upgradeStateFile()
	if p == "" {
		return false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	var st upgradeState
	if err := json.Unmarshal(raw, &st); err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, st.LastCheck)
	if err != nil {
		return false
	}
	return time.Since(t) < checkInterval
}

func markChecked() {
	dir := sessionDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(upgradeState{LastCheck: time.Now().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(upgradeStateFile(), raw, 0o600)
}

// ---- 日志：只写 stderr，绝不污染 stdout 的 JSON-RPC 输出 ----

func upgradeLog(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "[omres-cli:upgrade] "+format+"\n", a...)
}

func upgradeDebug(format string, a ...interface{}) {
	if !debugEnabled() {
		return
	}
	upgradeLog(format, a...)
}

// debugEnabled 在 cobra 解析 flag 之前就要能判断，因此直接扫 os.Args。
func debugEnabled() bool {
	if v := strings.ToLower(os.Getenv(EnvPrefix + "_DEBUG")); v == "1" || v == "true" {
		return true
	}
	for _, a := range os.Args[1:] {
		if a == "--debug" {
			return true
		}
	}
	return false
}

// ---- version 命令 ----

func buildVersionCommand() *Command {
	return &Command{
		Use:   "version",
		Short: "输出版本与构建信息",
		Args:  NoArgs,
		RunE: func(cmd *Command, args []string) error {
			flavor := BuildFlavor
			if flavor == "" {
				flavor = "dev"
			}
			info := map[string]interface{}{
				"build_time":   BuildTime,
				"commit_id":    CommitID,
				"flavor":       flavor,
				"go_version":   runtime.Version(),
				"platform":     runtime.GOOS + "/" + runtime.GOARCH,
				"binary_mtime": getLocalMtime(),
				"auto_upgrade": !skipUpgradeStatic(),
			}
			if BuildFlavor != "" {
				info["upgrade_url"] = getUpgradeURL()
			}
			PrintSuccess(newRequestID(), info)
			return nil
		},
	}
}

// skipUpgradeStatic 只判断编译期/环境层面的开关，忽略当前命令行（用于 version 展示）。
func skipUpgradeStatic() bool {
	if DisableUpgrade == "true" || BuildFlavor == "" {
		return true
	}
	switch strings.ToLower(os.Getenv(EnvPrefix + "_NO_UPGRADE")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
