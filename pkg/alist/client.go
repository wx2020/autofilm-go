package alist

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/httpclient"
	"github.com/sirupsen/logrus"
)

var (
	clients   = make(map[string]*AlistClient)
	clientsMu sync.RWMutex
)

// AlistClient Alist客户端
type AlistClient struct {
	url        string
	username   string
	password   string
	token      string
	basePath   string
	id         int
	httpClient *httpclient.HTTPClient
	logger     *logrus.Logger
	tokenMu    sync.RWMutex
	tokenExp   int64
}

// AlistPath Alist文件路径信息
type AlistPath struct {
	ServerURL string `json:"-"`
	BasePath  string `json:"-"`
	FullPath  string `json:"full_path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Type      int    `json:"type"` // 1: 文件夹, 0: 文件
	Modified  string `json:"modified"`
	RawURL    string `json:"raw_url,omitempty"`
	Thumb     string `json:"thumb,omitempty"`
}

// IsDir 判断是否为目录
func (p *AlistPath) IsDir() bool {
	return p.Type == 1
}

// Suffix 获取文件后缀
func (p *AlistPath) Suffix() string {
	for i := len(p.Name) - 1; i >= 0; i-- {
		if p.Name[i] == '.' {
			return p.Name[i:]
		}
	}
	return ""
}

// ModifiedTimestamp 获取修改时间戳
func (p *AlistPath) ModifiedTimestamp() int64 {
	layout := "2006-01-02T15:04:05.000000Z"
	t, err := time.Parse(layout, p.Modified)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// AlistStorage Alist存储信息
type AlistStorage struct {
	ID              int    `json:"id"`
	MountPath       string `json:"mount_path"`
	Order           int    `json:"order"`
	Remark          string `json:"remark"`
	Driver          string `json:"driver"`
	CacheExpiration int    `json:"cache_expiration"`
	Status          string `json:"status"`
	Addition        string `json:"addition"`
	Modified        string `json:"modified"`
	Disabled        bool   `json:"disabled"`
	EnableSign      bool   `json:"enable_sign"`
	OrderBy         string `json:"order_by"`
	OrderDirection  string `json:"order_direction"`
	ExtractFolder   string `json:"extract_folder"`
	WebProxy        bool   `json:"web_proxy"`
	WebdavPolicy    string `json:"webdav_policy"`
	DownProxyURL    string `json:"down_proxy_url"`
}

// Addition2dict 将Addition JSON字符串转换为字典
func (s *AlistStorage) Addition2dict() (map[string]interface{}, error) {
	if s.Addition == "" {
		return make(map[string]interface{}), nil
	}

	var result map[string]interface{}
	err := json.Unmarshal([]byte(s.Addition), &result)
	return result, err
}

// SetAdditionByDict 将字典设置到Addition
func (s *AlistStorage) SetAdditionByDict(data map[string]interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.Addition = string(bytes)
	return nil
}

// APIResponse Alist API响应
type APIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// FSListResponse 文件列表响应
type FSListResponse struct {
	Total   int         `json:"total"`
	Content []AlistPath `json:"content"`
}

// GetClient 获取Alist客户端（支持多实例）
func GetClient(url, username, password, token string) (*AlistClient, error) {
	// 确保url有协议前缀
	if url != "" && !startsWith(url, "http://") && !startsWith(url, "https://") {
		url = "https://" + url
	}
	url = trimRight(url, "/")

	clientsMu.Lock()
	defer clientsMu.Unlock()

	key := url + ":" + username
	if client, exists := clients[key]; exists {
		return client, nil
	}

	client, err := newAlistClient(url, username, password, token)
	if err != nil {
		return nil, err
	}

	clients[key] = client
	return client, nil
}

func newAlistClient(url, username, password, token string) (*AlistClient, error) {
	if (username == "" || password == "") && token == "" {
		return nil, fmt.Errorf("用户名及密码为空或令牌Token为空")
	}

	client := &AlistClient{
		url:        url,
		username:   username,
		password:   password,
		token:      token,
		httpClient: httpclient.GetClient(),
		logger:     core.GetLogger(),
	}

	if token != "" {
		client.tokenExp = -1 // 永不过期
	}

	// 获取用户信息
	if err := client.syncMe(context.Background()); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *AlistClient) getToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	if c.tokenExp == -1 {
		c.logger.Debug("使用永久令牌")
		return c.token
	}

	c.logger.Debug("使用临时令牌")
	now := time.Now().Unix()

	if c.tokenExp < now {
		// 令牌过期，需要重新获取
		c.tokenMu.RUnlock()
		c.tokenMu.Lock()

		// 双重检查
		if c.tokenExp < now {
			c.logger.Debug("令牌已过期，重新获取")
			newToken, err := c.authLogin(context.Background())
			if err != nil {
				c.tokenMu.Unlock()
				c.logger.Errorf("重新获取令牌失败: %v", err)
				return ""
			}
			c.token = newToken
			// 令牌有效期2天，提前5分钟刷新
			c.tokenExp = now + 2*24*60*60 - 5*60
		}

		c.tokenMu.Unlock()
		c.tokenMu.RLock()
	}

	return c.token
}

func (c *AlistClient) makeHeaders() map[string]string {
	return map[string]string{
		"Authorization": c.getToken(),
		"Content-Type":  "application/json",
	}
}

func (c *AlistClient) doRequest(ctx context.Context, method, endpoint string, jsonData []byte) (*APIResponse, error) {
	url := c.url + endpoint
	headers := c.makeHeaders()

	var resp *httpclient.Response
	var err error

	switch method {
	case "GET":
		resp, err = c.httpClient.Get(ctx, url, headers)
	case "POST":
		resp, err = c.httpClient.Post(ctx, url, headers, jsonData)
	case "PUT":
		resp, err = c.httpClient.Put(ctx, url, headers, jsonData)
	default:
		return nil, fmt.Errorf("不支持的HTTP方法: %s", method)
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API错误: %s", apiResp.Message)
	}

	return &apiResp, nil
}

// authLogin 登录获取令牌
func (c *AlistClient) authLogin(ctx context.Context) (string, error) {
	type LoginRequest struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	req := LoginRequest{
		Username: c.username,
		Password: c.password,
	}

	jsonData, _ := json.Marshal(req)
	resp, err := c.doRequest(ctx, "POST", "/api/auth/login", jsonData)
	if err != nil {
		return "", fmt.Errorf("登录失败: %w", err)
	}

	type LoginData struct {
		Token string `json:"token"`
	}
	var data LoginData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", fmt.Errorf("解析令牌失败: %w", err)
	}

	c.logger.Debugf("%s 更新令牌成功", c.username)
	return data.Token, nil
}

// syncMe 同步用户信息
func (c *AlistClient) syncMe(ctx context.Context) error {
	resp, err := c.doRequest(ctx, "GET", "/api/me", nil)
	if err != nil {
		return fmt.Errorf("获取用户信息失败: %w", err)
	}

	type MeData struct {
		BasePath string `json:"base_path"`
		ID       int    `json:"id"`
	}
	var data MeData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("解析用户信息失败: %w", err)
	}

	c.basePath = data.BasePath
	c.id = data.ID
	return nil
}

// FSList 获取文件列表
func (c *AlistClient) FSList(ctx context.Context, dirPath string) ([]AlistPath, error) {
	type ListRequest struct {
		Path     string `json:"path"`
		Password string `json:"password"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
		Refresh  bool   `json:"refresh"`
	}

	req := ListRequest{
		Path:     dirPath,
		Password: "",
		Page:     1,
		PerPage:  0, // 0 表示不分页，返回所有
		Refresh:  false,
	}

	jsonData, _ := json.Marshal(req)
	resp, err := c.doRequest(ctx, "POST", "/api/fs/list", jsonData)
	if err != nil {
		return nil, err
	}

	var result FSListResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}

	// 填充完整路径和下载链接
	for i := range result.Content {
		result.Content[i].ServerURL = c.url
		result.Content[i].BasePath = c.basePath
		result.Content[i].FullPath = dirPath + "/" + result.Content[i].Name

		// 获取文件的下载链接
		if !result.Content[i].IsDir() {
			fullPath := result.Content[i].FullPath
			c.logger.Debugf("[DEBUG] 正在获取文件下载链接: %s", fullPath)
			if fileDetail, err := c.FSGet(ctx, fullPath); err == nil && fileDetail != nil {
				result.Content[i].RawURL = fileDetail.RawURL
				result.Content[i].RawURL = fileDetail.RawURL
				c.logger.Debugf("[DEBUG] 文件: %s", result.Content[i].Name)
				c.logger.Debugf("[DEBUG]   RawURL: %s", result.Content[i].RawURL)
			} else {
				c.logger.Warnf("[WARN] 获取文件下载链接失败: %s, 错误: %v", fullPath, err)
			}
		}
	}

	return result.Content, nil
}

