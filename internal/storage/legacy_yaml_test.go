package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportLegacyYAMLOnce(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("Alist2StrmList:\n  - id: demo\n    url: http://localhost:5244\n    cron: '0 0 * * * *'\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	n, err := ImportLegacyYAML(s, path)
	if err != nil || n != 1 {
		t.Fatalf("import = %d, %v", n, err)
	}
	n, err = ImportLegacyYAML(s, path)
	if err != nil || n != 0 {
		t.Fatalf("second import = %d, %v", n, err)
	}
}
