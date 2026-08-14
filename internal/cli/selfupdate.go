package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// 本文件替代 github.com/inconshreveable/go-update：内网无法拉取该依赖，
// 而我们只用到它 "原子替换自身可执行文件" 这一个能力，标准库足够。
//
// 替换流程（与 go-update 一致）：
//  1. 新二进制先写到同目录的 .<name>.new —— 同目录才能保证 rename 是同一文件系统上的原子操作
//  2. 把正在运行的可执行文件 rename 成 .<name>.old —— Windows 不允许覆盖运行中的 exe，但允许改名
//  3. .new rename 成正式名字；失败则把 .old 改回去回滚
//  4. 删除 .old；Windows 上当前进程仍占用它，留到下次启动由 cleanupOldBinary 清理

func executablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Abs(p)
}

// applyUpdate 用 r 中的内容替换当前可执行文件。
func applyUpdate(r io.Reader) error {
	target, err := executablePath()
	if err != nil {
		return fmt.Errorf("定位当前可执行文件失败: %w", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("读取下载内容失败: %w", err)
	}
	if err := checkExecutable(data); err != nil {
		return err
	}

	dir := filepath.Dir(target)
	base := filepath.Base(target)
	newPath := filepath.Join(dir, "."+base+".new")
	oldPath := filepath.Join(dir, "."+base+".old")

	mode := os.FileMode(0o755)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}

	_ = os.Remove(newPath)
	if err := os.WriteFile(newPath, data, mode); err != nil {
		return fmt.Errorf("写入新二进制失败 (%s): %w", newPath, err)
	}
	// 有些文件系统会忽略 WriteFile 的 perm（比如 umask），显式再 chmod 一次。
	_ = os.Chmod(newPath, mode)

	_ = os.Remove(oldPath)
	if err := os.Rename(target, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("重命名当前二进制失败（目录无写权限？）: %w", err)
	}

	if err := os.Rename(newPath, target); err != nil {
		// 回滚：把旧的改回来，保证进程下次还能启动。
		if rbErr := os.Rename(oldPath, target); rbErr != nil {
			return fmt.Errorf("替换失败且回滚失败，请手动将 %s 改名为 %s（原始错误: %v，回滚错误: %v）",
				oldPath, target, err, rbErr)
		}
		_ = os.Remove(newPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	if err := os.Remove(oldPath); err != nil && runtime.GOOS != "windows" {
		upgradeDebug("清理 %s 失败: %v", oldPath, err)
	}
	return nil
}

// cleanupOldBinary 清理上一次升级遗留的 .old（主要是 Windows：升级时该文件仍被占用）。
func cleanupOldBinary() {
	target, err := executablePath()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".old"))
}

// checkExecutable 兜底校验：文件服务器返回错误页 / 空响应时，
// 绝不能把 HTML 当成二进制写进去——那会让 CLI 彻底无法启动，且无法自愈。
func checkExecutable(data []byte) error {
	if len(data) < 4096 {
		return fmt.Errorf("下载内容仅 %d 字节，不像可执行文件", len(data))
	}
	switch {
	case bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}): // Linux ELF
		return nil
	case bytes.HasPrefix(data, []byte{'M', 'Z'}): // Windows PE
		return nil
	case bytes.HasPrefix(data, []byte{0xcf, 0xfa, 0xed, 0xfe}), // macOS Mach-O 64
		bytes.HasPrefix(data, []byte{0xca, 0xfe, 0xba, 0xbe}): // macOS universal
		return nil
	}
	return fmt.Errorf("下载内容不是可执行文件（可能是错误页面或被代理拦截）")
}
