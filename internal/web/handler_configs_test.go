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

func TestValidateFileMoveConfig(t *testing.T) {
	valid := map[string]interface{}{
		"id": "local-move", "cron": "0 0 * * * *",
		"source_dir": `C:\downloads`, "target_dir": `D:\media`,
		"regex": `(?i)\.(mkv|mp4)$`, "min_size": float64(1024),
	}
	if err := validateModuleConfig("filemove", valid); err != nil {
		t.Fatal(err)
	}
	valid["min_size"] = float64(1073741824)
	if err := validateModuleConfig("filemove", valid); err != nil {
		t.Fatalf("large byte size rejected: %v", err)
	}
	valid["min_size"] = "1GB"
	valid["max_size"] = "2GB"
	if err := validateModuleConfig("filemove", valid); err != nil {
		t.Fatalf("human-readable byte size rejected: %v", err)
	}

	invalidRegex := map[string]interface{}{
		"id": "local-move", "cron": "0 0 * * * *",
		"source_dir": `C:\downloads`, "target_dir": `D:\media`, "regex": "[",
	}
	if err := validateModuleConfig("filemove", invalidRegex); err == nil {
		t.Fatal("invalid regex accepted")
	}

	invalidSize := map[string]interface{}{
		"id": "local-move", "cron": "0 0 * * * *",
		"source_dir": `C:\downloads`, "target_dir": `D:\media`, "min_size": -1,
	}
	if err := validateModuleConfig("filemove", invalidSize); err == nil {
		t.Fatal("negative size accepted")
	}
}