// FSGet 获取文件/目录详细信息
func (c *AlistClient) FSGet(ctx context.Context, path string) (*AlistPath, error) {
	type GetRequest struct {
		Path     string `json:"path"`
		Password string `json:"password"`
	}

	req := GetRequest{
		Path:     path,
		Password: "",
	}

	jsonData, _ := json.Marshal(req)
	c.logger.Debugf("[DEBUG] FSGet 请求路径: %s", path)
	c.logger.Debugf("[DEBUG] FSGet 请求数据: %s", string(jsonData))

	resp, err := c.doRequest(ctx, "POST", "/api/fs/get", jsonData)
	if err != nil {
		c.logger.Errorf("[ERROR] FSGet 请求失败: %v", err)
		return nil, err
	}

	c.logger.Debugf("[DEBUG] FSGet 响应码: %d", resp.Code)
	c.logger.Debugf("[DEBUG] FSGet 原始响应: %s", string(resp.Data))

	var result AlistPath
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		c.logger.Errorf("[ERROR] FSGet JSON 解析失败: %v, 原始数据: %s", err, string(resp.Data))
		return nil, err
	}

	result.ServerURL = c.url
	result.BasePath = c.basePath
	result.FullPath = path

	c.logger.Debugf("[DEBUG] FSGet 解析后 - RawURL: '%s'", result.RawURL)

	return &result, nil
}

