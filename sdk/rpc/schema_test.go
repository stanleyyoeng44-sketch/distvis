package rpc

import (
	"reflect"
	"testing"
)

func TestDestinationFieldTags(t *testing.T) {
	type args struct {
		To      []string `json:"to" distvis:"nodes"`
		Peer    string   `json:"peer" distvis:"node"`
		Count   int      `json:"count" distvis:"node"`     // wrong kind: stays a number
		Targets string   `json:"targets" distvis:"nodes"`  // wrong kind: stays text
		Tags    []string `json:"tags"`                     // untagged list: stays JSON
		Note    string   `json:"note"`
	}
	got := map[string]string{}
	for _, f := range goFields(reflect.TypeOf(args{})) {
		got[f.Name] = f.Type
	}
	want := map[string]string{"to": "nodes", "peer": "node", "count": "number", "targets": "text", "tags": "json", "note": "text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
