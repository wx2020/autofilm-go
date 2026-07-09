package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const keyFile = ".db_key"

// LoadOrCreateKey 加载或创建 AES-GCM 密钥
// 优先从环境变量 AUTOFILM_DB_KEY 读取，否则从 data/.db_key 文件读取或创建
func LoadOrCreateKey(dataDir string) ([]byte, error) {
	keyStr := os.Getenv("AUTOFILM_DB_KEY")
	if keyStr != "" {
		key, err := base64.StdEncoding.DecodeString(keyStr)
		if err != nil {
			return nil, fmt.Errorf("解析 AUTOFILM_DB_KEY 失败: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("AUTOFILM_DB_KEY 长度应为 32 字节（base64 编码后 44 字符）")
		}
		return key, nil
	}

	keyPath := filepath.Join(dataDir, keyFile)
	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("密钥文件 %s 长度异常（应为 32 字节）", keyPath)
		}
		return data, nil
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("写入密钥文件失败: %w", err)
	}

	return key, nil
}

// Encrypt AES-GCM 加密，返回 base64 编码的密文
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt AES-GCM 解密 base64 编码的密文
func Decrypt(key []byte, encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("密文太短")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
