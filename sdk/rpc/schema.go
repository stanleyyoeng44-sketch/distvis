package rpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	"distvis/sdk"
)

var fieldName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func actionID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "rpc_" + hex.EncodeToString(sum[:16])
}
func number(v float64) *float64 { return &v }
func baseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// JSON is only the browser representation. net/rpc peer traffic still uses gob.
func goFields(t reflect.Type) []sdk.InputField {
	t = baseType(t)
	if t.Kind() != reflect.Struct || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return []sdk.InputField{{Name: "request", Label: "Request", Type: "json", Required: true}}
	}
	fields := []sdk.InputField{}
	names := map[string]bool{}
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if f.Anonymous { // Embedded fields and custom JSON representations use one honest JSON editor.
			return []sdk.InputField{{Name: "request", Label: "Request", Type: "json", Required: true}}
		}
		if name == "" {
			name = f.Name
		}
		if !fieldName.MatchString(name) || names[name] || name == "constructor" || name == "prototype" {
			return []sdk.InputField{{Name: "request", Label: "Request", Type: "json", Required: true}}
		}
		names[name] = true
		input := sdk.InputField{Name: name, Label: name, Type: "json"}
		ft := baseType(f.Type)
		switch ft.Kind() {
		case reflect.String:
			input.Type = "text"
		case reflect.Bool:
			if f.Type.Kind() != reflect.Pointer {
				input.Type = "boolean"
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			input.Type = "number"
			input.Integer = true
			bits := ft.Bits()
			if bits > 53 {
				bits = 54
			}
			input.Min = number(-float64(int64(1)<<(bits-1)) + boolOffset(bits == 54))
			input.Max = number(float64(int64(1)<<(bits-1)) - 1)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			input.Type = "number"
			input.Integer = true
			bits := ft.Bits()
			if bits > 53 {
				bits = 53
			}
			input.Min = number(0)
			input.Max = number(float64(uint64(1)<<bits) - 1)
		case reflect.Float32, reflect.Float64:
			input.Type = "number"
		}
		// `distvis:"node"` on a string, or `distvis:"nodes"` on a []string, marks a destination:
		// the browser offers the run's other nodes as a dropdown instead of a free-text field.
		switch f.Tag.Get("distvis") {
		case "node":
			if f.Type.Kind() == reflect.String {
				input.Type = "node"
			}
		case "nodes":
			if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.String {
				input.Type = "nodes"
			}
		}
		fields = append(fields, input)
	}
	if len(fields) > 16 {
		return []sdk.InputField{{Name: "request", Label: "Request", Type: "json", Required: true}}
	}
	return fields
}
func boolOffset(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func unwrap(values json.RawMessage, fields []sdk.InputField, t reflect.Type) json.RawMessage {
	if len(fields) == 1 && fields[0].Name == "request" && fields[0].Required {
		var wrapped map[string]json.RawMessage
		if json.Unmarshal(values, &wrapped) == nil {
			return wrapped["request"]
		}
	}
	return values
}
