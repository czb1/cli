package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestImportGitCommandContract(t *testing.T) {
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

	res, ok := findResource(cfg, "task", "import-git")
	if !ok {
		t.Fatal("task import-git command is not configured")
	}
	if res.HTTP.Method != http.MethodPost || res.HTTP.Path != "/api/autoGit/importInfoFormGit" {
		t.Fatalf("unexpected HTTP contract: %s %s", res.HTTP.Method, res.HTTP.Path)
	}

	op, ok := sw.Lookup(res.HTTP.Path, res.HTTP.Method)
	if !ok {
		t.Fatal("import-git operation is missing from swagger")
	}
	params := classify(op)
	if params.body == nil || params.body.Schema == nil {
		t.Fatal("import-git must accept a JSON body")
	}
	gitForm := sw.Resolve(params.body.Schema.Properties["gitForm"])
	if gitForm == nil || gitForm.Type != "object" {
		t.Fatal("gitForm must be described as an object")
	}
	for _, name := range []string{"repositoryUrl", "branchName", "microService", "importTags", "importModuleTreeJson"} {
		if _, ok := gitForm.Properties[name]; !ok {
			t.Errorf("gitForm property %q is missing", name)
		}
	}
}

func TestImportGitCommandSendsExpectedRequest(t *testing.T) {
	type requestSnapshot struct {
		method      string
		path        string
		contentType string
		body        map[string]interface{}
	}
	received := make(chan requestSnapshot, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll(request body) error = %v", err)
		}
		var body map[string]interface{}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("Unmarshal(request body) error = %v", err)
		}
		received <- requestSnapshot{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"导入成功","status":true}`))
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

	payload := `{"gitForm":{"repositoryUrl":"ssh://git@example.com:2222/5gcore/cp/lib/omres.git","branchName":"refs/heads/personal/user/fanyitree","microService":"ompublic","importTags":"perf","importModuleTreeJson":"[]"},"taskId":48569,"taskName":"perfjian"}`
	root := BuildRootCommand(cfg, sw)
	if err := root.executeArgs([]string{"task", "import-git", "--body", payload}); err != nil {
		t.Fatalf("executeArgs() error = %v", err)
	}

	got := <-received
	if got.method != http.MethodPost || got.path != "/api/autoGit/importInfoFormGit" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if got.contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got.contentType)
	}
	var want map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &want); err != nil {
		t.Fatalf("Unmarshal(expected payload) error = %v", err)
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("request body = %#v, want %#v", got.body, want)
	}
}
