package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

var mmlEffects = map[string][2]string{
	"立即生效":                  {"该命令执行后立即生效。", "This command takes effect immediately after being executed."},
	"重启系统生效":                {"该命令执行后需要重新启动系统才能生效。", "This command takes effect after a system restart."},
	"新激活用户生效":               {"该命令执行后只对新激活用户生效。", "This command takes effect only for subscribers who are activated after this command is executed."},
	"执行SET REFRESHSRV命令后生效": {"该命令执行后需要等待执行SET REFRESHSRV命令刷新后生效。", "This command takes effect after SET REFRESHSRV is run."},
	"关闭跟踪任务，创建新跟踪任务生效":      {"该命令执行后需要关闭已经创建了数据面跟踪的所有跟踪任务，重新创建新的跟踪任务才能生效。", "This command takes effect only after the existing data-plane tracing tasks are stopped and new tracing tasks are created."},
	"最后一次执行命令90秒后生效":        {"为了防止频繁刷新规则对系统造成性能影响，识别规则添加、修改、删除后，不会立即生效，当最后一次执行本命令90秒后所有规则才生效。", "Frequent updates to dynamic protocol identification rules will affect the performance. To prevent this problem, the addition, modification, and deletion of dynamic protocol identification rules will take effect 90 seconds after the last time this command is executed."},
	"60s生效":                 {"该命令执行后60s生效。", "This command takes effect 60 seconds after being executed."},
	"30s生效":                 {"该命令执行后30s生效。", "This command takes effect 30 seconds after being executed."},
	"发生承载更新的用户或者新激活用户生效": {"该命令执行后只对之后发生承载更新的用户或者新激活用户生效。", "The setting takes effect only for subscribers whose bearers are updated or who are activated after the command is executed."},
	"新数据流生效": {"该命令执行后对新数据流生效。", "This command takes effect only for flows that are generated after this command is executed."},
}

// normalizeMMLEffect expands shorthand in the documented command object only.
// RawMessage preserves unrelated values, including large integer IDs.
func normalizeMMLEffect(body []byte) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return body, nil
	}
	var table map[string]json.RawMessage
	if json.Unmarshal(envelope["mmlCommandTable"], &table) != nil {
		return body, nil
	}
	var label string
	if json.Unmarshal(table["effectCh"], &label) != nil {
		return body, nil
	}
	label = strings.TrimSpace(label)
	set := func(key, value string) { table[key], _ = json.Marshal(value) }
	if pair, ok := mmlEffects[label]; ok {
		set("effectCh", pair[0])
		set("effectEn", pair[1])
		for _, key := range []string{"definitiontext", "definitionEn", "definitionService"} {
			set(key, "")
		}
	} else if label == "自定义" {
		var definition string
		for _, key := range []string{"definitiontext", "definitionEn", "definitionService"} {
			var value string
			if json.Unmarshal(table[key], &value) != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("自定义生效方式需要填写 mmlCommandTable.%s", key)
			}
			if key == "definitiontext" {
				definition = value
			}
		}
		set("effectCh", definition)
	} else {
		// Keep existing callers that supply full descriptions unchanged.
		return body, nil
	}
	encoded, err := json.Marshal(table)
	if err != nil {
		return nil, err
	}
	envelope["mmlCommandTable"] = encoded
	return json.Marshal(envelope)
}
