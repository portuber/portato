package cmd

import (
	"strings"
	"testing"
)

const (
	mitBodyMarker     = "OUT OF OR IN CONNECTION WITH THE SOFTWARE"
	mitBodyAbsent     = "PERMISSION IS HEREBY GRANTED"
	licenseFullHint   = "portato license --full"
	licenseThirdParty = "THIRD_PARTY_LICENSES.txt"
)

// TestLicenseShort verifies the short summary carries the MIT tag, the source
// URL, the third-party-notices pointer and the --full hint — but not the full
// MIT License body.
func TestLicenseShort(t *testing.T) {
	var b strings.Builder
	printLicense(&b, false)
	out := b.String()

	for _, want := range []string{"MIT", licenseSourceURL, licenseThirdParty, licenseFullHint} {
		if !strings.Contains(out, want) {
			t.Errorf("short license output missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, mitBodyAbsent) {
		t.Errorf("short license output must not include the full MIT body:\n%s", out)
	}
}

// TestLicenseFull verifies the --full form carries the short summary AND the
// full MIT License text.
func TestLicenseFull(t *testing.T) {
	var b strings.Builder
	printLicense(&b, true)
	out := b.String()

	for _, want := range []string{"License: MIT", licenseSourceURL, mitBodyMarker} {
		if !strings.Contains(out, want) {
			t.Errorf("full license output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestLicenseCmdRun verifies the `portato license` subcommand prints the short
// summary by default and the long form under --full.
func TestLicenseCmdRun(t *testing.T) {
	// Short form (default).
	c, out, errOut := captureCmd()
	if err := licenseCmd.RunE(c, nil); err != nil {
		t.Fatalf("licenseCmd: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), licenseSourceURL) {
		t.Errorf("license subcommand output missing source URL: %q", out.String())
	}
	if strings.Contains(out.String(), mitBodyAbsent) {
		t.Errorf("license subcommand must not print the full body by default:\n%s", out.String())
	}

	// Long form (--full): toggle the bound flag and restore it afterwards.
	prev := licenseFull
	licenseFull = true
	t.Cleanup(func() { licenseFull = prev })

	c2, out2, errOut2 := captureCmd()
	if err := licenseCmd.RunE(c2, nil); err != nil {
		t.Fatalf("licenseCmd --full: %v", err)
	}
	if errOut2.String() != "" {
		t.Errorf("unexpected stderr (--full): %q", errOut2.String())
	}
	if !strings.Contains(out2.String(), mitBodyMarker) {
		t.Errorf("license --full output missing the MIT body:\n%s", out2.String())
	}
}

// TestRootLicenseFlag verifies `portato --license` (rootRunE path) prints the
// short summary and returns nil without starting the daemon or TUI.
func TestRootLicenseFlag(t *testing.T) {
	prev := showLicense
	showLicense = true
	t.Cleanup(func() { showLicense = prev })

	c, out, errOut := captureCmd()
	if err := rootRunE(c, nil); err != nil {
		t.Fatalf("rootRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), licenseSourceURL) {
		t.Errorf("--license output missing source URL: %q", out.String())
	}
	if strings.Contains(out.String(), mitBodyAbsent) {
		t.Errorf("--license must print the short summary only:\n%s", out.String())
	}
}
