package rule

import (
	"reflect"
	"testing"
)

var (
	repoAPI   = Entry{Path: "internal/api/**/*.go", Rule: "api", Source: SourceRepo}
	dashGo    = Entry{Path: "**/*.go", Rule: "dash", Source: DashboardSource("base")}
	builtinGo = Entry{Path: "*.go", Rule: "builtin", Source: SourceBuiltIn, Name: "go"}
	builtinTF = Entry{Path: "*.tf", Rule: "tf", Source: SourceBuiltIn, Name: "tf"}
)

func TestResolveFirstMatchAcrossLayers(t *testing.T) {
	r := newResolver([]Entry{repoAPI, dashGo}, []Entry{builtinGo, builtinTF})
	tests := []struct {
		path string
		want Entry
		ok   bool
	}{
		{"internal/api/user.go", repoAPI, true},
		{"internal/store/x.go", dashGo, true},
		{"main.tf", builtinTF, true},
		{"README.md", Entry{}, false},
	}
	for _, tt := range tests {
		got, ok := r.Resolve(tt.path)
		if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Resolve(%q) = %+v, %v; want %+v, %v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
	if got := r.Entries(); got != 4 {
		t.Errorf("Entries() = %d, want 4", got)
	}

	off := newResolver([]Entry{repoAPI}, nil)
	if _, ok := off.Resolve("main.tf"); ok {
		t.Error("with no built-ins, main.tf got a rule")
	}
}

func TestNewResolverBuiltInSwitch(t *testing.T) {
	on := NewResolver([]Entry{repoAPI}, true)
	if !on.BuiltIn() || on.Entries() != 1+len(Builtin()) {
		t.Errorf("on: BuiltIn() = %v, Entries() = %d", on.BuiltIn(), on.Entries())
	}
	off := NewResolver([]Entry{repoAPI}, false)
	if off.BuiltIn() || off.Entries() != 1 {
		t.Errorf("off: BuiltIn() = %v, Entries() = %d", off.BuiltIn(), off.Entries())
	}
}

func TestResolveOffEndsSearch(t *testing.T) {
	r := newResolver([]Entry{{Path: "**/*.pb.go", Off: true, Source: SourceRepo}}, []Entry{builtinGo})
	if got, ok := r.Resolve("a/b.pb.go"); ok || !reflect.DeepEqual(got, Entry{}) {
		t.Errorf("Resolve = %+v, %v; want no rule", got, ok)
	}
	if _, ok := r.Resolve("a/b.go"); !ok {
		t.Error("a/b.go lost its built-in rule")
	}
}

func TestResolveNil(t *testing.T) {
	var r *Resolver
	if _, ok := r.Resolve("a.go"); ok {
		t.Error("a nil resolver gave a rule")
	}
	if r.Entries() != 0 || r.BuiltIn() {
		t.Error("a nil resolver has entries or built-ins")
	}
}

func TestLabelsAndHeadings(t *testing.T) {
	tests := []struct {
		e              Entry
		label, heading string
	}{
		{repoAPI, "repo:internal/api/**/*.go", "repo · internal/api/**/*.go"},
		{dashGo, "dashboard:base:**/*.go", "dashboard:base · **/*.go"},
		{builtinGo, "built-in:go", "built-in:go"},
	}
	for _, tt := range tests {
		if got := tt.e.Label(); got != tt.label {
			t.Errorf("Label() = %q, want %q", got, tt.label)
		}
		if got := tt.e.Heading(); got != tt.heading {
			t.Errorf("Heading() = %q, want %q", got, tt.heading)
		}
	}
}

func TestHashStable(t *testing.T) {
	list := []Entry{repoAPI, dashGo, {Path: "**/*.pb.go", Off: true, Source: SourceRepo}}
	h := Hash(list)
	if len(h) != 64 || Hash(list) != h {
		t.Fatalf("Hash = %q, not a stable hex SHA-256", h)
	}
	change := func(f func([]Entry)) string {
		l := append([]Entry(nil), list...)
		f(l)
		return Hash(l)
	}
	for name, got := range map[string]string{
		"rule":   change(func(l []Entry) { l[0].Rule = "other" }),
		"path":   change(func(l []Entry) { l[0].Path = "other/**" }),
		"off":    change(func(l []Entry) { l[2].Off = false }),
		"source": change(func(l []Entry) { l[1].Source = DashboardSource("other") }),
		"order":  change(func(l []Entry) { l[0], l[1] = l[1], l[0] }),
	} {
		if got == h {
			t.Errorf("changing the %s did not change the hash", name)
		}
	}
	if Hash(nil) != Hash([]Entry{}) {
		t.Error("Hash(nil) != Hash([]Entry{})")
	}
}

// Hides is a heuristic: it may miss an overlap, but it never reports one that
// cannot happen.
func TestHides(t *testing.T) {
	builtins := []Entry{builtinGo, builtinTF}
	tests := []struct {
		path string
		want []string
	}{
		{"**", []string{"go", "tf"}},
		{"internal/**/*.go", []string{"go"}},
		{"**/*.pb.go", []string{"go"}},
		{"*.ts", nil},
		{"docs/**", []string{"go", "tf"}},
		{"docs/*.md", nil},
	}
	for _, tt := range tests {
		got := Hides(Entry{Path: tt.path, Rule: "x", Source: SourceRepo}, builtins)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Hides(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
	if got := Hides(Entry{Path: "**/*.pb.go", Rule: "x"}, []Entry{{Path: "**/*.go", Name: "go"}}); !reflect.DeepEqual(got, []string{"go"}) {
		t.Errorf("Hides(**/*.pb.go, **/*.go) = %v", got)
	}
	if got := Hides(Entry{Path: "[ab].go", Rule: "x"}, []Entry{{Path: "a.go", Name: "a"}}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("Hides with a class = %v", got)
	}
}

func TestNewResolverWith(t *testing.T) {
	chosen, _ := SelectBuiltin([]string{"yaml", "github_workflows"})
	r := NewResolverWith([]Entry{repoAPI}, chosen)
	if !r.BuiltIn() || r.Entries() != 1+len(chosen) {
		t.Errorf("BuiltIn() = %v, Entries() = %d, want true and %d", r.BuiltIn(), r.Entries(), 1+len(chosen))
	}
	if e, ok := r.Resolve(".github/workflows/ci.yml"); !ok || e.Name != "github_workflows" {
		t.Errorf("a workflow got %+v, %v; want built-in:github_workflows", e, ok)
	}
	if _, ok := r.Resolve("main.go"); ok {
		t.Error("main.go got a rule with go not chosen")
	}
	if e, ok := r.Resolve("internal/api/x.go"); !ok || e != repoAPI {
		t.Errorf("the team entry lost: %+v, %v", e, ok)
	}
	none := NewResolverWith([]Entry{repoAPI}, nil)
	if none.BuiltIn() || none.Entries() != 1 {
		t.Errorf("no built-ins: BuiltIn() = %v, Entries() = %d", none.BuiltIn(), none.Entries())
	}
}
