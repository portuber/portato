package forward

import (
	"encoding/json"
	"time"
)

// State is the lifecycle state of a tunnel.
type State int

const (
	Off State = iota
	Connecting
	Connected
	Reconnecting
	Error
)

var stateNames = [...]string{
	Off:          "off",
	Connecting:   "connecting",
	Connected:    "connected",
	Reconnecting: "reconnecting",
	Error:        "error",
}

var stateByName = map[string]State{
	"off":          Off,
	"connecting":   Connecting,
	"connected":    Connected,
	"reconnecting": Reconnecting,
	"error":        Error,
}

func (s State) String() string {
	if s < 0 || int(s) >= len(stateNames) {
		return "unknown"
	}
	return stateNames[s]
}

// MarshalJSON renders State as its human-readable name (e.g. "connected"),
// used by the daemon IPC layer.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON restores State from its name; unknown names map to Off.
func (s *State) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	if v, ok := stateByName[name]; ok {
		*s = v
		return nil
	}
	*s = Off
	return nil
}

// Status is a point-in-time snapshot of a tunnel.
type Status struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Local       string    `json:"local"`
	Remote      string    `json:"remote"`
	State       State     `json:"state"`
	Error       string    `json:"error,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`

	// TOFU (Phase 11): when the tunnel is blocked by an unknown SSH host key
	// (accept_new_hosts: false), these carry the offending host, its
	// fingerprint and a ready-to-append known_hosts line so the TUI can offer
	// to accept it. Empty when not applicable.
	PendingHost        string `json:"pending_host,omitempty"`
	PendingFingerprint string `json:"pending_fingerprint,omitempty"`
	PendingHostLine    string `json:"pending_host_line,omitempty"`
}

func (s Status) Uptime() time.Duration {
	if s.State != Connected || s.ConnectedAt.IsZero() {
		return 0
	}
	return time.Since(s.ConnectedAt)
}

// Endpoint renders the directional endpoint string for display: "local →
// remote" for a local tunnel, "local ← remote" for a remote tunnel (traffic
// flows from the server to here). The arrow encodes the direction.
func (s Status) Endpoint() string {
	switch s.Type {
	case "remote":
		return s.Local + " ← " + s.Remote
	case "dynamic":
		return s.Local + " ⇄ *"
	default:
		return s.Local + " → " + s.Remote
	}
}
