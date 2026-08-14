package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// stdinIsTerminal 判断标准输入是否为真正的终端。
//
// 实现见 prompt_windows.go / prompt_unix.go。
// 注意不能只判断 os.ModeCharDevice：/dev/null 与 Windows 的 NUL 都是字符设备，
// 但都不是终端。脚本与 AI Agent 常把 stdin 重定向到它们，
// 误判会导致命令弹出永远等不到输入的交互提示。

// promptLine 输出提示并读取一行。提示写 stderr，保持 stdout 的 JSON-RPC 契约干净。
func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptPassword 读取密码，尽可能关闭回显。
// 平台相关实现见 prompt_windows.go / prompt_unix.go；关闭回显失败时降级为明文输入并给出告警。
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if pw, ok := readPasswordNoEcho(); ok {
		fmt.Fprintln(os.Stderr)
		return pw, nil
	}
	fmt.Fprintln(os.Stderr, "\n[警告] 当前终端无法关闭回显，密码将明文显示。建议改用 --password-stdin。")
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("读取密码失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
