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

func TestGitImportWorkflowContracts(t *testing.T) {
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
		query  []string
		body   []string
	}{
		{
			action: "pull-branch", method: http.MethodGet, path: "/api/autoGit/getMicroServices",
			query: []string{"repositoryUrl", "branchName", "taskId", "taskName", "resolveConflict"},
		},
		{
			action: "resource-types", method: http.MethodPost, path: "/api/autoGit/autoDisplayResource",
			body: []string{"microService", "taskId", "taskName"},
		},
		{
			action: "is-empty", method: http.MethodPost, path: "/myapi/upload/isEmptyProject",
			body: []string{"taskId"},
		},
		{
			action: "blacklist-check", method: http.MethodPost, path: "/api/autoGit/blacklistJudge",
			body: []string{"gitForm", "taskId"},
		},
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
			queryByName := make(map[string]Parameter, len(params.query))
			for _, p := range params.query {
				queryByName[p.Name] = p
			}
			for _, name := range tt.query {
				if p, ok := queryByName[name]; !ok || !p.Required {
					t.Errorf("required query parameter %q is missing", name)
				}
			}
			if len(tt.body) == 0 {
				return
			}
			if params.body == nil || params.body.Schema == nil {
				t.Fatal("JSON body schema is missing")
			}
			required := make(map[string]bool, len(params.body.Schema.Required))
			for _, name := range params.body.Schema.Required {
				required[name] = true
			}
			for _, name := range tt.body {
				if _, ok := params.body.Schema.Properties[name]; !ok || !required[name] {
					t.Errorf("required body property %q is missing", name)
				}
			}
		})
	}
}

func TestGitImportWorkflowRequests(t *testing.T) {
	gitForm := `{"repositoryUrl":"ssh://git@example.com:2222/5gcore/cp/lib/omres.git","branchName":"refs/heads/personal/user/e2e040827","microService":"ompublic","importTags":"perf","importModuleTreeJson":"[]"}`
	tests := []struct {
		name      string
		method    string
		path      string
		args      []string
		wantQuery url.Values
		wantBody  string
	}{
		{
			name: "pull-branch", method: http.MethodGet, path: "/api/autoGit/getMicroServices",
			args:      []string{"task", "pull-branch", "--repositoryUrl", "ssh://git@example.com:2222/5gcore/cp/lib/omres.git", "--branchName", "refs/heads/personal/user/e2e040827", "--taskId", "48818", "--taskName", "percreate", "--resolveConflict", "0"},
			wantQuery: url.Values{"repositoryUrl": {"ssh://git@example.com:2222/5gcore/cp/lib/omres.git"}, "branchName": {"refs/heads/personal/user/e2e040827"}, "taskId": {"48818"}, "taskName": {"percreate"}, "resolveConflict": {"0"}},
		},
		{
			name: "resource-types", method: http.MethodPost, path: "/api/autoGit/autoDisplayResource",
			args:     []string{"task", "resource-types", "--body", `{"microService":"ompublic","taskId":48818,"taskName":"percreate"}`},
			wantBody: `{"microService":"ompublic","taskId":48818,"taskName":"percreate"}`,
		},
		{
			name: "is-empty", method: http.MethodPost, path: "/myapi/upload/isEmptyProject",
			args:     []string{"task", "is-empty", "--body", `{"taskId":48818}`},
			wantBody: `{"taskId":48818}`,
		},
		{
			name: "blacklist-check", method: http.MethodPost, path: "/api/autoGit/blacklistJudge",
			args:     []string{"task", "blacklist-check", "--body", `{"gitForm":` + gitForm + `,"taskId":48818}`},
			wantBody: `{"gitForm":` + gitForm + `,"taskId":48818}`,
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
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("ReadAll(request body) error = %v", err)
				}
				received <- snapshot{r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("Content-Type"), body}
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
			if got.query.Encode() != tt.wantQuery.Encode() {
				t.Errorf("query = %v, want %v", got.query, tt.wantQuery)
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
