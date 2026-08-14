package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// loginPath is the API path for login. When a response from this path contains
// Set-Cookie, we persist the cookie so subsequent commands pick it up automatically.
const loginPath = "/api/auth/login"

// httpRequest is a fully-resolved request ready to be executed.
type httpRequest struct {
	Server  string
	Method  string
	Path    string // path params already substituted
	Query   url.Values
	Body    []byte            // JSON/plain body
	Form    map[string]string // multipart text fields
	Files   map[string]string // multipart file fields: field -> file path
	Headers map[string]string // 额外请求头（raw 命令用），在鉴权之前设置
	Auth    *AuthConfig
	Timeout int
	Debug   bool
}

// execute performs the request and returns the response plus its full body.
func (r *httpRequest) execute() (*http.Response, []byte, error) {
	base := strings.TrimRight(r.Server, "/")
	u := base + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}

	var reqBody io.Reader
	contentType := ""

	switch {
	case len(r.Files) > 0 || len(r.Form) > 0:
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range r.Form {
			if err := w.WriteField(k, v); err != nil {
				return nil, nil, err
			}
		}
		for field, path := range r.Files {
			f, err := os.Open(path)
			if err != nil {
				return nil, nil, fmt.Errorf("打开文件 %s 失败: %w", path, err)
			}
			part, err := w.CreateFormFile(field, filepath.Base(path))
			if err != nil {
				f.Close()
				return nil, nil, err
			}
			if _, err := io.Copy(part, f); err != nil {
				f.Close()
				return nil, nil, err
			}
			f.Close()
		}
		w.Close()
		contentType = w.FormDataContentType()
		reqBody = &buf
	case r.Body != nil:
		contentType = "application/json"
		reqBody = bytes.NewReader(r.Body)
	}

	req, err := http.NewRequest(strings.ToUpper(r.Method), u, reqBody)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	// 先设置自定义头，再应用鉴权：鉴权头不允许被随意覆盖。
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	applyAuth(req, r.Auth)

	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", req.Method, u)
		if r.Body != nil {
			fmt.Fprintf(os.Stderr, "[debug] body: %s\n", string(r.Body))
		}
	}

	client := &http.Client{Timeout: time.Duration(r.Timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, data, nil
}

// applyAuth injects credentials per the configured auth type.
func applyAuth(req *http.Request, auth *AuthConfig) {
	if auth == nil {
		return
	}
	switch strings.ToLower(auth.Type) {
	case "bearer":
		if auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		}
	case "api_key":
		if auth.APIKey != "" {
			header := auth.Header
			if header == "" {
				header = "X-API-Key"
			}
			req.Header.Set(header, auth.APIKey)
		}
	case "basic":
		if auth.Username != "" || auth.Password != "" {
			req.SetBasicAuth(auth.Username, auth.Password)
		}
	case "cookie":
		if auth.Cookie != "" {
			req.Header.Set("Cookie", auth.Cookie)
		}
	}
}

// formatResponse converts an HTTP response into a JSON-RPC message and prints it.
// If the request was a login and succeeded, the Set-Cookie header is persisted to
// the session file so subsequent commands are automatically authenticated.
func formatResponse(id string, server string, reqPath string, op *Operation, resp *http.Response, body []byte, err error) {
	if err != nil {
		PrintError(id, CodeInternalError, "Request Failed", err.Error())
		return
	}

	ct := resp.Header.Get("Content-Type")
	cd := resp.Header.Get("Content-Disposition")
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	// Auto-save session cookie on successful login. `auth login` does this itself
	// and additionally records the username, so this only covers the generic path.
	if success && reqPath == loginPath {
		if cookie, expires := extractCookieWithExpiry(resp); cookie != "" {
			sess := &Session{Cookie: cookie, Server: server, SavedAt: time.Now().Format(time.RFC3339)}
			if !expires.IsZero() {
				sess.ExpiresAt = expires.Format(time.RFC3339)
			}
			_ = SaveSession(sess)
		}
	}

	// Binary / file download handling.
	if success && isBinary(ct, cd, op) {
		saveBinary(id, resp, body, ct, cd)
		return
	}

	payload := decodePayload(ct, body)

	if success {
		// HTTP 2xx 不代表业务成功：本后端用 code!=0 或 status=false 表达失败。
		// 不识别的话，删除之类的操作会静默失败而调用方以为成功了。
		if reason, failed := businessFailure(payload); failed {
			PrintError(id, CodeBackendError, reason, payload)
			return
		}
		PrintSuccess(id, payload)
		return
	}
	PrintError(id, CodeBackendError, resp.Status, payload)
}

