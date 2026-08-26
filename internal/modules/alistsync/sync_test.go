package alistsync

import "testing"

func TestReplacePrefix(t *testing.T) {
	cases := []struct {
		path, oldPrefix, newPrefix, want string
	}{
		{"/movies/a/b.mkv", "/movies", "/backup", "/backup/a/b.mkv"},
		{"/movies2/a.mkv", "/movies", "/backup", "/movies2/a.mkv"},
		{"/movies", "/movies", "/backup", "/backup"},
		{"/x/y.mkv", "/", "/backup", "/backup/x/y.mkv"},
		{"/movies/a.mkv", "/movies/", "/dst", "/dst/a.mkv"},
		{"/other/a.mkv", "/movies", "/backup", "/other/a.mkv"},
	}
	for _, c := range cases {
		if got := replacePrefix(c.path, c.oldPrefix, c.newPrefix); got != c.want {
			t.Errorf("replacePrefix(%q,%q,%q) = %q, want %q", c.path, c.oldPrefix, c.newPrefix, got, c.want)
		}
	}
}
