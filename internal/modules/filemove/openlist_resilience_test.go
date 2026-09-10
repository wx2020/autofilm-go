package filemove

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// badSubdirServer /bad 子目录返回 object not found，其余正常
func badSubdirServer() *httptest.Server {
	listResp := func(items string) string {
		return `{"code":200,"message":"success","data":{"total":2,"content":[` + items + `]}}`
	}
	dirItem := func(name string) string {
		return `{"name":"` + name + `","size":0,"is_dir":true,"modified":"2026-09-01T00:00:00Z","sign":"","type":1}`
	}
	fileItem := func(name string) string {
		return `{"name":"` + name + `","size":10,"is_dir":false,"modified":"2026-09-02T00:00:00Z","sign":"","type":2}`
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
				w.Write([]byte(`{"code":404,"message":"object not found","data":null}`))
			case "/good":
				w.Write([]byte(listResp(fileItem("a.mp4"))))
			default:
				w.Write([]byte(listResp(dirItem("good") + "," + dirItem("bad"))))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestListOpenListRecursiveSkipsBadSubdir P0：坏子目录只跳过该子树，不中断整树
func TestListOpenListRecursiveSkipsBadSubdir(t *testing.T) {
	srv := badSubdirServer()
	defer srv.Close()

	mover, err := New(&Config{
		ID: "t-badsubdir", Backend: "openlist",
		URL: srv.URL, Username: "u", Password: "p",
		SourceDir: "/", TargetDir: "/dst",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files, failed, err := listOpenListRecursive(context.Background(), mover.client, "/")
	if err != nil {
		t.Fatalf("根目录正常时不应返回 error: %v", err)
	}
	if len(failed) != 1 || failed[0] != "/bad" {
		t.Fatalf("failed=%v", failed)
	}
	if len(files) != 1 || files[0].FullPath != "/good/a.mp4" {
		t.Fatalf("files=%v", files)
	}
}

// TestListOpenListRecursiveRootFailure P0：根目录本身失败才返回 error
func TestListOpenListRecursiveRootFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		default:
			w.Write([]byte(`{"code":404,"message":"object not found","data":null}`))
		}
	}))
	defer srv.Close()

	mover, err := New(&Config{
		ID: "t-rootfail", Backend: "openlist",
		URL: srv.URL, Username: "u", Password: "p",
		SourceDir: "/", TargetDir: "/dst",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := listOpenListRecursive(context.Background(), mover.client, "/"); err == nil {
		t.Fatal("根目录失败应返回 error")
	}
}
