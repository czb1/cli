package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

)

// buildDescribeCommand implements the global `describe <group> <action>` command.
// It never makes a network request; it emits pure local metadata from swagger.
func buildDescribeCommand(cfg *Config, sw *Swagger) *Command {
	return &Command{
		Use:   "describe <group> <action>",
		Short: "输出命令的完整语义契约（参数与输出 schema），供 AI 解析",
		Args:  ExactArgs(2),
		RunE: func(cmd *Command, args []string) error {
			group, action := args[0], args[1]
			res, ok := findResource(cfg, group, action)
			if !ok {
				return fmt.Errorf("未找到命令: %s %s", group, action)
			}
			op, ok := sw.Lookup(res.HTTP.Path, res.HTTP.Method)
			if !ok {
				return fmt.Errorf("swagger 中缺少 %s %s", res.HTTP.Method, res.HTTP.Path)
			}
			out := buildDescribe(sw, group, res, op)
			enc := json.NewEncoder(os.Stdout)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
}

func findResource(cfg *Config, group, action string) (Resource, bool) {
	gcfg, ok := cfg.Groups[group]
	if !ok {
		return Resource{}, false
	}
	for _, r := range gcfg.Resources {
		if r.CLI.Action == action {
			return r, true
		}
	}
	return Resource{}, false
}

type describeParam struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	In          string      `json:"in"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Schema      interface{} `json:"schema,omitempty"`
}

func buildDescribe(sw *Swagger, group string, res Resource, op *Operation) map[string]interface{} {
	desc := res.CLI.Description
	if desc == "" {
		desc = op.Summary
	}

	params := []describeParam{}
	for _, p := range op.Parameters {
		if p.In == "body" {
			// Expand body object into individual fields for clarity.
			schema := sw.Resolve(p.Schema)
			if schema != nil && len(schema.Properties) > 0 {
				req := toSet(schema.Required)
				for fname, fs := range schema.Properties {
					rs := sw.Resolve(fs)
					dp := describeParam{
						Name:        fname,
						Type:        schemaType(rs),
						In:          "body",
						Required:    req[fname],
						Description: firstNonEmpty(fs.Description, rs.Description),
					}
					// Only expand nested schema for object/array fields.
					if rs != nil && (rs.Type == "object" || rs.Type == "array") {
						dp.Schema = schemaSummary(sw, rs)
					}
					params = append(params, dp)
				}
				continue
			}
		}
		params = append(params, describeParam{
			Name:        p.Name,
			Type:        p.Type,
			In:          p.In,
			Required:    p.Required,
			Description: p.Description,
		})
	}

	out := map[string]interface{}{
		"command":     group + " " + res.CLI.Action,
		"description": desc,
		"http": map[string]string{
			"method": strings.ToUpper(res.HTTP.Method),
			"path":   res.HTTP.Path,
		},
		"parameters": params,
	}
	if res.CLI.Example != "" {
		out["example"] = res.CLI.Example
	}
	if outputSchema := successOutput(sw, op); outputSchema != nil {
		out["output"] = map[string]interface{}{"success": outputSchema}
	}
	return out
}

func successOutput(sw *Swagger, op *Operation) interface{} {
	resp, ok := op.Responses["200"]
	if !ok || resp.Schema == nil {
		return nil
	}
	return schemaSummary(sw, sw.Resolve(resp.Schema))
}

// schemaSummary produces a compact JSON-schema-like description.
func schemaSummary(sw *Swagger, s *Schema) interface{} {
	if s == nil {
		return nil
	}
	s = sw.Resolve(s)
	m := map[string]interface{}{}
	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		props := map[string]interface{}{}
		for k, v := range s.Properties {
			props[k] = schemaSummary(sw, v)
		}
		m["properties"] = props
	}
	if s.Items != nil {
		m["items"] = schemaSummary(sw, s.Items)
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	return m
}

func schemaType(s *Schema) string {
	if s == nil {
		return ""
	}
	if s.Type != "" {
		return s.Type
	}
	if s.Ref != "" || len(s.Properties) > 0 {
		return "object"
	}
	return ""
}

func toSet(list []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range list {
		m[x] = true
	}
	return m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- shared small helpers ----

func readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件 %s 失败: %w", path, err)
	}
	return data, nil
}

func validJSON(b []byte) bool {
	return json.Valid(b)
}
