package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMMLEffectPresets(t *testing.T) {
	if len(mmlEffects) != 10 {
		t.Fatal("expected all ten presets")
	}
	for label, pair := range mmlEffects {
		t.Run(label, func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{"taskId": json.Number("9007199254740993"), "mmlCommandTable": map[string]string{"effectCh": label, "effectEn": "old", "definitiontext": "old", "definitionEn": "old", "definitionService": "old", "extra": "keep"}})
			got, err := normalizeMMLEffect(body)
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]json.RawMessage
			json.Unmarshal(got, &envelope)
			if string(envelope["taskId"]) != "9007199254740993" {
				t.Fatal("ID changed")
			}
			var table map[string]string
			json.Unmarshal(envelope["mmlCommandTable"], &table)
			if table["effectCh"] != pair[0] || table["effectEn"] != pair[1] || table["extra"] != "keep" {
				t.Fatalf("unexpected table: %v", table)
			}
			for _, k := range []string{"definitiontext", "definitionEn", "definitionService"} {
				if v, ok := table[k]; !ok || v != "" {
					t.Fatalf("%s not cleared", k)
				}
			}
		})
	}
}

func TestMMLEffectCustomAndCompatibility(t *testing.T) {
	body := []byte(`{"mmlCommandTable":{"effectCh":"自定义","definitiontext":"测试","definitionEn":"test","definitionService":"TEST"}}`)
	got, err := normalizeMMLEffect(body)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Table map[string]string `json:"mmlCommandTable"`
	}
	json.Unmarshal(got, &env)
	if env.Table["effectCh"] != "测试" || env.Table["definitiontext"] != "测试" || env.Table["definitionEn"] != "test" || env.Table["definitionService"] != "TEST" {
		t.Fatal(string(got))
	}
	if _, ok := env.Table["effectEn"]; ok {
		t.Fatal("custom effectEn invented")
	}
	for _, key := range []string{"definitiontext", "definitionEn", "definitionService"} {
		var envelope map[string]map[string]string
		json.Unmarshal(body, &envelope)
		delete(envelope["mmlCommandTable"], key)
		invalid, _ := json.Marshal(envelope)
		if _, err := normalizeMMLEffect(invalid); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
	for _, input := range []string{`{}`, `null`, `[]`, `{"mmlCommandTable":{"effectCh":"完整描述","effectEn":"original"}}`, `{"mmlCommandTable":{"effectCh":"未知短语"}}`} {
		got, err := normalizeMMLEffect([]byte(input))
		if err != nil || string(got) != input {
			t.Fatalf("legacy body changed: %s", got)
		}
	}
}

func TestMMLEffectCLIRequest(t *testing.T) {
	for _, mode := range []string{"body", "body-file"} {
		t.Run(mode, func(t *testing.T) {
			received := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- struct{}{}
				if r.Method != "POST" || r.URL.Path != "/api/mmlCommand/insertOrUpdate" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				data, _ := io.ReadAll(r.Body)
				var env struct {
					Table map[string]string `json:"mmlCommandTable"`
				}
				if err := json.Unmarshal(data, &env); err != nil {
					t.Error(err)
				}
				if env.Table["effectCh"] != "该命令执行后立即生效。" || env.Table["effectEn"] != "This command takes effect immediately after being executed." {
					t.Errorf("unexpected body: %s", data)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"code":0}`))
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
			cfg.Defaults.Auth = &AuthConfig{Type: "none"}
			old := g
			defer func() { g = old }()
			g = globalOpts{}
			value := `{"taskId":1,"mocTypeId":1,"mmlCommandTable":{"mmlCommandName":"SET TEST","commandType":"update","effectCh":"立即生效"}}`
			if mode == "body-file" {
				path := filepath.Join(t.TempDir(), "body.json")
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
				value = path
			}
			if err := BuildRootCommand(cfg, sw).executeArgs([]string{"mml-command", "upsert", "--" + mode, value}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-received:
			default:
				t.Fatal("no request received")
			}
		})
	}
}
