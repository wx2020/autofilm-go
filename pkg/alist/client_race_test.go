package alist

import (
	"sync"
	"testing"
)

func TestGetClientRace(t *testing.T) {
	url := "http://test-race.example.com"
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
			for j := 0; j < 50; j++ {
				GetClient(url, username, password, token)
			}
		}()
	}
	wg.Wait()
}

func TestGetClientDuplicateCreation(t *testing.T) {
	url := "http://test-dup.example.com"
	username := "testuser2"
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
	clientCh := make(chan *AlistClient, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				client, _ := GetClient(url, username, password, token)
				if client != nil {
					clientCh <- client
				}
			}
		}()
	}
	wg.Wait()
	close(clientCh)

	uniqueClients := make(map[*AlistClient]bool)
	for c := range clientCh {
		uniqueClients[c] = true
	}

	if len(uniqueClients) != 1 {
		t.Errorf("期望只有 1 个客户端实例，实际有 %d 个", len(uniqueClients))
	}
}
