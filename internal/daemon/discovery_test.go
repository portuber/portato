package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryPathUnderConfigHome(t *testing.T) {
	p, err := DiscoveryPath()
	if err != nil {
		t.Fatalf("DiscoveryPath: %v", err)
	}
	if filepath.Base(p) != markerFile {
		t.Fatalf("DiscoveryPath base = %q, want %q", filepath.Base(p), markerFile)
	}
	if filepath.Base(filepath.Dir(p)) != "portato" {
		t.Fatalf("DiscoveryPath should sit under .../portato/, got %q", p)
	}
}

func TestRuntimeSocketPathUidScoped(t *testing.T) {
	p, err := RuntimeSocketPath()
	if err != nil {
		t.Fatalf("RuntimeSocketPath: %v", err)
	}
	if filepath.Base(p) == "portato.sock" {
		t.Fatalf("runtime socket name must be uid-scoped to avoid collisions, got %q", p)
	}
}

func TestWriteReadMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "nested", "daemon.socket") // dir does not exist yet
	socket := filepath.Join(dir, "portato.sock")
	if err := WriteMarker(mp, socket, 12345); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	// The parent dir is created by WriteMarker.
	if _, err := os.Stat(filepath.Dir(mp)); err != nil {
		t.Fatalf("marker dir not created: %v", err)
	}
	// Mode 0600.
	if info, err := os.Stat(mp); err != nil {
		t.Fatalf("stat marker: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("marker perm = %o, want 0600", info.Mode().Perm())
	}
	m, err := ReadMarker(mp)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if m.Socket != socket || m.PID != 12345 {
		t.Fatalf("marker = %+v, want socket=%q pid=12345", m, socket)
	}
}

func TestReadMarkerMissingIsNotExist(t *testing.T) {
	_, err := ReadMarker(filepath.Join(t.TempDir(), "nope"))
	if !os.IsNotExist(err) {
		t.Fatalf("want os.IsNotExist, got %v", err)
	}
}

func TestReadMarkerCorrupt(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := os.WriteFile(mp, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(mp); err == nil {
		t.Fatal("expected error for corrupt marker")
	}
}

func TestReadMarkerInvalidFields(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := os.WriteFile(mp, []byte(`{"socket":"","pid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(mp); err == nil {
		t.Fatal("expected error for marker with empty socket / pid 0")
	}
}

func TestRemoveMarkerIdempotent(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := RemoveMarker(mp); err != nil {
		t.Fatalf("remove on missing marker: %v", err)
	}
	if err := WriteMarker(mp, "/x", 1); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMarker(mp); err != nil {
		t.Fatalf("remove existing marker: %v", err)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("marker still present after RemoveMarker")
	}
}

func TestResolveSocketNoMarker(t *testing.T) {
	t.Setenv("PORTATO_SOCKET", "")
	socketOverride = "" // override cleared via SetSocketOverride in prod
	// Point discovery at a temp dir by setting XDG_CONFIG_HOME.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty socket when no marker, got %q", got)
	}
}

func TestResolveSocketStaleMarkerCleaned(t *testing.T) {
	t.Setenv("PORTATO_SOCKET", "")
	socketOverride = ""
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	mp, _ := DiscoveryPath()
	deadSocket := filepath.Join(dir, "dead.sock")
	_ = os.WriteFile(deadSocket, []byte{}, 0o600)
	if err := WriteMarker(mp, deadSocket, 999999); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty for stale marker, got %q", got)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("stale marker not removed by ResolveSocket")
	}
	if _, err := os.Stat(deadSocket); !os.IsNotExist(err) {
		t.Fatalf("stale socket pointed at by marker not removed")
	}
}

func TestResolveSocketLiveMarker(t *testing.T) {
	t.Setenv("PORTATO_SOCKET", "")
	socketOverride = ""
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	mp, _ := DiscoveryPath()
	liveSocket := filepath.Join(dir, "live.sock")
	if err := WriteMarker(mp, liveSocket, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != liveSocket {
		t.Fatalf("ResolveSocket = %q, want %q", got, liveSocket)
	}
}

func TestResolveSocketOverrideFlagWins(t *testing.T) {
	defer func() { socketOverride = "" }()
	SetSocketOverride("/tmp/portato-override-test.sock")
	if got := SocketOverride(); got != "/tmp/portato-override-test.sock" {
		t.Fatalf("SocketOverride flag = %q", got)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "/tmp/portato-override-test.sock" {
		t.Fatalf("ResolveSocket override = %q", got)
	}
}

func TestSocketOverrideEnvFallback(t *testing.T) {
	socketOverride = ""
	t.Setenv("PORTATO_SOCKET", "/tmp/portato-env-test.sock")
	if got := SocketOverride(); got != "/tmp/portato-env-test.sock" {
		t.Fatalf("SocketOverride env = %q", got)
	}
}

func TestWriteMarkerIsJSON(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := WriteMarker(mp, "/s", 42); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	var m Marker
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("marker is not valid JSON: %v", err)
	}
}
