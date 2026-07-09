package tui

import (
	"testing"

	"github.com/portuber/portato/internal/controller"
)

// fuzzyFixture is a richer set used for the Phase 20 fuzzy / filter: it includes
// hyphenated multi-token names (db-stage, db-replica, web-prod, cache) so
// subsequence matching can be told apart from plain substring matching.
func fuzzyFixture() *fakeCtrl {
	return newFake(
		controller.Status{Name: "db-stage", Type: "local", Local: "5432", Remote: "10.0.0.5:5432"},
		controller.Status{Name: "db-replica", Type: "remote", Local: "6432", Remote: "db:6432"},
		controller.Status{Name: "web-prod", Type: "dynamic", Local: "1080"},
		controller.Status{Name: "cache", Type: "local", Local: "6379", Remote: "cache:6379"},
	)
}

// TestFilter_FuzzyMatchesSubsequence is the Phase 20 DoD check: typing a
// non-contiguous subsequence ("dbst") selects "db-stage" even though "dbst" is
// not a substring of it. Pre-Phase-20 this query matched nothing.
func TestFilter_FuzzyMatchesSubsequence(t *testing.T) {
	m := New(fuzzyFixture(), Options{Mode: "standalone"})
	m.filter.SetValue("dbst")

	wantMatch := []string{"db-stage"}
	for _, name := range wantMatch {
		if !m.matches(controller.Status{Name: name, Type: "local", Local: "5432", Remote: "x"}) {
			t.Errorf("fuzzy: %q should match query dbst", name)
		}
	}
	wantMiss := []string{"web-prod", "cache", "db-replica"}
	for _, name := range wantMiss {
		if m.matches(controller.Status{Name: name, Type: "local", Local: "1", Remote: "x"}) {
			t.Errorf("fuzzy: %q should NOT match query dbst", name)
		}
	}
}

// TestFilter_FuzzyStillMatchesExactSubstring guards the fallback: a query that
// is a contiguous substring still matches (substring ⊂ subsequence, so this
// holds for both the fuzzy matcher and the defensive fallback).
func TestFilter_FuzzyStillMatchesExactSubstring(t *testing.T) {
	m := New(fuzzyFixture(), Options{Mode: "standalone"})
	for _, q := range []string{"db-stage", "stage", "prod", "cache", "eplica"} {
		m.filter.SetValue(q)
		// "stage" is a substring of db-stage; "prod" of web-prod; etc.
		target := "db-stage"
		if q == "prod" {
			target = "web-prod"
		} else if q == "cache" {
			target = "cache"
		} else if q == "eplica" {
			target = "db-replica"
		}
		if !m.matches(controller.Status{Name: target, Type: "local", Local: "1", Remote: "x"}) {
			t.Errorf("substring fallback: %q should match %q", q, target)
		}
	}
}

// TestFilter_FuzzyCaseInsensitive verifies MatchFold lowercases both sides, so
// "DBST" matches "db-stage" the same as "dbst".
func TestFilter_FuzzyCaseInsensitive(t *testing.T) {
	m := New(fuzzyFixture(), Options{Mode: "standalone"})
	for _, q := range []string{"DBST", "DbSt", "WEB"} {
		m.filter.SetValue(q)
		target := "db-stage"
		if q == "WEB" {
			target = "web-prod"
		}
		if !m.matches(controller.Status{Name: target, Type: "local", Local: "1", Remote: "x"}) {
			t.Errorf("case-insensitive fuzzy: %q should match %q", q, target)
		}
	}
}

// TestFilter_FuzzyMatchesTypeAndEndpoint ensures fuzzy matching still covers
// the type and endpoint fields (not just the name), preserving the
// multi-field scope of the pre-Phase-20 substring filter.
func TestFilter_FuzzyMatchesTypeAndEndpoint(t *testing.T) {
	m := New(fuzzyFixture(), Options{Mode: "standalone"})
	// "dymc" is a subsequence of "dynamic" — the type of web-prod. The name
	// "web-prod" does not contain d/y/m/c, so this only matches via the type.
	m.filter.SetValue("dymc")
	if !m.matches(controller.Status{Name: "web-prod", Type: "dynamic", Local: "1080"}) {
		t.Error("fuzzy should match the 'dynamic' type via subsequence 'dymc'")
	}

	// Endpoint-only match: a tunnel whose name and type share no chars with
	// the query, but whose endpoint ("zz ← lonely-host:9") does.
	m.filter.SetValue("lnlyhst")
	s := controller.Status{Name: "zz", Type: "remote", Local: "1", Remote: "lonely-host:9"}
	if !m.matches(s) {
		t.Error("fuzzy should match the endpoint via subsequence 'lnlyhst' in lonely-host")
	}
}

// TestFilter_EmptyQueryMatchesAll guards the empty-query short-circuit.
func TestFilter_EmptyQueryMatchesAll(t *testing.T) {
	m := New(fuzzyFixture(), Options{Mode: "standalone"})
	m.filter.SetValue("")
	for _, name := range []string{"db-stage", "web-prod", "cache"} {
		if !m.matches(controller.Status{Name: name, Type: "local", Local: "1", Remote: "x"}) {
			t.Errorf("empty query should match %q", name)
		}
	}
}
