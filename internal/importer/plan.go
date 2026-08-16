package importer

import (
	"fmt"
	"strconv"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"

	"github.com/portuber/portato/internal/config"
)

// Planned pairs a ready-to-append tuber with the candidate it came from.
type Planned struct {
	Tuber     config.Tuber
	Candidate Candidate
}

// Plan is the prepared outcome of an import: the named tubers to append and
// the candidates skipped as already covered.
type Plan struct {
	Add     []Planned
	Skipped []Candidate
}

// PlanImport prepares candidates for writing. It drops a candidate an
// existing tuber already covers — same type, same normalised listen
// addresses, same resolved ssh host. Both sides are resolved against the
// scanned ssh config (via config.ResolveSSHHost, Phase-44 precedence) so a
// --from import still dedups consistently: an explicit-host tuber matches an
// alias candidate for the same machine, and two aliases for one HostName
// collapse. It names the rest from the host pattern and the declared listen
// port ("db-5432"), and de-conflicts names against the config plus earlier
// additions with a -2/-3 suffix. The tubers keep the raw pattern in `ssh:`
// (never a resolved host) so load-time resolution and Save round-trip the
// user's own spelling. The caller appends, prepares, validates and saves.
func PlanImport(cfg *config.Config, sshCfg *ssh_config.Config, cands []Candidate) (*Plan, error) {
	plan := &Plan{}
	taken := make(map[string]struct{}, len(cfg.Tubers))
	covered := make(map[string]struct{}, len(cfg.Tubers))
	for i := range cfg.Tubers {
		t := &cfg.Tubers[i]
		taken[t.Name] = struct{}{}
		host, port := t.Host, t.Port
		if strings.TrimSpace(t.SSH) != "" {
			if h, p := config.ResolveSSHHost(t.SSH, sshCfg); h != "" {
				host, port = h, p
			}
		}
		covered[coverageKey(t.Type, t.ListenAddr(), t.RemoteListenAddr(), host, port)] = struct{}{}
	}
	for _, c := range cands {
		host, port := config.ResolveSSHHost(c.SSHHost, sshCfg)
		if host == "" {
			return nil, fmt.Errorf("ssh host %q: proxyjump from ssh config does not resolve", c.SSHHost)
		}
		probe := config.Tuber{Type: c.Type, Local: c.Local, Remote: c.Remote}
		key := coverageKey(probe.Type, probe.ListenAddr(), probe.RemoteListenAddr(), host, port)
		if _, dup := covered[key]; dup {
			plan.Skipped = append(plan.Skipped, c)
			continue
		}
		covered[key] = struct{}{}
		name := tuberName(c, taken)
		plan.Add = append(plan.Add, Planned{
			Tuber: config.Tuber{
				Name:    name,
				Type:    c.Type,
				Local:   c.Local,
				Remote:  c.Remote,
				SSH:     c.SSHHost,
				Enabled: false,
			},
			Candidate: c,
		})
	}
	return plan, nil
}

// coverageKey identifies a forward semantically: type, the normalised listen
// addresses (Tuber.ListenAddr / RemoteListenAddr expand bare ports the way
// the engine will bind them), and the resolved ssh endpoint.
func coverageKey(typ, local, remote, host string, port int) string {
	return typ + "|" + local + "|" + remote + "|" + host + "|" + strconv.Itoa(port)
}

// tuberName derives a valid tuber name from the host pattern and the
// declared listen port ("db-5432"), and de-conflicts it against taken
// (existing tubers plus names assigned earlier in this import) with a
// -2/-3 suffix.
func tuberName(c Candidate, taken map[string]struct{}) string {
	base := sanitizeName(c.SSHHost)
	if port := declaredPort(c); port != "" {
		base += "-" + port
	}
	if base == "" {
		base = "imported"
	}
	name := base
	for n := 2; ; n++ {
		if _, clash := taken[name]; !clash {
			break
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
	taken[name] = struct{}{}
	return name
}

// declaredPort is the listen port the directive declared: the local side for
// local/dynamic forwards, the remote side for remote (the fields swap).
func declaredPort(c Candidate) string {
	s := c.Local
	if c.Type == "remote" {
		s = c.Remote
	}
	_, port, ok := splitBind(s)
	if !ok {
		return ""
	}
	return port
}

// sanitizeName lowercases a host pattern and replaces every rune outside the
// validName alphabet with a single dash ("Web.Prod" → "web-prod").
func sanitizeName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		ok := r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !ok {
			dash = true
			continue
		}
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		dash = false
		b.WriteRune(r)
	}
	return b.String()
}
