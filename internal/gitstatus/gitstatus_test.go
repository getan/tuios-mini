package gitstatus

import "testing"

// Porcelain -z is a wire contract: a NUL inside a path, a rename's old path
// and the branch header are the three places a naive line parser goes wrong.
func TestParsePorcelainStatus(t *testing.T) {
	raw := []byte("## main...origin/main [ahead 2, behind 1]\x00" +
		"MM both.go\x00" +
		"R  new name.go\x00old name.go\x00" +
		"?? untracked.txt\x00" +
		"UU conflict.go\x00" +
		" D deleted.go\x00" +
		"A  added.go\x00")
	st := Parse(raw)
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 2 || st.Behind != 1 || !st.HasUpstream {
		t.Fatalf("branch header = %+v", st)
	}
	if len(st.Staged) != 3 || len(st.Unstaged) != 2 || len(st.Untracked) != 1 || len(st.Conflicts) != 1 {
		t.Fatalf("groups = staged %v unstaged %v untracked %v conflicts %v", st.Staged, st.Unstaged, st.Untracked, st.Conflicts)
	}
	if st.Staged[1].Path != "new name.go" || st.Staged[1].Orig != "old name.go" || st.Staged[1].Letter != Renamed {
		t.Fatalf("rename = %+v", st.Staged[1])
	}
	// MM is both sides: the working-tree half must not vanish into the staged
	// half, or the Git view would offer one diff for two different changes.
	both := false
	for _, e := range st.Unstaged {
		both = both || e.Path == "both.go" && e.Letter == Modified
	}
	if !both {
		t.Fatalf("both.go has no unstaged half: %v", st.Unstaged)
	}
}

func TestDecorationsAggregateFolders(t *testing.T) {
	st := Parse([]byte("?? notes.md\x00 M internal/app/x.go\x00UU internal/db/y.go\x00"))
	d := BuildDecorations(st)
	if l, ok := d.File("internal/app/x.go"); !ok || l != Modified {
		t.Fatalf("file = %v %v", l, ok)
	}
	if l, ok := d.Dir("internal/app"); !ok || l != Modified {
		t.Fatalf("folder = %v %v", l, ok)
	}
	if l, ok := d.Dir("internal"); !ok || l != Conflicted {
		t.Fatalf("conflict must outrank a modification: %v %v", l, ok)
	}
	if _, ok := d.Dir("docs"); ok {
		t.Fatal("an untouched folder was decorated")
	}
}

func TestBranchForms(t *testing.T) {
	for _, tc := range []struct {
		in       string
		branch   string
		upstream string
		ahead    int
	}{
		{"main", "main", "", 0},
		{"feature/x...origin/feature/x [ahead 3]", "feature/x", "origin/feature/x", 3},
		{"HEAD (no branch)", "HEAD", "", 0},
	} {
		b, u, a, _, _ := parseBranch(tc.in)
		if b != tc.branch || u != tc.upstream || a != tc.ahead {
			t.Errorf("%q = %q %q %d", tc.in, b, u, a)
		}
	}
}
