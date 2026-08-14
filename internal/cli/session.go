package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sessionFileName = "session.json"

	// defaultSessionTTLHours 是后端只下发「会话 Cookie」（没有 Expires/Max-Age）时，
	// 本地认为会话仍然有效的软上限。可用 OMRES_SESSION_TTL_HOURS 覆盖。
	defaultSessionTTLHours = 8
)

// Session 是落盘到 ~/.omres-cli/session.json 的登录态。
// 字段名与旧版本 sessionData 兼容，旧文件可直接读取。
type Session struct {
	Cookie    string `json:"cookie"`
	Username  string `json:"username,omitempty"`
	Server    string `json:"server,omitempty"`
	SavedAt   string `json:"saved_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	AccountID string `json:"account_id,omitempty"`
}

// sessionDir 返回会话/状态文件目录（~/.omres-cli）。
func sessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".omres-cli")
}

// sessionFile 返回会话文件完整路径。
func sessionFile() string {
	d := sessionDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, sessionFileName)
}

// sessionTTL 返回无显式过期时间时使用的软 TTL。
func sessionTTL() time.Duration {
	if v := os.Getenv(EnvPrefix + "_SESSION_TTL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	return defaultSessionTTLHours * time.Hour
}

// SaveSession 以 0600 权限写入会话文件。
func SaveSession(s *Session) error {
	dir := sessionDir()
	if dir == "" {
		return fmt.Errorf("无法确定用户主目录，会话无法持久化")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建会话目录失败: %w", err)
	}
	if s.SavedAt == "" {
		s.SavedAt = time.Now().Format(time.RFC3339)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, sessionFileName), raw, 0o600); err != nil {
		return fmt.Errorf("写入会话文件失败: %w", err)
	}
	return nil
}

// LoadSession 读取会话文件。文件不存在时返回 (nil, nil)。
func LoadSession() (*Session, error) {
	p := sessionFile()
	if p == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("会话文件已损坏 (%s): %w", p, err)
	}
	if s.Cookie == "" {
		return nil, nil
	}
	return &s, nil
}

// ClearSession 删除会话文件。文件本就不存在时返回 (false, nil)。
func ClearSession() (bool, error) {
	p := sessionFile()
	if p == "" {
		return false, nil
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// LoadSessionCookie 读取仍然有效的会话 Cookie，供其它命令自动携带。
// 无会话或已过期时返回 ("", nil)。
func LoadSessionCookie() (string, error) {
	s, err := LoadSession()
	if err != nil || s == nil {
		return "", err
	}
	if expired, _ := s.Expired(); expired {
		return "", nil
	}
	return s.Cookie, nil
}

// savedAtTime 解析 saved_at；无法解析时返回零值。
func (s *Session) savedAtTime() time.Time {
	t, err := time.Parse(time.RFC3339, s.SavedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// expiresAtTime 解析 expires_at；未设置时返回零值。
func (s *Session) expiresAtTime() time.Time {
	if s.ExpiresAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Expired 判断会话是否失效，并给出原因：
//   - "cookie_expired"：后端下发的 Cookie 到期时间已过
//   - "ttl_exceeded"：无显式到期时间，且落盘时间超过软 TTL
func (s *Session) Expired() (bool, string) {
	if exp := s.expiresAtTime(); !exp.IsZero() {
		if time.Now().After(exp) {
			return true, "cookie_expired"
		}
		return false, ""
	}
	saved := s.savedAtTime()
	if saved.IsZero() {
		return false, ""
	}
	if time.Since(saved) > sessionTTL() {
		return true, "ttl_exceeded"
	}
	return false, ""
}

// AgeSeconds 返回会话已保存的秒数；saved_at 不可解析时返回 -1。
func (s *Session) AgeSeconds() int64 {
	saved := s.savedAtTime()
	if saved.IsZero() {
		return -1
	}
	return int64(time.Since(saved).Seconds())
}

// maskCookie 把 Cookie 值打码，只保留首尾各 3 个字符，用于安全回显。
func maskCookie(cookie string) string {
	if cookie == "" {
		return ""
	}
	var out []string
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			out = append(out, "***")
			continue
		}
		out = append(out, kv[0]+"="+maskValue(kv[1]))
	}
	return strings.Join(out, "; ")
}

func maskValue(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:3] + "******" + v[len(v)-3:]
}
