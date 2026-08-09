package tui

import (
	"reflect"
	"sort"
	"testing"

	"github.com/portuber/portato/internal/controller"
)

// tagsFixture: db-prod and web-prod share the prod tag; db-prod also has db;
// cache is untagged. db-stage is NAMED with a db prefix but NOT tagged db — the
// key trap: #db must match the tag, not the name.
func tagsFixture() *fakeCtrl {
	return newFake(
		controller.Status{Name: "db-prod", Type: "local", Local: "5432", Remote: "db:5432", State: controller.Off, Tags: []string{"prod", "db"}},
		controller.Status{Name: "web-prod", Type: "local", Local: "8080", Remote: "web:80", State: controller.Off, Tags: []string{"prod"}},
		controller.Status{Name: "db-stage", Type: "local", Local: "5433", Remote: "db:5433", State: controller.Off},
		controller.Status{Name: "cache", Type: "local", Local: "6379", Remote: "cache:6379", State: controller.Off},
	)
}

// TestFilter_TagExact covers the Phase 46 #tag selector: a leading # is an
// exact tag match (case-insensitive), and crucially #db does NOT match a tuber
// merely named db-stage (the name-vs-tag disambiguation).
func TestFilter_TagExact(t *testing.T) {
	m := New(tagsFixture(), Options{Mode: "standalone"})

	m.filter.SetValue("#db")
	if !m.matches(controller.Status{Name: "db-prod", Tags: []string{"prod", "db"}}) {
		t.Error("#db should match the db-tagged tuber")
	}
	if m.matches(controller.Status{Name: "db-stage"}) {
		t.Error("#db must NOT match a tuber merely NAMED db-stage (no db tag)")
	}

	m.filter.SetValue("#prod")
	if !m.matches(controller.Status{Name: "db-prod", Tags: []string{"prod", "db"}}) {
		t.Error("#prod should match db-prod")
	}
	if !m.matches(controller.Status{Name: "web-prod", Tags: []string{"prod"}}) {
		t.Error("#prod should match web-prod")
	}
	if m.matches(controller.Status{Name: "cache"}) {
		t.Error("#prod must NOT match cache")
	}
}

// TestFilter_TagCaseInsensitive: #Prod == #prod.
func TestFilter_TagCaseInsensitive(t *testing.T) {
	m := New(tagsFixture(), Options{Mode: "standalone"})
	m.filter.SetValue("#Prod")
	if !m.matches(controller.Status{Name: "x", Tags: []string{"prod"}}) {
		t.Error("#Prod should match the prod tag (case-insensitive)")
	}
}

// TestFilter_PlainQueryIgnoresTags confirms a non-# query never matches via
// tags — the two modes stay distinct so a tag never collides with a name.
func TestFilter_PlainQueryIgnoresTags(t *testing.T) {
	m := New(tagsFixture(), Options{Mode: "standalone"})
	m.filter.SetValue("prod")
	// "prod" is not a substring/subsequence of name/type/endpoint here.
	if m.matches(controller.Status{Name: "x", Type: "local", Local: "1", Remote: "r", Tags: []string{"prod"}}) {
		t.Error("plain query 'prod' must NOT match via tags")
	}
}

// TestEnableAll_NoFilterIsRegressionBound: with no filter, enableAll enables
// every Off tuber — byte-identical to the pre-Phase-46 behaviour. This is the
// regression guard for the m.matches(s) gate.
func TestEnableAll_NoFilterIsRegressionBound(t *testing.T) {
	f := tagsFixture()
	m := New(f, Options{Mode: "standalone"})
	m.enableAll()

	want := []string{"db-prod", "web-prod", "db-stage", "cache"}
	got := append([]string{}, f.enabled...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("no-filter enableAll: got %v, want %v", got, want)
	}
}

// TestEnableAll_TagFilterGates: with a #prod filter, enableAll enables only the
// tagged tubers — turning the filtered view into a group op.
func TestEnableAll_TagFilterGates(t *testing.T) {
	f := tagsFixture()
	m := New(f, Options{Mode: "standalone"})
	m.filter.SetValue("#prod")
	m.enableAll()

	want := []string{"db-prod", "web-prod"}
	got := append([]string{}, f.enabled...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("#prod enableAll: got %v, want %v", got, want)
	}
}

// TestDisableAll_TagFilterGates mirrors the enable case for disableAll.
func TestDisableAll_TagFilterGates(t *testing.T) {
	f := newFake(
		controller.Status{Name: "db-prod", Type: "local", Local: "1", Remote: "r", State: controller.Connected, Tags: []string{"prod"}},
		controller.Status{Name: "web-prod", Type: "local", Local: "2", Remote: "r", State: controller.Connected, Tags: []string{"prod"}},
		controller.Status{Name: "cache", Type: "local", Local: "3", Remote: "r", State: controller.Connected},
	)
	m := New(f, Options{Mode: "standalone"})
	m.filter.SetValue("#prod")
	m.disableAll()

	want := []string{"db-prod", "web-prod"}
	got := append([]string{}, f.disabled...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("#prod disableAll: got %v, want %v", got, want)
	}
}
