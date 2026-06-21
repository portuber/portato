package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// This file implements comment-preserving edits to the config file by operating
// on its yaml.Node tree. Adding, replacing or removing a single tunnel touches
// only that tunnel's node: comments on every other tunnel, on defaults: and at
// the top of the file are preserved. A tunnel that is replaced is re-marshaled
// from the struct, so its own inline comments are not retained.
//
// These functions only edit the file's AST; they do not validate. Callers
// (the controllers) first validate a prospective in-memory config via the
// WithTunnel* helpers below, then apply the matching *TunnelNode edit and
// Reload.

// LoadNode reads the YAML file at path into a *yaml.Node document tree,
// preserving comments and structure. Used by the AST patch operations.
func LoadNode(path string) (*yaml.Node, error) {
	path = expandTilde(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &doc, nil
}

// SaveNode writes a *yaml.Node document tree to path (dir 0700, file 0600).
// The output uses the same formatting as Config.Save so enable/disable
// (which go through Save) and edit/new/delete produce identical style.
func SaveNode(path string, doc *yaml.Node) error {
	path = expandTilde(path)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// AddTunnelNode appends tunnel t to the tunnels list in the YAML file at path,
// creating the tunnels: sequence if it is absent.
func AddTunnelNode(path string, t Tunnel) error {
	doc, err := LoadNode(path)
	if err != nil {
		return err
	}
	root, err := documentRoot(doc)
	if err != nil {
		return err
	}
	seq, err := tunnelsSequence(root, true)
	if err != nil {
		return err
	}
	node, err := tunnelNode(t)
	if err != nil {
		return err
	}
	seq.Content = append(seq.Content, node)
	return SaveNode(path, doc)
}

// ReplaceTunnelNode replaces the tunnel named name with t in the file, allowing
// a rename (t.Name may differ from name). It errors if name is not present.
func ReplaceTunnelNode(path, name string, t Tunnel) error {
	doc, err := LoadNode(path)
	if err != nil {
		return err
	}
	root, err := documentRoot(doc)
	if err != nil {
		return err
	}
	seq, err := tunnelsSequence(root, false)
	if err != nil {
		return err
	}
	idx := findTunnelIndex(seq, name)
	if idx < 0 {
		return fmt.Errorf("tunnel %q not found", name)
	}
	node, err := tunnelNode(t)
	if err != nil {
		return err
	}
	seq.Content[idx] = node
	return SaveNode(path, doc)
}

// DeleteTunnelNode removes the tunnel named name from the file. It errors if
// name is not present.
func DeleteTunnelNode(path, name string) error {
	doc, err := LoadNode(path)
	if err != nil {
		return err
	}
	root, err := documentRoot(doc)
	if err != nil {
		return err
	}
	seq, err := tunnelsSequence(root, false)
	if err != nil {
		return err
	}
	idx := findTunnelIndex(seq, name)
	if idx < 0 {
		return fmt.Errorf("tunnel %q not found", name)
	}
	seq.Content = append(seq.Content[:idx], seq.Content[idx+1:]...)
	return SaveNode(path, doc)
}

// WithTunnelAdded returns a copy of c with t appended, then prepared and
// validated. It does not touch the file. Use it to validate a creation before
// applying AddTunnelNode.
func (c *Config) WithTunnelAdded(t Tunnel) (*Config, error) {
	out := c.clone()
	out.Tunnels = append(out.Tunnels, t)
	out.prepare()
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// WithTunnelReplaced returns a copy of c where the tunnel named name is
// replaced by t (rename allowed), then prepared and validated.
func (c *Config) WithTunnelReplaced(name string, t Tunnel) (*Config, error) {
	out := c.clone()
	idx := -1
	for i := range out.Tunnels {
		if out.Tunnels[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("tunnel %q not found", name)
	}
	out.Tunnels[idx] = t
	out.prepare()
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// WithTunnelRemoved returns a copy of c with the tunnel named name removed,
// then prepared and validated.
func (c *Config) WithTunnelRemoved(name string) (*Config, error) {
	out := c.clone()
	idx := -1
	for i := range out.Tunnels {
		if out.Tunnels[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("tunnel %q not found", name)
	}
	out.Tunnels = append(out.Tunnels[:idx], out.Tunnels[idx+1:]...)
	out.prepare()
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// Clone returns a deep copy of the config so callers can mutate it without
// affecting the controller's in-memory state.
func (c *Config) Clone() *Config { return c.clone() }

func (c *Config) clone() *Config {
	out := &Config{Defaults: c.Defaults}
	if c.Tunnels != nil {
		out.Tunnels = make([]Tunnel, len(c.Tunnels))
		copy(out.Tunnels, c.Tunnels)
	}
	return out
}

func documentRoot(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("invalid config document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root is not a mapping")
	}
	return root, nil
}

// tunnelsSequence returns the sequence node under the "tunnels" key. When
// create is true and the key is absent, it is added with an empty sequence.
func tunnelsSequence(root *yaml.Node, create bool) (*yaml.Node, error) {
	seq := mappingValue(root, "tunnels")
	if seq == nil {
		if !create {
			return nil, fmt.Errorf("config has no tunnels")
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tunnels"}
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, key, seq)
		return seq, nil
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("config tunnels is not a sequence")
	}
	return seq, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func tunnelName(mapping *yaml.Node) string {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return ""
	}
	n := mappingValue(mapping, "name")
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func findTunnelIndex(seq *yaml.Node, name string) int {
	for i, n := range seq.Content {
		if tunnelName(n) == name {
			return i
		}
	}
	return -1
}

// tunnelNode marshals a single Tunnel into a mapping node suitable for splice
// into the tunnels sequence.
func tunnelNode(t Tunnel) (*yaml.Node, error) {
	data, err := yaml.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("marshal tunnel: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse tunnel node: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("unexpected tunnel node")
	}
	return doc.Content[0], nil
}
