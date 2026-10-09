package main

import (
	"distvis/sdk"
	"encoding/json"
)

func main() {
	n, err := sdk.Open()
	if err != nil {
		panic(err)
	}
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	fields := []sdk.InputField{
		{Name: "text", Label: "文本", Type: "text", Required: true, MaxLength: 100},
		{Name: "count", Label: "数量", Type: "number", Required: true, Integer: true},
		{Name: "enabled", Label: "启用", Type: "boolean", Required: true},
		{Name: "mode", Label: "模式", Type: "select", Options: []string{"first", "second"}, Required: true},
		{Name: "data", Label: "结构化数据", Type: "json", Required: true},
	}
	action := "customInput"
	if n.ID == "node-2" {
		action = "nodeTwoOnly"
		fields = []sdk.InputField{{Name: "note", Label: "节点二备注", Type: "text", Required: true}}
	}
	must(n.DeclareInput([]sdk.InputAction{{Action: action, Label: n.ID + " 应用入口", Fields: fields}}))
	must(n.Report(map[string]any{"role": "ready"}))
	for cmd := range n.Commands {
		var values any
		if err := json.Unmarshal(cmd.Values, &values); err != nil {
			panic(err)
		}
		must(n.Report(map[string]any{"role": "ready", "lastInput": values, "action": cmd.Action, "inputId": cmd.ID}))
	}
}
