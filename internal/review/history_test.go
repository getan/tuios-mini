package review

import "testing"

func TestRemoteRowsKeepOneAddressPerRemote(t *testing.T) {
	out := "origin\tgit@github.com:getan/tuios-mini.git (fetch)\n" +
		"origin\tgit@github.com:getan/tuios-mini.git (push)\n" +
		"upstream\thttps://github.com/Gaurav-Gosain/tuios.git (fetch)\n" +
		"upstream\thttps://github.com/Gaurav-Gosain/tuios.git (push)\n"
	rows := remoteRows(out, 100)
	want := []string{
		"origin  git@github.com:getan/tuios-mini.git",
		"upstream  https://github.com/Gaurav-Gosain/tuios.git",
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %#v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestRemoteRowsKeepADifferentPushAddress(t *testing.T) {
	out := "origin\tgit@github.com:getan/repo.git (fetch)\n" +
		"origin\tgit@github.com:someone/repo.git (push)\n"
	rows := remoteRows(out, 100)
	if len(rows) != 2 || rows[1] != "origin  git@github.com:someone/repo.git (push)" {
		t.Fatalf("rows = %#v", rows)
	}
}
