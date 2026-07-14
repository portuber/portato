package forward

import (
	"encoding/json"
	"time"
)

// State is the lifecycle state of a tuber.
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

// Status is a point-in-time snapshot of a tuber.
type Status struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Local       string    `json:"local"`
	Remote      string    `json:"remote"`
	State       State     `json:"state"`
	Error       string    `json:"error,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`

	// TOFU (Phase 11): when the tuber is blocked by an unknown SSH host key
	// (accept_new_hosts: false), these carry the offending host, its
	// fingerprint and a ready-to-append known_hosts line so the TUI can offer
	// to accept it. Empty when not applicable.
	PendingHost        string `json:"pending_host,omitempty"`
	PendingFingerprint string `json:"pending_fingerprint,omitempty"`
	PendingHostLine    string `json:"pending_host_line,omitempty"`

	// Passphrase (Phase 19): when the tuber's identity key is
	// passphrase-protected and no passphrase is available yet, this carries
	// the identity path that needs one, so the TUI/CLI can prompt. The dial
	// blocks (PassphraseProvider.Wait) until a passphrase arrives, rather than
	// spinning the reconnect backoff. Empty when not applicable.
	PendingPassphrase string `json:"pending_passphrase,omitempty"`

	// Password (Phase 35): when the tuber has opted into password_auth, no
	// usable key authenticated, and no password is available yet, this carries
	// the server account ("password:<user>@<host>:<port>") that needs one, so
	// the TUI/CLI can prompt. The dial blocks (PasswordProvider.Wait) until a
	// password arrives, rather than spinning the reconnect backoff. Empty when
	// not applicable. State stays Connecting while blocked.
	PendingPassword string `json:"pending_password,omitempty"`
	// PasswordAttempts (Phase 35) is how many times the server has rejected a
	// submitted password for this tuber. The TUI uses it to show an accurate
	// "wrong password" hint only on a real rejection. 0 when none / on a fresh
	// dial attempt.
	PasswordAttempts int `json:"password_attempts,omitempty"`
}

func (s Status) Uptime() time.Duration {
	if s.State != Connected || s.ConnectedAt.IsZero() {
		return 0
	}
	return time.Since(s.ConnectedAt)
}

// Endpoint renders the directional endpoint string for display: "local →
// remote" for a local tuber, "local ← remote" for a remote tuber (traffic
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
