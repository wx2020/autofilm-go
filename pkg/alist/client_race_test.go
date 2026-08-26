package alist

import (
	"sync"
	"testing"
)

func TestGetClientRace(t *testing.T) {
	url := "http://test.example.com"
	username := "testuser"
	password := "testpass"
	token := ""

	// 清理测试后的客户端
	clientsMu.Lock()
	delete(clients, url+":"+username)
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, url+":"+username)
		clientsMu.Unlock()
	}()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				client, err := GetClient(url, username, password, token)
				if err != nil {
					// 忽略网络错误，专注于竞态测试
					continue
				}
				if client == nil {
					errors <- nil
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err == nil {
			t.Error("GetClient 返回 nil，存在竞态条件")
			break
		}
	}
}

func TestGetClientDuplicateCreation(t *testing.T) {
	url := "http://test2.example.com"
	username := "testuser2"
	password := "testpass"
	token := ""

	// 清理测试后的客户端
	clientsMu.Lock()
	delete(clients, url+":"+username)
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, url+":"+username)
		clientsMu.Unlock()
	}()

	var wg sync.WaitGroup
	clientChan := make(chan *AlistClient, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				client, _ := GetClient(url, username, password, token)
				clientChan <- client
			}
		}()
	}

	wg.Wait()
	close(clientChan)

	clients := make(map[*AlistClient]bool)
	for client := range clientChan {
		clients[client] = true
	}

	if len(clients) > 1 {
		t.Errorf("期望只有 1 个客户端实例，实际有 %d 个，存在重复创建问题", len(clients))
	}
}
