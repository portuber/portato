package forward

import (
	"encoding/json"
	"testing"
)

func TestStateMarshalRoundTrip(t *testing.T) {
	for _, s := range []State{Off, Connecting, Connected, Reconnecting, Error} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %s: %v", s, err)
		}
		var got string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if got != s.String() {
			t.Fatalf("state %s marshaled as %q", s, got)
		}
		var back State
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal state: %v", err)
		}
		if back != s {
			t.Fatalf("round-trip %s -> %s", s, back)
		}
	}
}

func TestStateUnknownNameMapsToOff(t *testing.T) {
	var s State
	if err := json.Unmarshal([]byte(`"bogus"`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s != Off {
		t.Fatalf("expected Off, got %s", s)
	}
}

func TestStatusJSONShape(t *testing.T) {
	s := Status{
		Name:   "db",
		Type:   "local",
		Local:  "127.0.0.1:5432",
		Remote: "db:5432",
		State:  Connected,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "type", "local", "remote", "state", "connected_at"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing json key %q in %s", key, data)
		}
	}
	if _, ok := m["error"]; ok {
		t.Fatalf("empty error should be omitted, got %s", data)
	}
	var back Status
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != s.Name || back.State != s.State {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}
