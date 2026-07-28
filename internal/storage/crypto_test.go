package storage

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	encoded, err := Encrypt(key, "secret-token")
	if err != nil { t.Fatal(err) }
	if encoded == "secret-token" { t.Fatal("ciphertext equals plaintext") }
	plain, err := Decrypt(key, encoded)
	if err != nil { t.Fatal(err) }
	if plain != "secret-token" { t.Fatalf("plain = %q", plain) }
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	encoded, _ := Encrypt(bytes.Repeat([]byte{1}, 32), "secret")
	if _, err := Decrypt(bytes.Repeat([]byte{2}, 32), encoded); err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("strong-password")
	if err != nil { t.Fatal(err) }
	if !VerifyPassword(hash, "strong-password") { t.Fatal("valid password rejected") }
	if VerifyPassword(hash, "wrong-password") { t.Fatal("invalid password accepted") }
}
