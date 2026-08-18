//go:build windows

package service

import (
	"os/exec"
	"strings"
	"testing"
)

// TestSCMInstalled_MatchesSCExe pins the no-elevation contract: SCMInstalled
// must report the service's existence exactly, without administrator rights
// (the SCM opens with SC_MANAGER_CONNECT and the service with
// SERVICE_QUERY_STATUS — both granted to ordinary users). sc.exe query is the
// independent oracle, equally unelevated. A machine without the service is
// the common case; a smoke-test host that installed it is equally fine.
func TestSCMInstalled_MatchesSCExe(t *testing.T) {
	out, err := exec.Command("sc.exe", "query", ServiceName).CombinedOutput()
	installed := err == nil && !strings.Contains(string(out), "FAILED 1060")
	if installed && !SCMInstalled() {
		t.Errorf("SCMInstalled() = false but sc.exe sees the service (access-denied misread as not-installed?)\nsc.exe: %s", out)
	}
	if !installed && SCMInstalled() {
		t.Errorf("SCMInstalled() = true but sc.exe does not see the service\nsc.exe: %s", out)
	}
}
