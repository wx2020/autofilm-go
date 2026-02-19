package libraryposter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/sirupsen/logrus"
)

// Config LibraryPoster配置
type Config struct {
	ID                string
	URL               string
	APIKey            string
	TitleFontPath     string
	SubtitleFontPath  string
	Configs           []LibraryConfig
	Cron              string
}

// LibraryConfig 单个媒体库配置
type LibraryConfig struct {
	LibraryName string `json:"library_name"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Limit       int    `json:"limit"`
}

// LibraryPoster 库海报处理器
type LibraryPoster struct {
	config *Config
	logger *logrus.Logger
	client *http.Client
}

// New 创建新的LibraryPoster实例
func New(cfg *Config) (*LibraryPoster, error) {
	lp := &LibraryPoster{
		config: cfg,
		logger: core.GetLogger(),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// 验证字体文件
	if _, err := os.Stat(cfg.TitleFontPath); os.IsNotExist(err) {
		lp.logger.Warnf("主标题字体文件不存在: %s", cfg.TitleFontPath)
	}
	if _, err := os.Stat(cfg.SubtitleFontPath); os.IsNotExist(err) {
		lp.logger.Warnf("副标题字体文件不存在: %s", cfg.SubtitleFontPath)
	}

	return lp, nil
}

// Run 运行LibraryPoster处理
func (lp *LibraryPoster) Run(ctx context.Context) error {
	lp.logger.Info("开始LibraryPoster处理")

	// 获取媒体库列表
	libraries, err := lp.getLibraries(ctx)
	if err != nil {
		return fmt.Errorf("获取媒体库列表失败: %w", err)
	}

	// 创建媒体库名称到ID的映射
	libraryMap := make(map[string]Library)
	for _, lib := range libraries {
		libraryMap[lib.Name] = lib
	}

	// 处理每个配置
	var wg sync.WaitGroup
	for _, cfg := range lp.config.Configs {
		lib, exists := libraryMap[cfg.LibraryName]
		if !exists {
			lp.logger.Warnf("未找到媒体库: %s", cfg.LibraryName)
			continue
		}

		wg.Add(1)
		go func(config LibraryConfig, library Library) {
			defer wg.Done()
			if err := lp.processLibrary(ctx, library, config); err != nil {
				lp.logger.Errorf("处理媒体库 %s 失败: %v", library.Name, err)
			}
		}(cfg, lib)
	}

	wg.Wait()
	lp.logger.Info("LibraryPoster处理完成")
	return nil
}

// Library 媒体库信息
type Library struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// User 用户信息
type User struct {
	ID string `json:"Id"`
}

// Item 媒体项目
type Item struct {
	ID       string `json:"Id"`
	Name     string `json:"Name"`
	IsFolder bool   `json:"IsFolder"`
}

// getUsers 获取用户列表
func (lp *LibraryPoster) getUsers(ctx context.Context) ([]User, error) {
	apiURL := lp.buildURL("/Users", map[string]string{"api_key": lp.config.APIKey})

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := lp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取用户列表失败，状态码: %d", resp.StatusCode)
	}

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}

	return users, nil
}

// getLibraries 获取媒体库列表
func (lp *LibraryPoster) getLibraries(ctx context.Context) ([]Library, error) {
	apiURL := lp.buildURL("/Library/MediaFolders", map[string]string{"api_key": lp.config.APIKey})

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := lp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取媒体库列表失败，状态码: %d", resp.StatusCode)
	}

	var result struct {
		Items []Library `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Items, nil
}

// getLibraryItems 获取媒体库项目
func (lp *LibraryPoster) getLibraryItems(ctx context.Context, libraryID string, limit int) ([]Item, error) {
	// 获取用户
	users, err := lp.getUsers(ctx)
	if err != nil || len(users) == 0 {
		return nil, fmt.Errorf("获取用户失败")
	}
	userID := users[0].ID

	params := map[string]string{
		"api_key":  lp.config.APIKey,
		"ParentId": libraryID,
		"Limit":    fmt.Sprintf("%d", limit),
	}

	apiURL := lp.buildURL(fmt.Sprintf("/Users/%s/Items", userID), params)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := lp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取媒体库项目失败，状态码: %d", resp.StatusCode)
	}

	var result struct {
		Items []Item `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Items, nil
}

// downloadItemImage 下载项目图片
func (lp *LibraryPoster) downloadItemImage(ctx context.Context, itemID, imageType string) ([]byte, error) {
	params := map[string]string{
		"api_key": lp.config.APIKey,
	}

	apiURL := lp.buildURL(fmt.Sprintf("/Items/%s/Images/%s", itemID, imageType), params)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := lp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败，状态码: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// processLibrary 处理单个媒体库
func (lp *LibraryPoster) processLibrary(ctx context.Context, library Library, config LibraryConfig) error {
	lp.logger.Infof("开始处理媒体库: %s", library.Name)

	limit := config.Limit
	if limit <= 0 {
		limit = 15
	}

	// 获取媒体库项目
	items, err := lp.getLibraryItems(ctx, library.ID, limit)
	if err != nil {
		return err
	}

	lp.logger.Infof("获取到 %s 的 %d 个项目", library.Name, len(items))

	// 下载图片
	var images [][]byte
	for _, item := range items {
		imageData, err := lp.downloadItemImage(ctx, item.ID, "Primary")
		if err != nil {
			lp.logger.Warnf("下载 %s 的图片失败: %v", item.Name, err)
			continue
		}
		images = append(images, imageData)

		if len(images) >= limit {
			break
		}
	}

	if len(images) == 0 {
		return fmt.Errorf("没有下载到任何图片")
	}

	lp.logger.Infof("获取到 %d 张海报图片", len(images))

	// 处理海报（简化版：使用第一张图片作为封面）
	// 完整的海报拼接功能需要使用更复杂的图像处理库
	posterData := images[0]

	// 更新媒体库图片
	if err := lp.updateLibraryImage(ctx, library, posterData); err != nil {
		return err
	}

	lp.logger.Infof("媒体库 %s 的海报更新成功", library.Name)
	return nil
}

// updateLibraryImage 更新媒体库图片
func (lp *LibraryPoster) updateLibraryImage(ctx context.Context, library Library, imageData []byte) error {
	params := map[string]string{
		"api_key": lp.config.APIKey,
	}

	apiURL := lp.buildURL(fmt.Sprintf("/Items/%s/Images/Primary", library.ID), params)

	// 编码为base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(base64Data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "image/png")

	resp, err := lp.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("更新图片失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// buildURL 构建API URL
func (lp *LibraryPoster) buildURL(path string, params map[string]string) string {
	u := lp.config.URL + path
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		u += "?" + values.Encode()
	}
	return u
}
