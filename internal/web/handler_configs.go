package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"
)

var moduleTypes = map[string]bool{"alist2strm": true, "ani2alist": true, "libraryposter": true, "alissync": true}

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
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(spec); err != nil {
		return fmt.Errorf("Cron 表达式无效: %w", err)
	}
	if typ != "libraryposter" {
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
	return nil
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
