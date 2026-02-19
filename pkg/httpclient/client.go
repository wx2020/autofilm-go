package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	globalClient *HTTPClient
	clientOnce   sync.Once
)

// HTTPClient HTTP客户端
type HTTPClient struct {
	client      *http.Client
	timeout     time.Duration
	userAgent   string
	logger      *logrus.Logger
	maxRetries  int
	retryDelay  time.Duration
}

// Config HTTP客户端配置
type Config struct {
	Timeout    time.Duration
	MaxRetries int
	UserAgent  string
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Timeout:    10 * time.Second,
		MaxRetries: 3,
		UserAgent:  "AutoFilm/v1.5.0-1",
	}
}

// GetClient 获取全局HTTP客户端
func GetClient(config ...*Config) *HTTPClient {
	clientOnce.Do(func() {
		cfg := DefaultConfig()
		if len(config) > 0 && config[0] != nil {
			if config[0].Timeout > 0 {
				cfg.Timeout = config[0].Timeout
			}
			if config[0].MaxRetries > 0 {
				cfg.MaxRetries = config[0].MaxRetries
			}
			if config[0].UserAgent != "" {
				cfg.UserAgent = config[0].UserAgent
			}
		}

		globalClient = &HTTPClient{
			client: &http.Client{
				Timeout: cfg.Timeout,
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			},
			timeout:    cfg.Timeout,
			userAgent:  cfg.UserAgent,
			maxRetries: cfg.MaxRetries,
			retryDelay: time.Second,
		}
	})
	return globalClient
}

// Response HTTP响应
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Request 发送HTTP请求
func (c *HTTPClient) Request(ctx context.Context, method, url string, headers map[string]string, body io.Reader) (*Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Debugf("重试请求 %s %s (第 %d/%d 次)", method, url, attempt, c.maxRetries)
			time.Sleep(c.retryDelay * time.Duration(attempt))
		}

		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}

		// 设置请求头
		req.Header.Set("User-Agent", c.userAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			c.logger.Debugf("请求失败 %s: %v", url, err)
			continue
		}

		defer resp.Body.Close()

		// 读取响应体
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			c.logger.Debugf("读取响应体失败 %s: %v", url, err)
			continue
		}

		return &Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       respBody,
		}, nil
	}

	return nil, fmt.Errorf("请求失败，已重试 %d 次: %w", c.maxRetries, lastErr)
}

// Get 发送GET请求
func (c *HTTPClient) Get(ctx context.Context, url string, headers map[string]string) (*Response, error) {
	return c.Request(ctx, http.MethodGet, url, headers, nil)
}

// Post 发送POST请求
func (c *HTTPClient) Post(ctx context.Context, url string, headers map[string]string, body []byte) (*Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	return c.Request(ctx, http.MethodPost, url, headers, bodyReader)
}

// Put 发送PUT请求
func (c *HTTPClient) Put(ctx context.Context, url string, headers map[string]string, body []byte) (*Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	return c.Request(ctx, http.MethodPut, url, headers, bodyReader)
}

// Delete 发送DELETE请求
func (c *HTTPClient) Delete(ctx context.Context, url string, headers map[string]string) (*Response, error) {
	return c.Request(ctx, http.MethodDelete, url, headers, nil)
}

// Head 发送HEAD请求
func (c *HTTPClient) Head(ctx context.Context, url string, headers map[string]string) (*Response, error) {
	return c.Request(ctx, http.MethodHead, url, headers, nil)
}

// Download 下载文件到指定路径
func (c *HTTPClient) Download(ctx context.Context, url, filePath string, headers map[string]string) error {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Debugf("重试下载 %s (第 %d/%d 次)", url, attempt, c.maxRetries)
			time.Sleep(c.retryDelay * time.Duration(attempt))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("创建下载请求失败: %w", err)
		}

		req.Header.Set("User-Agent", c.userAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
			continue
		}

		// 创建文件
		out, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("创建文件失败: %w", err)
		}
		defer out.Close()

		// 写入文件
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		c.logger.Debugf("文件下载成功: %s", filePath)
		return nil
	}

	return fmt.Errorf("下载失败，已重试 %d 次: %w", c.maxRetries, lastErr)
}

// Close 关闭HTTP客户端
func (c *HTTPClient) Close() {
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
}
