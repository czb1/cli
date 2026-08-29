package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestUDGExportCommandContracts(t *testing.T) {
	cfg, err := LoadConfig(configData)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	sw, err := ParseSwagger(swaggerData)
	if err != nil {
		t.Fatalf("ParseSwagger() error = %v", err)
	}
	if err := ValidateConfig(cfg, sw); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}

	tests := []struct {
		action string
		method string
		path   string
		body   bool
	}{
		{"create-git-commit", http.MethodPost, "/api/udgExport/", true},
		{"git-commit-status", http.MethodGet, "/api/udgExport/queryUdgExportTask/", false},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			res, ok := findResource(cfg, "task", tt.action)
			if !ok {
				t.Fatalf("task %s command is not configured", tt.action)
			}
			if res.HTTP.Method != tt.method || res.HTTP.Path != tt.path {
				t.Fatalf("HTTP contract = %s %s, want %s %s", res.HTTP.Method, res.HTTP.Path, tt.method, tt.path)
			}
			op, ok := sw.Lookup(tt.path, tt.method)
			if !ok {
				t.Fatal("operation is missing from swagger")
			}
			params := classify(op)
			if len(params.query) != 1 || params.query[0].Name != "taskId" || !params.query[0].Required {
				t.Fatalf("required taskId query parameter is missing: %#v", params.query)
			}
			if tt.body && (params.body == nil || params.body.Schema == nil) {
				t.Fatal("JSON body schema is missing")
			}
		})
	}
}

func TestUDGExportRequests(t *testing.T) {
	payload := `{"task_name":"e2emml","branch":"personal/z00942561/mml824","w3_account":"z00942561","ms_list":["DGW_NLS"],"repo_list":["pf","om"],"task_types":["cfg_model","head","doc","enum_head","alpha_xml","excel"],"change_obj_list":[],"ne_type":"UDG","commit_msg":"第一次提交"}`
	tests := []struct {
		name     string
		method   string
		path     string
		args     []string
		wantBody string
	}{
		{
			name: "create-git-commit", method: http.MethodPost, path: "/api/udgExport/",
			args:     []string{"task", "create-git-commit", "--taskId", "48861", "--body", payload},
			wantBody: payload,
		},
		{
			name: "git-commit-status", method: http.MethodGet, path: "/api/udgExport/queryUdgExportTask/",
			args: []string{"task", "git-commit-status", "--taskId", "48861"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type snapshot struct {
				method      string
				path        string
				query       url.Values
				contentType string
				body        []byte
			}
			received := make(chan snapshot, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("ReadAll(request body) error = %v", err)
				}
				received <- snapshot{r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("Content-Type"), data}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":true,"data":[]}`))
			}))
			defer server.Close()

			cfg, err := LoadConfig(configData)
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			cfg.Defaults.Server = server.URL
			cfg.Defaults.Timeout = 2
			cfg.Defaults.Auth = nil
			sw, err := ParseSwagger(swaggerData)
			if err != nil {
				t.Fatalf("ParseSwagger() error = %v", err)
			}

			oldGlobalOpts := g
			defer func() { g = oldGlobalOpts }()
			g = globalOpts{}
			if err := BuildRootCommand(cfg, sw).executeArgs(tt.args); err != nil {
				t.Fatalf("executeArgs() error = %v", err)
			}

			got := <-received
			if got.method != tt.method || got.path != tt.path {
				t.Fatalf("request = %s %s, want %s %s", got.method, got.path, tt.method, tt.path)
			}
			if !reflect.DeepEqual(got.query, url.Values{"taskId": {"48861"}}) {
				t.Errorf("query = %v, want taskId=48861", got.query)
			}
			if tt.wantBody == "" {
				if len(got.body) != 0 {
					t.Errorf("body = %q, want empty", got.body)
				}
				return
			}
			if got.contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got.contentType)
			}
			var gotJSON, wantJSON interface{}
			if err := json.Unmarshal(got.body, &gotJSON); err != nil {
				t.Fatalf("Unmarshal(request body) error = %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &wantJSON); err != nil {
				t.Fatalf("Unmarshal(expected body) error = %v", err)
			}
			if !reflect.DeepEqual(gotJSON, wantJSON) {
				t.Errorf("body = %#v, want %#v", gotJSON, wantJSON)
			}
		})
	}
}
