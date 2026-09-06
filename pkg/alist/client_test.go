package alist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func jsonDecodeBody(r *http.Request, v any) error {
	b, _ := io.ReadAll(r.Body)
	return json.Unmarshal(b, v)
}

func genItems(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"name":"f%d","size":1,"type":4,"modified":"2026-01-01T00:00:00Z"}`, i)
	}
	return sb.String()
}

func newAPIError(msg string) error { return fmt.Errorf("API错误: %s", msg) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func boolPtr(b bool) *bool { return &b }

func TestIsDirPrefersIsDirFlag(t *testing.T) {
	video := &AlistPath{Type: 2, IsDirFlag: boolPtr(false)}
	if video.IsDir() {
		t.Fatal("type=2 + is_dir=false 应为文件")
	}
	folder := &AlistPath{Type: 1, IsDirFlag: boolPtr(true)}
	if !folder.IsDir() {
		t.Fatal("type=1 + is_dir=true 应为目录")
	}
	legacy := &AlistPath{Type: 1}
	if !legacy.IsDir() {
		t.Fatal("老版本 Type==1 应回退判目录")
	}
}

func TestTaskInfoUsesOfflineDownloadEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/admin/task/offline_download/info":
			gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
			w.Write([]byte(`{"code":200,"message":"success","data":[{"id":"t1","name":"d","state":"succeeded","status":"","progress":100,"error":""}]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"code":404,"message":"not found","data":null}`))
		}
	}))
	defer srv.Close()
	c, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	info, err := c.TaskInfo(t.Context(), "t1")
	if err != nil {
		t.Fatalf("TaskInfo: %v", err)
	}
	if info.State != "succeeded" || gotPath != "/api/admin/task/offline_download/info" || !contains(gotQuery, "tid=t1") {
		t.Fatalf("got %+v %s %s", info, gotPath, gotQuery)
	}
}

func TestFSListPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/fs/list":
			var req struct {
				Page    int `json:"page"`
				PerPage int `json:"per_page"`
			}
			_ = jsonDecodeBody(r, &req)
			if req.PerPage != 100 {
				t.Errorf("per_page = %d, want 100", req.PerPage)
			}
			if req.Page == 1 {
				w.Write([]byte(`{"code":200,"message":"success","data":{"total":101,"content":[` + genItems(100) + `]}}`))
			} else {
				w.Write([]byte(`{"code":200,"message":"success","data":{"total":101,"content":[` + genItems(1) + `]}}`))
			}
		}
	}))
	defer srv.Close()
	c, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	items, err := c.FSListLight(t.Context(), "/d")
	if err != nil {
		t.Fatalf("FSListLight: %v", err)
	}
	if len(items) != 101 {
		t.Fatalf("items = %d, want 101", len(items))
	}
	if items[0].FullPath != "/d/f0" {
		t.Fatalf("FullPath = %s", items[0].FullPath)
	}
}

// TestGetTokenPasswordAuthNoDeadlock 回归测试：
// 用户名/密码认证（tokenExp 初始为 0）首次调用必须能完成登录，
// 不得因写锁内再次 RLock 造成自锁死锁。
func TestGetTokenPasswordAuthNoDeadlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"new-token"}}`))
		case "/api/me":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewStandalone(srv.URL, "user", "pass", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	done := make(chan string, 1)
	go func() {
		done <- client.getToken()
	}()
	select {
	case tok := <-done:
		if tok != "new-token" {
			t.Fatalf("token = %q, want new-token", tok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("getToken 死锁：5 秒内未返回")
	}
}

func TestSuffixLowercase(t *testing.T) {
	p := &AlistPath{Name: "MOVIE.MKV"}
	if got := p.Suffix(); got != ".mkv" {
		t.Fatalf("Suffix() = %q, want .mkv", got)
	}
	p = &AlistPath{Name: "Sub.SRT"}
	if got := p.Suffix(); got != ".srt" {
		t.Fatalf("Suffix() = %q, want .srt", got)
	}
	p = &AlistPath{Name: "noext"}
	if got := p.Suffix(); got != "" {
		t.Fatalf("Suffix() = %q, want empty", got)
	}
}

func TestClientCacheKeyDistinguishesCredentials(t *testing.T) {
	_, k1 := clientCacheKey("http://a.com", "u", "p1", "t")
	_, k2 := clientCacheKey("http://a.com", "u", "p2", "t")
	_, k3 := clientCacheKey("http://a.com", "u", "p1", "t")
	if k1 == k2 {
		t.Fatal("不同密码的缓存键不应相同")
	}
	if k1 != k3 {
		t.Fatal("相同凭据的缓存键应一致")
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(newAPIError("object not found")) {
		t.Fatal("object not found 应判为 NotFound")
	}
	if IsNotFound(newAPIError("success")) {
		t.Fatal("success 不应判为 NotFound")
	}
	if IsNotFound(nil) {
		t.Fatal("nil 不应判为 NotFound")
	}
}

func TestDoRequestHTMLBodyDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/me":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/fs/get":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>not found</body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端: %v", err)
	}
	_, err = c.FSGet(t.Context(), "/wo/a.mkv")
	if err == nil {
		t.Fatal("HTML 响应应返回错误")
	}
	msg := err.Error()
	if !contains(msg, "/api/fs/get") || !contains(msg, "<html>") {
		t.Fatalf("诊断信息应含 endpoint 与 body 摘要，got: %s", msg)
	}
}

func TestAddOfflineDownloadUsesV4API(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/fs/add_offline_download":
			w.Write([]byte(`{"code":200,"message":"success","data":{"tasks":[{"id":"task123","name":"d","state":0,"status":"","progress":0,"error":""}]}}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`<html>404</html>`))
		}
	}))
	defer srv.Close()

	c, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端: %v", err)
	}
	id, err := c.FSPut(t.Context(), "/wo/电视剧", []FSPutFile{{Path: "a.mkv", URL: "http://x/a.mkv?sign=1"}})
	if err != nil {
		t.Fatalf("FSPut: %v", err)
	}
	if id != "task123" {
		t.Fatalf("task id = %q, want task123", id)
	}
	if gotMethod != "POST" || gotPath != "/api/fs/add_offline_download" {
		t.Fatalf("应调 POST /api/fs/add_offline_download，got %s %s", gotMethod, gotPath)
	}
	for _, want := range []string{`"path":"/wo/`, `"urls"`, "SimpleHttp", "delete_on_upload_succeed"} {
		if !contains(gotBody, want) {
			t.Fatalf("请求体缺 %s，got: %s", want, gotBody)
		}
	}
}
