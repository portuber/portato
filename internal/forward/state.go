package forward

import "time"

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

func (s State) String() string {
	if s < 0 || int(s) >= len(stateNames) {
		return "unknown"
	}
	return stateNames[s]
}

// Status is a point-in-time snapshot of a tunnel.
type Status struct {
	Name        string
	Type        string
	Local       string
	Remote      string
	State       State
	Error       string
	ConnectedAt time.Time
}

func (s Status) Uptime() time.Duration {
	if s.State != Connected || s.ConnectedAt.IsZero() {
		return 0
	}
	return time.Since(s.ConnectedAt)
}
