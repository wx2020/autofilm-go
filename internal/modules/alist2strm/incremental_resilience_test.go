package alist2strm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akimio/autofilm/pkg/alist"
)

// badSubdirServer 模拟一个坏子目录（/bad 返回驱动 EOF 错误），其余正常
func badSubdirServer() *httptest.Server {
	listResp := func(items string) string {
		return `{"code":200,"message":"success","data":{"total":2,"content":[` + items + `]}}`
	}
	dirItem := func(name string) string {
		return `{"name":"` + name + `","size":0,"is_dir":true,"modified":"2026-09-01T00:00:00Z","sign":"","type":1}`
	}
	fileItem := func(name string) string {
		return `{"name":"` + name + `","size":10,"is_dir":false,"modified":"2026-09-02T00:00:00Z","sign":"s1","type":2}`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/fs/list":
			var req struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			switch req.Path {
			case "/bad":
				// 模拟云盘驱动瞬时 EOF（业务 code!=200）
				w.Write([]byte(`{"code":500,"message":"EOF","data":null}`))
			case "/good":
				w.Write([]byte(listResp(fileItem("a.mkv"))))
			default:
				w.Write([]byte(listResp(dirItem("good") + "," + dirItem("bad"))))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestIterPathLightSkipsBadSubdir R1：坏子目录只跳过该子树，不中断整树，且错误带路径
func TestIterPathLightSkipsBadSubdir(t *testing.T) {
	srv := badSubdirServer()
	defer srv.Close()

	a2s, err := New(&Config{
		ID:        "t-badsubdir",
		URL:       srv.URL,
		Username:  "u",
		Password:  "p",
		SourceDir: "/",
		Mode:      "AlistPath",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files, failedDirs, incomplete, err := a2s.iterPathLight(context.Background(), "/", 0)
	if err != nil {
		t.Fatalf("根目录正常时不应返回 error: %v", err)
	}
	if !incomplete {
		t.Fatal("应标记 incomplete")
	}
	if len(failedDirs) != 1 || failedDirs[0] != "/bad" {
		t.Fatalf("failedDirs=%v", failedDirs)
	}
	if len(files) != 1 || files[0].FullPath != "/good/a.mkv" {
		t.Fatalf("files=%v", files)
	}
}

// TestIterPathLightRootFailure R1：根目录本身失败才返回 error（供调用方直接返回，不回退全量）
func TestIterPathLightRootFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		default:
			w.Write([]byte(`{"code":500,"message":"EOF","data":null}`))
		}
	}))
	defer srv.Close()

	a2s, err := New(&Config{
		ID: "t-rootfail", URL: srv.URL, Username: "u", Password: "p",
		SourceDir: "/", Mode: "AlistPath",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, _, err = a2s.iterPathLight(context.Background(), "/", 0)
	if err == nil {
		t.Fatal("根目录失败应返回 error")
	}
	// EOF 在 doRequest 层会重试 2 次后返回，错误链应保留可定位信息
	if !strings.Contains(err.Error(), "/") {
		t.Fatalf("错误应带路径信息: %v", err)
	}
}

// TestNeedFSGet R4：视频在 AlistPath / 无 PublicURL 的 AlistURL 模式下免 fs/get
func TestNeedFSGet(t *testing.T) {
	newA2S := func(mode Alist2StrmMode, publicURL string, downloadSrt bool) *Alist2Strm {
		dl := map[string]bool{}
		if downloadSrt {
			dl[".srt"] = true
		}
		return &Alist2Strm{
			config:       &Config{ID: "t", Mode: string(mode), PublicURL: publicURL},
			mode:         mode,
			downloadExts: dl,
		}
	}
	light := func(name, full string) *alist.AlistPath {
		return &alist.AlistPath{Name: name, FullPath: full, Size: 10, Modified: "x", Sign: "s"}
	}

	cases := []struct {
		name string
		a2s  *Alist2Strm
		path *alist.AlistPath
		want bool
	}{
		{"AlistPath视频免get", newA2S(AlistPathMode, "", false), light("a.mkv", "/a.mkv"), false},
		{"AlistURL无public免get", newA2S(AlistURLMode, "", false), light("a.mkv", "/a.mkv"), false},
		{"AlistURL有public需get", newA2S(AlistURLMode, "https://x.example", false), light("a.mkv", "/a.mkv"), true},
		{"RawURL视频需get", newA2S(RawURLMode, "", false), light("a.mkv", "/a.mkv"), true},
		{"字幕下载需get", newA2S(AlistPathMode, "", true), light("a.srt", "/a.srt"), true},
		{"BDMV成员免get只收集", newA2S(RawURLMode, "", false), light("a.m2ts", "/m/BDMV/STREAM/a.m2ts"), false},
		{"空指针保守需get", newA2S(AlistPathMode, "", false), nil, true},
	}
	for _, c := range cases {
		if got := c.a2s.needFSGet(c.path); got != c.want {
			t.Errorf("%s: needFSGet=%v want %v", c.name, got, c.want)
		}
	}
}
