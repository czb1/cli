package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseSwagger parses embedded swagger.json bytes.
func ParseSwagger(data []byte) (*Swagger, error) {
	var sw Swagger
	if err := json.Unmarshal(data, &sw); err != nil {
		return nil, fmt.Errorf("解析 swagger 失败: %w", err)
	}
	if sw.Paths == nil {
		return nil, fmt.Errorf("swagger 中没有 paths")
	}
	return &sw, nil
}

// Lookup finds an operation by path and method (method case-insensitive).
func (sw *Swagger) Lookup(path, method string) (*Operation, bool) {
	methods, ok := sw.Paths[path]
	if !ok {
		return nil, false
	}
	op, ok := methods[strings.ToLower(method)]
	if !ok {
		return nil, false
	}
	return &op, true
}

// Resolve dereferences a #/definitions/X $ref, returning the target schema.
func (sw *Swagger) Resolve(s *Schema) *Schema {
	if s == nil {
		return nil
	}
	if s.Ref == "" {
		return s
	}
	const prefix = "#/definitions/"
	if strings.HasPrefix(s.Ref, prefix) {
		name := strings.TrimPrefix(s.Ref, prefix)
		if def, ok := sw.Definitions[name]; ok {
			return def
		}
	}
	return s
}

// classifyParams splits an operation's parameters by location.
type classifiedParams struct {
	path  []Parameter
	query []Parameter
	body  *Parameter
	form  []Parameter
}

func classify(op *Operation) classifiedParams {
	var c classifiedParams
	for i := range op.Parameters {
		p := op.Parameters[i]
		switch p.In {
		case "path":
			c.path = append(c.path, p)
		case "query":
			c.query = append(c.query, p)
		case "body":
			bp := op.Parameters[i]
			c.body = &bp
		case "formData":
			c.form = append(c.form, p)
		}
	}
	return c
}

func (op *Operation) consumesMultipart() bool {
	for _, c := range op.Consumes {
		if strings.Contains(c, "multipart/form-data") {
			return true
		}
	}
	return false
}

func (op *Operation) producesBinary() bool {
	for _, p := range op.Produces {
		if strings.Contains(p, "octet-stream") || strings.Contains(p, "application/zip") {
			return true
		}
	}
	return false
}
