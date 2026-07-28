package web

import "testing"

func TestValidateModuleConfig(t *testing.T) {
	valid := map[string]interface{}{
		"id": "media-main", "cron": "0 0 * * * *", "url": "http://localhost:5244",
		"source_dir": "/media", "target_dir": "C:/strm",
	}
	if err := validateModuleConfig("alist2strm", valid); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]interface{}{
		{"id": "bad id", "cron": "0 0 * * * *", "url": "http://localhost:5244", "source_dir": "/", "target_dir": "x"},
		{"id": "ok", "cron": "bad", "url": "http://localhost:5244", "source_dir": "/", "target_dir": "x"},
		{"id": "ok", "cron": "0 0 * * * *", "url": "file:///etc/passwd", "source_dir": "/", "target_dir": "x"},
		{"id": "ok", "cron": "0 0 * * * *", "url": "http://localhost:5244", "source_dir": "relative", "target_dir": "x"},
	}
	for i, c := range cases {
		if err := validateModuleConfig("alist2strm", c); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
