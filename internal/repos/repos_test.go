package repos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHEAD(t *testing.T) {
	cases := map[string]string{
		"ref: refs/heads/main":                     "main",
		"ref: refs/heads/wf/1-fix":                 "wf/1-fix",
		"9972eef1234567890abcdef1234567890abcdef1": "9972eef1 (detached)",
		"": "",
	}
	for in, want := range cases {
		if got := ParseHEAD(in); got != want {
			t.Errorf("ParseHEAD(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscoverAndFilter(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, branch string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"),
			[]byte("ref: refs/heads/"+branch), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	api := mk("work/acme/api-server", "develop")
	mk("hobby/dotfiles", "main")
	mk("work/acme/api-server/vendor/dep", "x") // inside a repo: must not appear
	nm := filepath.Join(root, "node_modules", "pkg")
	_ = os.MkdirAll(filepath.Join(nm, ".git"), 0o755) // ignored dir

	all := Discover([]string{root}, []string{api})
	if len(all) != 2 {
		t.Fatalf("Discover found %d repos, want 2: %+v", len(all), all)
	}
	if !all[0].Known || all[0].Path != api || all[0].Branch != "develop" {
		t.Errorf("known repo should be first with branch: %+v", all[0])
	}

	ms := Filter(all, "apisrv")
	if len(ms) != 1 || ms[0].Path != api {
		t.Fatalf("fuzzy filter failed: %+v", ms)
	}
	if len(ms[0].Highlights) == 0 {
		t.Error("no highlight positions")
	}
	if got := Filter(all, ""); len(got) != 2 {
		t.Errorf("empty query should return all, got %d", len(got))
	}
}
