package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/akimio/autofilm/internal/core"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// handleAddModule POST /api/config/module
// 添加或更新一个模块配置条目到 config.yaml
func (s *Server) handleAddModule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type   string                 `json:"type"`
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"请求体解析失败"}`, http.StatusBadRequest)
		return
	}
	if body.Type == "" || body.Config == nil {
		http.Error(w, `{"error":"缺少 type 或 config"}`, http.StatusBadRequest)
		return
	}

	cfgFile := core.Store().GetConfigFile()
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		http.Error(w, `{"error":"读取配置失败"}`, http.StatusInternalServerError)
		return
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		http.Error(w, `{"error":"解析 YAML 失败"}`, http.StatusInternalServerError)
		return
	}

	root := &doc
	if len(root.Content) == 0 {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.MappingNode})
	}
	mapping := root.Content[0]

	listKey := moduleTypeToListKey(body.Type)
	if listKey == "" {
		http.Error(w, `{"error":"未知模块类型"}`, http.StatusBadRequest)
		return
	}

	// 找到或创建列表节点
	var listNode *yaml.Node
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == listKey {
			listNode = mapping.Content[i+1]
			break
		}
	}
	if listNode == nil {
		listNode = &yaml.Node{Kind: yaml.SequenceNode}
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: listKey}, listNode)
	}

	id, _ := body.Config["id"].(string)

	// 如果已存在同名 id，替换；否则追加
	replaced := false
	for _, item := range listNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j < len(item.Content)-1; j += 2 {
			if item.Content[j].Value == "id" && item.Content[j+1].Value == id {
				// 替换整个 mapping
				newNode := mapToYamlNode(body.Config)
				*item = *newNode
				replaced = true
				break
			}
		}
		if replaced {
			break
		}
	}
	if !replaced {
		listNode.Content = append(listNode.Content, mapToYamlNode(body.Config))
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		http.Error(w, `{"error":"序列化 YAML 失败"}`, http.StatusInternalServerError)
		return
	}

	tmpPath := cfgFile + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0644); err != nil {
		http.Error(w, `{"error":"写入配置失败"}`, http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, cfgFile); err != nil {
		http.Error(w, `{"error":"替换配置失败"}`, http.StatusInternalServerError)
		return
	}

	// 触发热重载
	if err := core.Store().Reload(); err != nil {
		s.logger.Warnf("重载配置失败: %v", err)
	}
	core.TriggerReload()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "模块配置已添加/更新，定时任务已重建",
	})
}

// handleDeleteModule DELETE /api/config/module/{type}/{id}
// 从 config.yaml 中删除指定模块
func (s *Server) handleDeleteModule(w http.ResponseWriter, r *http.Request) {
	moduleType := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	if moduleType == "" || id == "" {
		http.Error(w, `{"error":"缺少 type 或 id"}`, http.StatusBadRequest)
		return
	}

	cfgFile := core.Store().GetConfigFile()
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		http.Error(w, `{"error":"读取配置失败"}`, http.StatusInternalServerError)
		return
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		http.Error(w, `{"error":"解析 YAML 失败"}`, http.StatusInternalServerError)
		return
	}

	mapping := doc.Content[0]
	listKey := moduleTypeToListKey(moduleType)

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == listKey {
			listNode := mapping.Content[i+1]
			for j := 0; j < len(listNode.Content); j++ {
				item := listNode.Content[j]
				if item.Kind != yaml.MappingNode {
					continue
				}
				for k := 0; k < len(item.Content)-1; k += 2 {
					if item.Content[k].Value == "id" && item.Content[k+1].Value == id {
						listNode.Content = append(listNode.Content[:j], listNode.Content[j+1:]...)
						goto SAVED
					}
				}
			}
		}
	}

	http.Error(w, `{"error":"模块未找到"}`, http.StatusNotFound)
	return

SAVED:
	out, _ := yaml.Marshal(&doc)
	tmpPath := cfgFile + ".tmp"
	os.WriteFile(tmpPath, out, 0644)
	os.Rename(tmpPath, cfgFile)

	core.Store().Reload()
	core.TriggerReload()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "模块已删除",
	})
}

func moduleTypeToListKey(t string) string {
	switch t {
	case "alist2strm":
		return "Alist2StrmList"
	case "ani2alist":
		return "Ani2AlistList"
	case "alissync":
		return "AlistSyncList"
	case "libraryposter":
		return "LibraryPosterList"
	}
	return ""
}

func mapToYamlNode(m map[string]interface{}) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for k, v := range m {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k})
		n.Content = append(n.Content, valueToYamlNode(v))
	}
	return n
}

func valueToYamlNode(v interface{}) *yaml.Node {
	switch val := v.(type) {
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val}
	case bool:
		if val {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "false", Tag: "!!bool"}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", val), Tag: "!!int"}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%g", val), Tag: "!!float"}
	case map[string]interface{}:
		return mapToYamlNode(val)
	case []interface{}:
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range val {
			seq.Content = append(seq.Content, valueToYamlNode(item))
		}
		return seq
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: ""}
	}
}
