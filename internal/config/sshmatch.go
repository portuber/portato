package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxLenientIncludeDepth mirrors the ssh_config library's own include depth
// cap (5, also importer's maxIncludeDepth): deeper chains keep the original
// Include line so the decoder reports them exactly as it does today.
const maxLenientIncludeDepth = 5

// matchCriterionWords are the ssh_config(5) Match criteria. Portato (like the
// kevinburke/ssh_config decoder) can only evaluate `all` and `host`; a block
// headed by any other criterion — or by several criteria ANDed — can never be
// matched, so the lenient reader skips it (phase 60).
var matchCriterionWords = map[string]struct{}{
	"all":          {},
	"canonical":    {},
	"exec":         {},
	"final":        {},
	"host":         {},
	"localuser":    {},
	"originalhost": {},
	"rdomain":      {},
	"user":         {},
}

// LenientSSHConfig rewrites raw ssh_config text so the decoder never sees
// Match blocks portato cannot evaluate (issue #4, phase 60). OpenSSH
// semantics: a Match criterion the client cannot evaluate simply never
// matches, so the block's directives are inert — dropping the whole block
// (its header plus every directive up to the next Host/Match header) is
// behaviour-preserving. Each dropped block is reported through warn, named
// by its file and header line. Include directives are expanded first: the
// library resolves them inside Decode, where an unsupported Match in an
// included file would still abort the whole load. Path rules mirror the
// library (absolute and ~/ as-is, everything else relative to ~/.ssh,
// wildcards globbed); a malformed, unreadable, or too-deep Include keeps the
// original line so the decoder surfaces the same error it does today.
func LenientSSHConfig(data []byte, path string, warn func(string)) []byte {
	lines := filterSSHConfigLines(strings.Split(string(data), "\n"), path, 0,
		map[string]bool{filepath.Clean(path): true}, warn)
	return []byte(strings.Join(lines, "\n"))
}

// filterSSHConfigLines is LenientSSHConfig's recursion body: lines in,
// filtered lines out. skipping is true while the reader is inside a dropped
// Match block (everything until the next Host/Match header goes with it).
func filterSSHConfigLines(lines []string, path string, depth int, visiting map[string]bool, warn func(string)) []string {
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == '#' {
			if !skipping {
				out = append(out, line)
			}
			continue
		}
		toks := splitSSHTokens(trimmed)
		if len(toks) == 0 {
			out = append(out, line)
			continue
		}
		kw, args := splitKeyword(toks)
		switch kw {
		case "host":
			skipping = false
			out = append(out, line)
		case "match":
			if matchEvaluable(args) {
				skipping = false
				out = append(out, line)
			} else {
				skipping = true
				warn(fmt.Sprintf("%s: skipped Match block %q (portato cannot evaluate it; like OpenSSH, it never matches here)", path, trimmed))
			}
		case "include":
			if skipping {
				continue
			}
			out = append(out, expandLenientInclude(args, trimmed, depth, visiting, warn)...)
		default:
			if !skipping {
				out = append(out, line)
			}
		}
	}
	return out
}

// splitKeyword splits a directive's first token into keyword and arguments,
// accepting the `Keyword=value` form (args then start with the text after
// the equals sign).
func splitKeyword(toks []string) (string, []string) {
	kw := strings.ToLower(toks[0])
	if i := strings.IndexByte(toks[0], '='); i >= 0 {
		kw = strings.ToLower(toks[0][:i])
		args := toks[1:]
		if v := toks[0][i+1:]; v != "" {
			args = append([]string{v}, args...)
		}
		return kw, args
	}
	return kw, toks[1:]
}

// matchEvaluable reports whether the ssh_config library can decode a Match
// header with these criteria: a single `all`, or `host` with at least one
// pattern. Anything else — exec, user, final, … or several criteria ANDed —
// would make Decode fail (or, for `host` + extras, silently misparse), so
// the block is skipped instead.
func matchEvaluable(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(args[0]) {
	case "all":
		return !hasCriterionWord(args[1:])
	case "host":
		return len(args) > 1 && !hasCriterionWord(args[1:])
	default:
		return false
	}
}

// hasCriterionWord reports whether any argument is itself a Match criterion
// keyword, i.e. the header ANDs several criteria ("Match host x exec y").
func hasCriterionWord(args []string) bool {
	for _, a := range args {
		if _, ok := matchCriterionWords[strings.ToLower(a)]; ok {
			return true
		}
	}
	return false
}

// expandLenientInclude resolves an Include directive and returns the filtered
// lines of the included files in glob order (textual insertion, OpenSSH's
// model). The original line survives only when the decoder must complain
// about it itself (no argument, bad pattern, unreadable file, depth
// exceeded); a glob that matches nothing contributes nothing, silently.
func expandLenientInclude(args []string, line string, depth int, visiting map[string]bool, warn func(string)) []string {
	if depth >= maxLenientIncludeDepth {
		return []string{line}
	}
	if len(args) == 0 {
		return []string{line}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{line}
	}
	var files []string
	for _, a := range args {
		p := a
		switch {
		case filepath.IsAbs(p):
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		default:
			p = filepath.Join(home, ".ssh", p)
		}
		ms, err := filepath.Glob(p)
		if err != nil {
			return []string{line}
		}
		files = append(files, ms...)
	}
	files = dedupeStrings(files)
	if len(files) == 0 {
		return nil
	}
	var out []string
	for _, f := range files {
		key := filepath.Clean(f)
		if visiting[key] {
			warn(fmt.Sprintf("%s: include cycle; skipping", key))
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return []string{line}
		}
		visiting[key] = true
		out = append(out, filterSSHConfigLines(strings.Split(string(data), "\n"), f, depth+1, visiting, warn)...)
		delete(visiting, key)
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// splitSSHTokens splits a directive line into whitespace-separated tokens,
// honouring double quotes and stopping at an unquoted comment ('#' at a
// token start), the way the ssh_config lexer does.
func splitSSHTokens(s string) []string {
	var toks []string
	var cur strings.Builder
	inQuote, haveTok := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			haveTok = true
		case !inQuote && (c == ' ' || c == '\t'):
			if haveTok {
				toks = append(toks, cur.String())
				cur.Reset()
				haveTok = false
			}
		case !inQuote && c == '#' && !haveTok:
			return toks
		default:
			cur.WriteByte(c)
			haveTok = true
		}
	}
	if haveTok {
		toks = append(toks, cur.String())
	}
	return toks
}
