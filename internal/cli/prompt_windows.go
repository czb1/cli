//go:build windows

package cli

import (
	"bufio"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// 只用标准库 syscall 调 kernel32，不引入 golang.org/x/term 依赖。
var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableEchoInput = 0x0004

// stdinIsTerminal 判断标准输入是否为真正的控制台。
// GetConsoleMode 只对控制台句柄成功，NUL / 管道 / 文件重定向都会失败，
// 正好是需要的语义。
func stdinIsTerminal() bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(
		uintptr(syscall.Handle(os.Stdin.Fd())),
		uintptr(unsafe.Pointer(&mode)),
	)
	return r != 0
}

// readPasswordNoEcho 关闭回显读取一行。非控制台（管道等）返回 ok=false。
func readPasswordNoEcho() (string, bool) {
	handle := syscall.Handle(os.Stdin.Fd())

	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return "", false
	}
	if r, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(mode&^enableEchoInput)); r == 0 {
		return "", false
	}
	defer procSetConsoleMode.Call(uintptr(handle), uintptr(mode))

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}
