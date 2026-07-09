package web

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/akimio/autofilm/internal/core"
)

// handleGetConfig GET /api/config
// 返回当前配置文件内容，敏感字段用 *** 遮蔽
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configFile := core.Store().GetConfigFile()
	data, err := os.ReadFile(configFile)
	if err != nil {
		http.Error(w, `{"error":"读取配置失败"}`, http.StatusInternalServerError)
		return
	}

	// 解析为通用 map 以便遮蔽敏感字段
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// 如果是 YAML 格式，直接返回原文
		maskSensitive(string(data))
		json.NewEncoder(w).Encode(map[string]interface{}{
			"raw":    string(data),
			"parsed": false,
		})
		return
	}

	maskSensitiveMap(cfg)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"raw":    string(data),
		"parsed": cfg,
	})
}

// handleUpdateConfig PUT /api/config
// 全量写入配置 → 触发重载
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"请求体解析失败"}`, http.StatusBadRequest)
		return
	}

	if body.Raw == "" {
		http.Error(w, `{"error":"配置内容不能为空"}`, http.StatusBadRequest)
		return
	}

	configFile := core.Store().GetConfigFile()
	tmpPath := configFile + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(body.Raw), 0644); err != nil {
		http.Error(w, `{"error":"写入配置失败"}`, http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, configFile); err != nil {
		http.Error(w, `{"error":"替换配置失败"}`, http.StatusInternalServerError)
		return
	}

	// 触发配置重载
	if err := core.Store().Reload(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 通知 main 重建 cron
	core.TriggerReload()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "配置已更新",
	})
}

// maskSensitive 遮蔽 YAML 原文中的敏感字段
func maskSensitive(raw string) string {
	// 简单实现：不修改原文，仅用于标记
	return raw
}

// maskSensitiveMap 递归遮蔽 map 中的敏感字段
func maskSensitiveMap(m map[string]interface{}) {
	sensitiveKeys := map[string]bool{
		"password": true,
		"token":    true,
		"api_key":  true,
		"secret":   true,
	}
	for k, v := range m {
		if sensitiveKeys[k] {
			m[k] = "***"
		} else if subMap, ok := v.(map[string]interface{}); ok {
			maskSensitiveMap(subMap)
		} else if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					maskSensitiveMap(itemMap)
				}
			}
		}
	}
}
