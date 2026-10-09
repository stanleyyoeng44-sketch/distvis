// Package sdk provides the instrumented transport used by DistVis.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type Message struct {
	ID      string          `json:"id"`
	From    string          `json:"from"`
	To      string          `json:"to"`
	Payload json.RawMessage `json:"payload"`
}

type Command struct {
	ID     string          `json:"id"`
	Action string          `json:"action,omitempty"`
	Key    string          `json:"key,omitempty"`
	Value  string          `json:"value,omitempty"`
	Values json.RawMessage `json:"values,omitempty"`
}

type InputField struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Type      string   `json:"type"` // text, number, boolean, select, json, node (one node ID), nodes (list of node IDs)
	Required  bool     `json:"required,omitempty"`
	Integer   bool     `json:"integer,omitempty"`
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Options   []string `json:"options,omitempty"`
}
type InputAction struct {
	Action      string       `json:"action"`
	Label       string       `json:"label"`
	Description string       `json:"description,omitempty"`
	Fields      []InputField `json:"fields"`
}

// DeclareInput publishes the application commands this node accepts. The
// coordinator uses this schema to render a form; protocol code owns its fields.
func (n *Node) DeclareInput(schema []InputAction) error {
	return n.emit(map[string]any{"type": "input_schema", "schema": schema})
}

// CommandResult records an application RPC's return value or failure.
func (n *Node) CommandResult(id string, result any, err error) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return n.emit(map[string]any{"type": "command_result", "commandId": id, "result": result, "error": message})
}

type Node struct {
	ID       string
	Nodes    []string
	Protocol string
	Commands chan Command
	inbox    chan Message
	done     chan struct{}
	out      *json.Encoder
	mu       sync.Mutex
}

// Open waits for the coordinator's initialization record. stdout is reserved
// for the protocol; ordinary diagnostics should go to stderr.
func Open() (*Node, error) { return OpenStreams(os.Stdin, os.Stdout) }

func OpenStreams(input io.Reader, output io.Writer) (*Node, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() {
		return nil, fmt.Errorf("missing initialization: %v", scanner.Err())
	}
	var init struct {
		Type     string   `json:"type"`
		Node     string   `json:"node"`
		Nodes    []string `json:"nodes"`
		Protocol string   `json:"protocol"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &init); err != nil {
		return nil, err
	}
	if init.Type != "init" || init.Node == "" {
		return nil, fmt.Errorf("invalid initialization")
	}
	n := &Node{ID: init.Node, Nodes: init.Nodes, Protocol: init.Protocol,
		Commands: make(chan Command, 64), inbox: make(chan Message, 256),
		done: make(chan struct{}), out: json.NewEncoder(output)}
	go func() {
		defer close(n.done)
		defer close(n.inbox)
		defer close(n.Commands)
		for scanner.Scan() {
			var record struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			switch record.Type {
			case "message":
				var msg Message
				if json.Unmarshal(scanner.Bytes(), &msg) == nil {
					n.inbox <- msg
				}
			case "command":
				var cmd Command
				if json.Unmarshal(scanner.Bytes(), &cmd) == nil {
					n.Commands <- cmd
				}
			}
		}
	}()
	return n, nil
}

func (n *Node) emit(value any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.out.Encode(value)
}

func (n *Node) Send(to string, payload any) error {
	valid := false
	for _, peer := range n.Nodes {
		if peer == to {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown node %q", to)
	}
	return n.emit(map[string]any{"type": "send", "to": to, "payload": payload})
}

func (n *Node) Receive(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case msg, ok := <-n.inbox:
		if !ok {
			return Message{}, io.EOF
		}
		if err := n.emit(map[string]any{"type": "received", "id": msg.ID}); err != nil {
			return Message{}, err
		}
		return msg, nil
	}
}

func (n *Node) Report(state any) error {
	return n.emit(map[string]any{"type": "state", "state": state})
}

// Save persists protocol state across process crashes in the node's /state directory.
func Save(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(stateFile("state.json.tmp"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(stateFile("state.json.tmp"), stateFile("state.json"))
}

func Load(value any) error {
	data, err := os.ReadFile(stateFile("state.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func stateFile(name string) string {
	dir := os.Getenv("DISTVIS_STATE_DIR")
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, name)
}
