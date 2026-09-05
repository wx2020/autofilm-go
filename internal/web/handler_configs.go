package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/modules/filemove"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

var moduleTypes = map[string]bool{"alist2strm": true, "ani2alist": true, "libraryposter": true, "alistsync": true, "filemove": true}

func moduleStore(w http.ResponseWriter, r *http.Request) (*storage.Store, string, bool) {
	typ := chi.URLParam(r, "type")
	if !moduleTypes[typ] {
		writeJSONError(w, http.StatusBadRequest, "未知模块类型")
		return nil, "", false
	}
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return nil, "", false
	}
	return store, typ, true
}

func (s *Server) handleListDbConfigs(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	list, err := store.ListModuleConfigs(typ)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveDbConfig(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "配置格式无效")
		return
	}
	if err := validateModuleConfig(typ, cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.SaveModuleConfig(typ, cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

var configIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)

func validateModuleConfig(typ string, cfg map[string]interface{}) error {
	id := fmt.Sprint(cfg["id"])
	if !configIDPattern.MatchString(id) {
		return fmt.Errorf("配置 ID 格式无效")
	}
	spec := fmt.Sprint(cfg["cron"])
	// 使用与调度器、注册表同一份解析器校验，避免“保存成功但排期失败”
	if _, err := SharedCronParser().Parse(spec); err != nil {
		return fmt.Errorf("Cron 表达式无效: %w", err)
	}
	if typ != "libraryposter" && typ != "filemove" {
		rawURL := fmt.Sprint(cfg["url"])
		u, err := url.ParseRequestURI(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("Alist 地址无效")
		}
	}
	pathRequired := func(key string) error {
		value := fmt.Sprint(cfg[key])
		if !strings.HasPrefix(value, "/") {
			return fmt.Errorf("%s 必须以 / 开头", key)
		}
		return nil
	}
	if typ == "alist2strm" {
		if err := pathRequired("source_dir"); err != nil {
			return err
		}
		if strings.TrimSpace(fmt.Sprint(cfg["target_dir"])) == "" {
			return fmt.Errorf("target_dir 不能为空")
		}
	}
	if typ == "ani2alist" {
		return pathRequired("target_dir")
	}
	if typ == "alistsync" || typ == "filemove" {
		// qps_limit 必须是非负整数，0 表示不限流
		if value, exists := cfg["qps_limit"]; exists && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			if parseConfigInt64(value) < 0 {
				return fmt.Errorf("qps_limit 不能为负数")
			}
		}
	}
	if typ == "filemove" {
		backendValue, backendSet := cfg["backend"]
		backend := strings.ToLower(strings.TrimSpace(fmt.Sprint(backendValue)))
		if !backendSet || backendValue == nil || backend == "" || backend == "<nil>" {
			backend = "local"
		}
		if backend != "local" && backend != "openlist" {
			return fmt.Errorf("filemove backend must be local or openlist")
		}
		if backend == "openlist" {
			rawURL := fmt.Sprint(cfg["url"])
			u, err := url.ParseRequestURI(rawURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("OpenList 地址无效")
			}
			for _, key := range []string{"source_dir", "target_dir"} {
				if !strings.HasPrefix(strings.TrimSpace(fmt.Sprint(cfg[key])), "/") {
					return fmt.Errorf("%s 必须以 / 开头", key)
				}
			}
		}
		if strings.TrimSpace(fmt.Sprint(cfg["source_dir"])) == "" {
			return fmt.Errorf("source_dir cannot be empty")
		}
		if strings.TrimSpace(fmt.Sprint(cfg["target_dir"])) == "" {
			return fmt.Errorf("target_dir cannot be empty")
		}
		if source, target := filepath.Clean(fmt.Sprint(cfg["source_dir"])), filepath.Clean(fmt.Sprint(cfg["target_dir"])); source == target {
			return fmt.Errorf("source_dir and target_dir must be different")
		}
		if regex := strings.TrimSpace(fmt.Sprint(cfg["regex"])); regex != "" {
			if _, err := regexp.Compile(regex); err != nil {
				return fmt.Errorf("regex 表达式无效: %w", err)
			}
		}
		if renameRegex := strings.TrimSpace(fmt.Sprint(cfg["rename_regex"])); renameRegex != "" {
			if _, err := regexp.Compile(renameRegex); err != nil {
				return fmt.Errorf("rename_regex must be valid: %w", err)
			}
		}
		for _, key := range []string{"size", "min_size", "max_size"} {
			if value, exists := cfg[key]; exists && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
				if _, err := filemove.ParseSize(value); err != nil {
					return fmt.Errorf("%s must be a non-negative integer (bytes)", key)
				}
			}
		}
		minSize, _ := filemove.ParseSize(cfg["min_size"])
		maxSize, _ := filemove.ParseSize(cfg["max_size"])
		if maxSize > 0 && minSize > maxSize {
			return fmt.Errorf("min_size cannot be greater than max_size")
		}
	}
	return nil
}

func parseConfigInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func (s *Server) handleDeleteDbConfig(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	if err := store.DeleteModuleConfig(typ, chi.URLParam(r, "id")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