// extractCookieWithExpiry builds a Cookie header value from the Set-Cookie
// response headers, together with the earliest expiry declared by the backend.
// A zero time means only session cookies were issued, in which case the local
// soft TTL applies.
func extractCookieWithExpiry(resp *http.Response) (string, time.Time) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return "", time.Time{}
	}
	var parts []string
	var earliest time.Time
	now := time.Now()
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)

		var exp time.Time
		switch {
		case c.MaxAge > 0:
			exp = now.Add(time.Duration(c.MaxAge) * time.Second)
		case !c.Expires.IsZero():
			exp = c.Expires
		}
		if !exp.IsZero() && (earliest.IsZero() || exp.Before(earliest)) {
			earliest = exp
		}
	}
	return strings.Join(parts, "; "), earliest
}

// decodePayload returns a native JSON object when Content-Type is JSON,
// otherwise the raw string. Never returns stringified JSON.
func decodePayload(contentType string, body []byte) interface{} {
	if isJSON(contentType) {
		var v interface{}
		if err := json.Unmarshal(body, &v); err == nil {
			return v
		}
	}
	// Fall back to string; try JSON anyway in case Content-Type was missing.
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		var v interface{}
		if err := json.Unmarshal(trimmed, &v); err == nil {
			return v
		}
	}
	return string(body)
}

func isJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

func isBinary(contentType, contentDisposition string, op *Operation) bool {
	if strings.Contains(strings.ToLower(contentDisposition), "attachment") {
		return true
	}
	ctl := strings.ToLower(contentType)
	if strings.Contains(ctl, "octet-stream") || strings.Contains(ctl, "application/zip") ||
		strings.Contains(ctl, "application/lua") || strings.Contains(ctl, "application/x-") {
		return true
	}
	// Empty content-type on an operation declared to produce binary.
	if ctl == "" && op != nil && op.producesBinary() {
		return true
	}
	return false
}

// saveBinary writes the response body to a temp file and emits file info.
func saveBinary(id string, resp *http.Response, body []byte, contentType, contentDisposition string) {
	name := filenameFromDisposition(contentDisposition)
	if name == "" {
		name = "download-" + strings.TrimPrefix(id, "req-")
	}
	tmp := filepath.Join(os.TempDir(), name)
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		PrintError(id, CodeFileSaveError, "File Save Failed", err.Error())
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	PrintSuccess(id, map[string]interface{}{
		"file":         tmp,
		"content_type": contentType,
		"size":         len(body),
		"base64_head":  base64Head(body),
	})
}

func base64Head(body []byte) string {
	n := len(body)
	if n > 48 {
		n = 48
	}
	return base64.StdEncoding.EncodeToString(body[:n])
}

func filenameFromDisposition(cd string) string {
	for _, part := range strings.Split(cd, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "filename=") {
			name := strings.Trim(strings.TrimPrefix(part, "filename="), `"`)
			return filepath.Base(name)
		}
	}
	return ""
}

// businessFailure 识别 HTTP 2xx 响应中的业务失败。
//
// 只认接口契约里明确定义的两种表达，避免误判：
//   - code 字段存在且非 0（swagger 中统一注明「0=成功」）
//   - status 字段为布尔 false（如 /api/task/deleteOne）
// 其余形态一律视为成功，保持既有行为不变。
func businessFailure(payload interface{}) (string, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return "", false
	}
	if raw, exists := m["status"]; exists {
		if b, isBool := raw.(bool); isBool && !b {
			return "Operation Failed", true
		}
	}
	if raw, exists := m["code"]; exists {
		switch v := raw.(type) {
		case float64:
			if v != 0 {
				return "Business Error", true
			}
		case string:
			if n, err := strconv.ParseFloat(v, 64); err == nil && n != 0 {
				return "Business Error", true
			}
		}
	}
	return "", false
}
