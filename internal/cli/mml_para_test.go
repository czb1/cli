package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMMLParaUpsertContract(t *testing.T) {
	cfg, err := LoadConfig(configData)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := ParseSwagger(swaggerData)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(cfg, sw); err != nil {
		t.Fatal(err)
	}
	res, ok := findResource(cfg, "mml-para", "upsert")
	if !ok || res.HTTP.Method != "POST" || res.HTTP.Path != "/api/mmlPara/insertOrUpdate" {
		t.Fatalf("unexpected MML parameter mapping: %+v", res)
	}
	op, ok := sw.Lookup(res.HTTP.Path, res.HTTP.Method)
	if !ok {
		t.Fatal("missing MML parameter operation")
	}
	p := classify(op)
	if p.body == nil || p.body.Schema == nil {
		t.Fatal("missing body schema")
	}
	table := p.body.Schema.Properties["mmlParaTable"]
	if table == nil || table.Properties["source"] == nil || table.Properties["source"].Type != "string" {
		t.Fatal("missing mmlParaTable.source string schema")
	}
	response := op.Responses["200"].Schema
	if response == nil || response.Properties["status"] == nil || response.Properties["status"].Type != "boolean" {
		t.Fatal("missing status response schema")
	}
	command, ok := findResource(cfg, "command-para", "upsert")
	if !ok || command.HTTP.Path != "/api/commandPara/insertOrUpdate" {
		t.Fatal("command-para upsert must keep its separate endpoint")
	}
}

func TestMMLParaUpsertRequest(t *testing.T) {
	const payload = `{"taskId":49589,"mmlParaTable":{"id":10,"paraName":"SUBAPPIDNAME","source":"本端规划","sourceOptions":[{"value":"本端规划","label":"本端规划"}],"isGenHelp":null,"ranges":[{"min":"0","max":"63"}],"extendEnumRanges":[],"paraDescChSel":false,"extraField":{"keep":true}}}`
	for _, mode := range []string{"body", "body-file"} {
		t.Run(mode, func(t *testing.T) {
			received := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/mmlPara/insertOrUpdate" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected content type: %s", r.Header.Get("Content-Type"))
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				received <- string(data)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":null,"message":"","status":true}`))
			}))
			defer server.Close()
			cfg, err := LoadConfig(configData)
			if err != nil {
				t.Fatal(err)
			}
			sw, err := ParseSwagger(swaggerData)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Defaults.Server = server.URL
			cfg.Defaults.Timeout = 2
			cfg.Defaults.Auth = &AuthConfig{Type: "none"}
			old := g
			defer func() { g = old }()
			g = globalOpts{}
			value := payload
			if mode == "body-file" {
				value = filepath.Join(t.TempDir(), "mml-para.json")
				if err := os.WriteFile(value, []byte(payload), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := BuildRootCommand(cfg, sw).executeArgs([]string{"mml-para", "upsert", "--" + mode, value}); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-received:
				if got != payload {
					t.Errorf("body changed: got %s, want %s", got, payload)
				}
			default:
				t.Fatal("no request received")
			}
		})
	}
}
