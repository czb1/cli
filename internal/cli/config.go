package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvPrefix is used to build override env var names, e.g. OMRES_SERVER.
const EnvPrefix = "OMRES"

// DefaultTimeout 是所有接口默认的请求超时秒数。
// 服务端部分接口（导入、编译、批量校验等）耗时较长，默认给足 20 分钟，
// 需要更短可用 --timeout 或 OMRES_TIMEOUT 覆盖。
const DefaultTimeout = 1200

// LoadConfig parses the embedded config bytes.
func LoadConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if cfg.Defaults.Timeout == 0 {
		cfg.Defaults.Timeout = DefaultTimeout
	}
	if cfg.Defaults.Output == "" {
		cfg.Defaults.Output = "json"
	}
	applyEnvOverrides(&cfg)
	return &cfg, nil
}

// applyEnvOverrides lets environment variables override config values.
// Env vars take precedence over the config file (safe for CI/CD).
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv(EnvPrefix + "_SERVER"); v != "" {
		cfg.Defaults.Server = v
	}
	if v := os.Getenv(EnvPrefix + "_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Defaults.Timeout = n
		}
	}

	// Auth overrides; instantiate Auth lazily if any auth env var is set.
	auth := cfg.Defaults.Auth
	ensure := func() *AuthConfig {
		if auth == nil {
			auth = &AuthConfig{}
			cfg.Defaults.Auth = auth
		}
		return auth
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_TOKEN"); v != "" {
		ensure().Token = v
		if auth.Type == "" {
			auth.Type = "bearer"
		}
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_API_KEY"); v != "" {
		ensure().APIKey = v
		if auth.Type == "" {
			auth.Type = "api_key"
		}
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_USERNAME"); v != "" {
		ensure().Username = v
		if auth.Type == "" {
			auth.Type = "basic"
		}
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_PASSWORD"); v != "" {
		ensure().Password = v
	}
	if v := os.Getenv(EnvPrefix + "_AUTH_COOKIE"); v != "" {
		ensure().Cookie = v
		if auth.Type == "" {
			auth.Type = "cookie"
		}
	}
	// 未显式指定 Cookie 时，会话文件由 mergeAuth 在构建请求时加载，
	// 保证「命令行 > 环境变量 > 配置 > 会话文件」这一条优先级链只有一处实现。
}

// ValidateConfig ensures every configured path+method exists in swagger.
func ValidateConfig(cfg *Config, sw *Swagger) error {
	var missing []string
	seen := map[string]bool{}
	for gname, g := range cfg.Groups {
		for _, r := range g.Resources {
			key := gname + " " + r.CLI.Action
			if seen[key] {
				return fmt.Errorf("重复的命令定义: %s", key)
			}
			seen[key] = true
			if _, ok := sw.Lookup(r.HTTP.Path, r.HTTP.Method); !ok {
				missing = append(missing, fmt.Sprintf("%s %s (%s %s)",
					gname, r.CLI.Action, r.HTTP.Method, r.HTTP.Path))
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("以下配置的 path+method 在 swagger.json 中不存在:\n  - %s",
			strings.Join(missing, "\n  - "))
	}
	return nil
}
