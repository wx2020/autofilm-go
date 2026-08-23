package storage

import (
	"database/sql"
	"os"

	"gopkg.in/yaml.v3"
)

// ImportLegacyYAML imports an existing config.yaml once when the SQLite module
// configuration table is empty. The file is never written and is ignored after import.
func ImportLegacyYAML(s *Store, path string) (int, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM module_configs").Scan(&count); err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return 0, err
	}
	keys := map[string]string{"Alist2StrmList": "alist2strm", "Ani2AlistList": "ani2alist", "LibraryPosterList": "libraryposter", "AlistSyncList": "alissync", "FileMoveList": "filemove"}
	imported := 0
	for key, typ := range keys {
		items, _ := root[key].([]interface{})
		for _, item := range items {
			cfg, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if err := s.SaveModuleConfig(typ, cfg); err != nil {
				return imported, err
			}
			imported++
		}
	}
	return imported, nil
}
