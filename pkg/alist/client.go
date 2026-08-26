package alist

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/httpclient"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

var (
	clients   = make(map[string]*AlistClient)
	clientsMu sync.RWMutex
)

// AlistClient Alist客户端
type AlistClient struct {
	url         string
	username    string
	password    string
	token       string
	basePath    string
	id          int
	httpClient  *httpclient.HTTPClient
	logger      *logrus.Logger
	tokenMu     sync.RWMutex
	loginMu     sync.Mutex  // 序列化令牌刷新，避免并发重复登录
	refreshing  atomic.Bool // 登录请求进行中：嵌套的 getToken 直接返回当前令牌
	tokenExp    int64
	rateLimiter atomic.Pointer[rate.Limiter] // QPS 限流器；原子访问：多个任务可能共享客户端并各自设置限流
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
	Sign      string `json:"sign"`
	RawURL    string `json:"raw_url,omitempty"`
	Thumb     string `json:"thumb,omitempty"`
}

// IsDir 判断是否为目录
func (p *AlistPath) IsDir() bool {
	return p.Type == 1
}

// Suffix 获取文件后缀（统一小写，保证大写扩展名也能匹配扩展名表）
func (p *AlistPath) Suffix() string {
	for i := len(p.Name) - 1; i >= 0; i-- {
		if p.Name[i] == '.' {
			return strings.ToLower(p.Name[i:])
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
// 缓存键包含凭据摘要，避免同地址不同密码时复用错误客户端
func GetClient(url, username, password, token string) (*AlistClient, error) {
	url, key := clientCacheKey(url, username, password, token)

	// 快速路径：已有客户端直接返回（读锁）
	clientsMu.RLock()
	if client, exists := clients[key]; exists {
		clientsMu.RUnlock()
		return client, nil
	}
	clientsMu.RUnlock()

	// 创建新客户端（写锁）
	clientsMu.Lock()
	defer clientsMu.Unlock()

	// 二次检查：可能在等待锁期间被其他协程创建
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

// NewStandalone 创建不进入全局缓存的临时客户端，用于连接测试等一次性场景，
// 避免测试凭据污染正式任务的客户端缓存。
func NewStandalone(url, username, password, token string) (*AlistClient, error) {
	url = normalizeBaseURL(url)
	return newAlistClient(url, username, password, token)
}

func normalizeBaseURL(url string) string {
	if url != "" && !startsWith(url, "http://") && !startsWith(url, "https://") {
		url = "https://" + url
	}
	return trimRight(url, "/")
}

func clientCacheKey(rawURL, username, password, token string) (string, string) {
	url := normalizeBaseURL(rawURL)
	sum := sha256.Sum256([]byte(password + "\x00" + token))
	digest := base64.RawStdEncoding.EncodeToString(sum[:8])
	return url, url + "|" + username + "|" + digest
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

	// core 日志未初始化时（如独立使用本包）回退到标准 logrus，避免 nil 指针
	if client.logger == nil {
		client.logger = logrus.StandardLogger()
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

// getToken 获取当前可用的 Authorization 令牌
// 刷新令牌时绝不持有任何锁调用 authLogin：
//   - 不持有 tokenMu 写锁，避免同 goroutine 内再次 RLock 自锁；
//   - authLogin 自身的请求也会经过 getToken（makeHeaders），
//     此时 refreshing 标记使嵌套调用直接返回当前令牌而不再尝试刷新。
func (c *AlistClient) getToken() string {
	c.tokenMu.RLock()
	tok := c.token
	exp := c.tokenExp
	c.tokenMu.RUnlock()

	if exp == -1 || exp >= time.Now().Unix() || c.refreshing.Load() {
		return tok
	}

	// 序列化刷新动作，避免过期瞬间并发重复登录
	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	// 双重检查：可能已被其他协程刷新
	c.tokenMu.RLock()
	tok = c.token
	exp = c.tokenExp
	c.tokenMu.RUnlock()
	if exp == -1 || exp >= time.Now().Unix() {
		return tok
	}

	c.refreshing.Store(true)
	newToken, err := c.authLogin(context.Background())
	c.refreshing.Store(false)

	if err != nil {
		c.logger.Errorf("重新获取令牌失败: %v", err)
		return ""
	}

	c.tokenMu.Lock()
	c.token = newToken
	// 令牌有效期2天，提前5分钟刷新
	c.tokenExp = time.Now().Unix() + 2*24*60*60 - 5*60
	c.tokenMu.Unlock()

	c.logger.Debugf("%s 更新令牌成功", c.username)
	return newToken
}

func (c *AlistClient) makeHeaders() map[string]string {
	return map[string]string{
		"Authorization": c.getToken(),
		"Content-Type":  "application/json",
	}
}

// LimitQPS 返回当前生效的 QPS 限流值（次/秒），未设置限流时返回 0。
// 供诊断、测试与共享客户端场景下确认限流策略。
func (c *AlistClient) LimitQPS() int {
	if lim := c.rateLimiter.Load(); lim != nil {
		return int(lim.Limit())
	}
	return 0
}

// SetRateLimit 设置 QPS 限流（qps<=0 取消限流）
// 注意：通过 GetClient 缓存共享同一客户端的所有任务共用这一个限流器，
// 后设置的值对全部在途/后续请求生效。
func (c *AlistClient) SetRateLimit(qps int) {
	if qps <= 0 {
		c.rateLimiter.Store(nil)
		return
	}
	c.rateLimiter.Store(rate.NewLimiter(rate.Limit(qps), qps))
}

func (c *AlistClient) doRequest(ctx context.Context, method, endpoint string, jsonData []byte) (*APIResponse, error) {
	// 原子读取一次，避免读取过程中被其他协程替换导致不一致
	if lim := c.rateLimiter.Load(); lim != nil {
		if err := lim.Wait(ctx); err != nil {
			return nil, fmt.Errorf("限流等待失败: %w", err)
		}
	}

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

// FSList 获取文件列表（轻量，不逐文件发起 fs/get 请求）
func (c *AlistClient) FSList(ctx context.Context, dirPath string) ([]AlistPath, error) {
	return c.FSListLight(ctx, dirPath)
}

// FSListLight 获取文件列表（轻量版，不发起 fs/get 请求）
// 返回的文件信息不含 RawURL；仅用于增量快照收集，降低请求量
func (c *AlistClient) FSListLight(ctx context.Context, dirPath string) ([]AlistPath, error) {
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
		PerPage:  0,
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

	for i := range result.Content {
		result.Content[i].ServerURL = c.url
		result.Content[i].BasePath = c.basePath
		result.Content[i].FullPath = dirPath + "/" + result.Content[i].Name
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

	c.logger.Debugf("[DEBUG] FSGet 解析后 - RawURL: '%s'", maskURL(result.RawURL))

	return &result, nil
}

// FSPutFile 文件上传请求中的文件项
type FSPutFile struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// TaskInfoData 任务状态信息
type TaskInfoData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"` // succeeded, failed, canceled, running
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}

// FSMkdir 创建目录（类似 mkdir -p，可递归创建）
func (c *AlistClient) FSMkdir(ctx context.Context, dirPath string) error {
	req := map[string]string{"path": dirPath}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/fs/mkdir", jsonData)
	return err
}

// FSRemove 删除目录中的文件或目录。
func (c *AlistClient) FSRemove(ctx context.Context, dirPath string, names []string) error {
	req := struct {
		Dir   string   `json:"dir"`
		Names []string `json:"names"`
	}{Dir: dirPath, Names: names}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/fs/remove", jsonData)
	return err
}

// FSMove 在同一个 OpenList 实例内移动文件或目录。
func (c *AlistClient) FSMove(ctx context.Context, srcDir, dstDir string, names []string) error {
	req := struct {
		SrcDir string   `json:"src_dir"`
		DstDir string   `json:"dst_dir"`
		Names  []string `json:"names"`
	}{SrcDir: srcDir, DstDir: dstDir, Names: names}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/fs/move", jsonData)
	return err
}

// FSRename 重命名文件或目录。
func (c *AlistClient) FSRename(ctx context.Context, path, name string) error {
	req := struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}{Path: path, Name: name}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/fs/rename", jsonData)
	return err
}

// FSPut 提交异步文件上传任务
// dstPath: 目标目录路径
// files: 要上传的文件列表（path=文件名, url=源文件直链）
// 返回值: alist_task_id（异步任务ID）
func (c *AlistClient) FSPut(ctx context.Context, dstPath string, files []FSPutFile) (string, error) {
	req := struct {
		Path  string      `json:"path"`
		Files []FSPutFile `json:"files"`
	}{
		Path:  dstPath,
		Files: files,
	}

	jsonData, _ := json.Marshal(req)
	resp, err := c.doRequest(ctx, "POST", "/api/fs/put", jsonData)
	if err != nil {
		return "", err
	}

	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("解析任务ID失败: %w", err)
	}
	return result.TaskID, nil
}

// TaskInfo 查询异步任务状态
func (c *AlistClient) TaskInfo(ctx context.Context, taskID string) (*TaskInfoData, error) {
	req := map[string]string{"id": taskID}
	jsonData, _ := json.Marshal(req)
	resp, err := c.doRequest(ctx, "POST", "/api/admin/task/task_info", jsonData)
	if err != nil {
		return nil, err
	}

	var result TaskInfoData
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析任务信息失败: %w", err)
	}
	return &result, nil
}

// TaskCancel 取消异步任务
func (c *AlistClient) TaskCancel(ctx context.Context, taskID string) error {
	req := map[string]string{"id": taskID}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/admin/task/cancel", jsonData)
	return err
}

// TaskRetry 重试失败的异步任务
func (c *AlistClient) TaskRetry(ctx context.Context, taskID string) error {
	req := map[string]string{"id": taskID}
	jsonData, _ := json.Marshal(req)
	_, err := c.doRequest(ctx, "POST", "/api/admin/task/retry", jsonData)
	return err
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}

	for _, path := range paths {
		if path.IsDir() {
			// 递归处理子目录
			if err := c.iterPathRecursive(ctx, path.FullPath, waitTime, isDetail, filterFunc, outCh); err != nil {
				return err
			}
			continue
		}
		if filterFunc == nil || !filterFunc(&path) {
			continue
		}
		// 过滤后的文件才按需补全详情：显式要求详情，或列表接口未返回签名/直链时
		resultPath := &path
		if isDetail || (path.RawURL == "" && path.Sign == "") {
			detail, err := c.FSGet(ctx, path.FullPath)
			if err != nil {
				c.logger.Warnf("获取文件详细信息失败 %s: %v（使用目录列表数据继续）", path.FullPath, err)
			} else {
				resultPath = detail
			}
		}
		select {
		case outCh <- resultPath:
		case <-ctx.Done():
			return ctx.Err()
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

func maskURL(url string) string {
	if url == "" {
		return ""
	}
	if len(url) > 50 {
		return url[:50] + "..."
	}
	return url
}
