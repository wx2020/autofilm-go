package alist2strm

import "testing"

func TestDiffSnapshots(t *testing.T) {
	old := &Snapshot{Files: map[string]FileEntry{
		"/same.mkv":    {Size: 1, Modified: "a"},
		"/changed.mkv": {Size: 1, Modified: "a"},
		"/deleted.mkv": {Size: 1, Modified: "a"},
	}}
	next := &Snapshot{Files: map[string]FileEntry{
		"/same.mkv":    {Size: 1, Modified: "a"},
		"/changed.mkv": {Size: 2, Modified: "a"},
		"/added.mkv":   {Size: 1, Modified: "a"},
	}}
	added, changed, deleted := DiffSnapshots(old, next)
	if len(added) != 1 || added[0] != "/added.mkv" {
		t.Fatalf("added=%v", added)
	}
	if len(changed) != 1 || changed[0] != "/changed.mkv" {
		t.Fatalf("changed=%v", changed)
	}
	if len(deleted) != 1 || deleted[0] != "/deleted.mkv" {
		t.Fatalf("deleted=%v", deleted)
	}
}
