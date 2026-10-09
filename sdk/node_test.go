package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestTransportAndState(t *testing.T) {
	input := strings.NewReader("{\"type\":\"init\",\"node\":\"node-1\",\"nodes\":[\"node-1\",\"node-2\"],\"protocol\":\"raft\"}\n" +
		"{\"type\":\"message\",\"id\":\"m1\",\"from\":\"node-2\",\"to\":\"node-1\",\"payload\":{\"type\":\"Vote\"}}\n")
	var output bytes.Buffer
	node, err := OpenStreams(input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if node.ID != "node-1" || node.Protocol != "raft" {
		t.Fatal("invalid init")
	}
	msg, err := node.Receive(context.Background())
	if err != nil || msg.ID != "m1" || msg.From != "node-2" {
		t.Fatalf("receive: %+v %v", msg, err)
	}
	if err := node.Send("node-2", map[string]string{"type": "Hello"}); err != nil {
		t.Fatal(err)
	}
	if err := node.Report(map[string]string{"role": "leader"}); err != nil {
		t.Fatal(err)
	}
	if err := node.Send("unknown", nil); err == nil {
		t.Fatal("invalid peer accepted")
	}
	dec := json.NewDecoder(&output)
	for _, kind := range []string{"received", "send", "state"} {
		var record map[string]any
		if err := dec.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["type"] != kind {
			t.Fatalf("wanted %s: %v", kind, record)
		}
	}
	_, err = node.Receive(context.Background())
	if err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestRejectMissingOrMalformedInitialization(t *testing.T) {
	for _, input := range []string{"", "invalid\n", "{\"type\":\"message\"}\n"} {
		if _, err := OpenStreams(strings.NewReader(input), io.Discard); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestPersistentStateSurvivesReload(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Save(map[string]any{"term": 4, "votedFor": "node-2"}); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Term     int
		VotedFor string
	}
	if err := Load(&state); err != nil {
		t.Fatal(err)
	}
	if state.Term != 4 || state.VotedFor != "node-2" {
		t.Fatalf("%+v", state)
	}
	if _, err := os.Stat("state.json.tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary state was not renamed")
	}
}

func TestStateDirectoryCanBeSeparateFromReadOnlyProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DISTVIS_STATE_DIR", dir)
	if err := Save(map[string]int{"term": 9}); err != nil {
		t.Fatal(err)
	}
	var state map[string]int
	if err := Load(&state); err != nil {
		t.Fatal(err)
	}
	if state["term"] != 9 {
		t.Fatalf("%+v", state)
	}
	if _, err := os.Stat(dir + "/state.json"); err != nil {
		t.Fatal(err)
	}
}

func TestDeclaredApplicationInputsAndTypedCommands(t *testing.T) {
	input := strings.NewReader("{\"type\":\"init\",\"node\":\"node-1\",\"nodes\":[\"node-1\",\"node-2\"]}\n" +
		"{\"type\":\"command\",\"id\":\"input-7\",\"action\":\"apply\",\"values\":{\"number\":0,\"enabled\":false,\"payload\":{\"items\":[1,null]}}}\n")
	var output bytes.Buffer
	node, err := OpenStreams(input, &output)
	if err != nil {
		t.Fatal(err)
	}
	schema := []InputAction{{Action: "apply", Label: "Apply", Fields: []InputField{{Name: "number", Label: "Number", Type: "number"}}}}
	if err := node.DeclareInput(schema); err != nil {
		t.Fatal(err)
	}
	cmd := <-node.Commands
	if cmd.ID != "input-7" || cmd.Action != "apply" {
		t.Fatalf("%+v", cmd)
	}
	var values map[string]any
	if err := json.Unmarshal(cmd.Values, &values); err != nil {
		t.Fatal(err)
	}
	if values["number"] != float64(0) || values["enabled"] != false {
		t.Fatalf("%+v", values)
	}
	var record struct {
		Type   string
		Schema []InputAction
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Type != "input_schema" || record.Schema[0].Action != "apply" {
		t.Fatalf("%+v", record)
	}
}