// AdminStorageList 列出存储列表（需要管理员权限）
func (c *AlistClient) AdminStorageList(ctx context.Context) ([]AlistStorage, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/admin/storage/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Content []AlistStorage `json:"content"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}

	return result.Content, nil
}

// AdminStorageCreate 创建存储（需要管理员权限）
func (c *AlistClient) AdminStorageCreate(ctx context.Context, storage *AlistStorage) error {
	jsonData, _ := json.Marshal(storage)
	_, err := c.doRequest(ctx, "POST", "/api/admin/storage/create", jsonData)
	return err
}

// AdminStorageUpdate 更新存储（需要管理员权限）
func (c *AlistClient) AdminStorageUpdate(ctx context.Context, storage *AlistStorage) error {
	jsonData, _ := json.Marshal(storage)
	_, err := c.doRequest(ctx, "POST", "/api/admin/storage/update", jsonData)
	return err
}

// GetStorageByMountPath 通过挂载路径获取存储器
func (c *AlistClient) GetStorageByMountPath(ctx context.Context, mountPath string, create bool, driver string) (*AlistStorage, error) {
	storages, err := c.AdminStorageList(ctx)
	if err != nil {
		return nil, err
	}

	for _, storage := range storages {
		if storage.MountPath == mountPath {
			return &storage, nil
		}
	}

	c.logger.Debugf("在Alist服务器上未找到存储器 %s", mountPath)

	if create {
		newStorage := &AlistStorage{
			MountPath: mountPath,
			Driver:    driver,
			Order:     999,
		}
		if err := c.AdminStorageCreate(ctx, newStorage); err != nil {
			return nil, fmt.Errorf("创建存储失败: %w", err)
		}
		return newStorage, nil
	}

	return nil, fmt.Errorf("未找到挂载路径: %s", mountPath)
}

// IterPath 遍历路径（异步生成器）
func (c *AlistClient) IterPath(ctx context.Context, dirPath string, waitTime time.Duration, isDetail bool, filterFunc func(*AlistPath) bool) (<-chan *AlistPath, <-chan error) {
	outCh := make(chan *AlistPath)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		if err := c.iterPathRecursive(ctx, dirPath, waitTime, isDetail, filterFunc, outCh); err != nil {
			errCh <- err
		}
	}()

	return outCh, errCh
}

func (c *AlistClient) iterPathRecursive(ctx context.Context, dirPath string, waitTime time.Duration, isDetail bool, filterFunc func(*AlistPath) bool, outCh chan<- *AlistPath) error {
	paths, err := c.FSList(ctx, dirPath)
	if err != nil {
		return err
	}

	if waitTime > 0 {
		time.Sleep(waitTime)
	}

	for _, path := range paths {
		if path.IsDir() {
			// 递归处理子目录
			if err := c.iterPathRecursive(ctx, path.FullPath, waitTime, isDetail, filterFunc, outCh); err != nil {
				return err
			}
		} else if filterFunc != nil && filterFunc(&path) {
			// 应用过滤器
			var resultPath *AlistPath
			if isDetail {
				resultPath, err = c.FSGet(ctx, path.FullPath)
				if err != nil {
					c.logger.Warnf("获取文件详细信息失败 %s: %v", path.FullPath, err)
					continue
				}
			} else {
				resultPath = &path
			}
			outCh <- resultPath
		}
	}

	return nil
}

// Sign 计算Alist签名
func Sign(secretKey, data string) string {
	if secretKey == "" {
		return ""
	}

	h := hmac.New(sha256.New, []byte(secretKey))
	expireTimeStamp := "0"
	h.Write([]byte(data + ":" + expireTimeStamp))
	return "?sign=" + base64.URLEncoding.EncodeToString(h.Sum(nil)) + ":" + expireTimeStamp
}

// 辅助函数
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimRight(s, cutset string) string {
	for len(s) > 0 && s[len(s)-1:] == cutset {
		s = s[:len(s)-1]
	}
	return s
}
