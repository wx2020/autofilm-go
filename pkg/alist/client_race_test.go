package alist

import (
	"sync"
	"testing"
)

func TestClientsMapAccess(t *testing.T) {
	url := "http://test.example.com"
	username := "testuser"
	password := "testpass"
	token := ""

	clientsMu.Lock()
	delete(clients, url+":"+username)
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, url+":"+username)
		clientsMu.Unlock()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				clientsMu.RLock()
				_, exists := clients[url+":"+username]
				clientsMu.RUnlock()
				if exists {
					t.Error("客户端不应该存在")
				}
			}
		}()
	}
	wg.Wait()
}
