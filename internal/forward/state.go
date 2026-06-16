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
}

func (s Status) Uptime() time.Duration {
	if s.State != Connected || s.ConnectedAt.IsZero() {
		return 0
	}
	return time.Since(s.ConnectedAt)
}
