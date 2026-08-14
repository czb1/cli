package cli

// ---- Config (api_cli_config.json) ----

type Config struct {
	Version  string                 `json:"version"`
	Defaults Defaults               `json:"defaults"`
	Groups   map[string]GroupConfig `json:"groups"`
}

type Defaults struct {
	Server  string      `json:"server"`
	Output  string      `json:"output"`
	Timeout int         `json:"timeout"`
	Auth    *AuthConfig `json:"auth,omitempty"`
}

type AuthConfig struct {
	Type     string `json:"type"`               // bearer | api_key | basic | cookie | none
	Token    string `json:"token,omitempty"`    // bearer
	APIKey   string `json:"api_key,omitempty"`  // api_key
	Header   string `json:"header,omitempty"`   // api_key header name (default X-API-Key)
	Username string `json:"username,omitempty"` // basic
	Password string `json:"password,omitempty"` // basic
	Cookie   string `json:"cookie,omitempty"`   // cookie

	// 以下两项供 `auth status --online` 使用：一个只读、无副作用的探活接口。
	ProbePath string `json:"probe_path,omitempty"`
	ProbeBody string `json:"probe_body,omitempty"`
}

type GroupConfig struct {
	Description string     `json:"description"`
	Resources   []Resource `json:"resources"`
}

type Resource struct {
	HTTP HTTPSpec `json:"http"`
	CLI  CLISpec  `json:"cli"`
}

type HTTPSpec struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

type CLISpec struct {
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
}

// ---- Swagger 2.0 (subset) ----

type Swagger struct {
	BasePath    string                          `json:"basePath"`
	Host        string                          `json:"host"`
	Paths       map[string]map[string]Operation `json:"paths"`
	Definitions map[string]*Schema              `json:"definitions"`
}

type Operation struct {
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Consumes    []string            `json:"consumes"`
	Produces    []string            `json:"produces"`
	Parameters  []Parameter         `json:"parameters"`
	Responses   map[string]Response `json:"responses"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"` // path | query | body | formData | header
	Required    bool    `json:"required"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Schema      *Schema `json:"schema,omitempty"`
}

type Response struct {
	Description string  `json:"description"`
	Schema      *Schema `json:"schema,omitempty"`
}

type Schema struct {
	Type        string             `json:"type,omitempty"`
	Ref         string             `json:"$ref,omitempty"`
	Format      string             `json:"format,omitempty"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Required    []string           `json:"required,omitempty"`
}
