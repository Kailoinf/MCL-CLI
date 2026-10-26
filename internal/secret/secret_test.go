package secret

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDeriveKeyIsDeterministicAndSalted(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	k1, err := DeriveKey("machine-a", salt)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := DeriveKey("machine-a", salt)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) {
		t.Error("same identity + salt must derive the same key")
	}
	if len(k1) != 32 {
		t.Errorf("key length = %d, want 32", len(k1))
	}
	other, err := DeriveKey("machine-b", salt)
	if err != nil {
		t.Fatal(err)
	}
	if string(other) == string(k1) {
		t.Error("different identities must derive different keys")
	}
	otherSalt, _ := NewSalt()
	k3, _ := DeriveKey("machine-a", otherSalt)
	if string(k3) == string(k1) {
		t.Error("different salts must derive different keys")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := DeriveKey("machine-a", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := "M.C522_SN1!refresh-token-lookalike"
	sealed, err := Encrypt(key, []byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, plaintext) {
		t.Fatal("ciphertext leaks the plaintext")
	}
	if _, err := base64.StdEncoding.DecodeString(sealed); err != nil {
		t.Fatalf("ciphertext is not base64: %v", err)
	}
	// nonces must differ between encryptions of the same value
	sealed2, _ := Encrypt(key, []byte(plaintext))
	if sealed == sealed2 {
		t.Error("nonce reuse: identical ciphertexts for identical plaintext")
	}
	got, err := Decrypt(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != plaintext {
		t.Errorf("round trip = %q", got)
	}
}

func TestDecryptFailsOnForeignMachine(t *testing.T) {
	salt := []byte("0123456789abcdef")
	keyA, _ := DeriveKey("machine-a", salt)
	keyB, _ := DeriveKey("machine-b", salt)
	sealed, _ := Encrypt(keyA, []byte("secret"))
	if _, err := Decrypt(keyB, sealed); err == nil {
		t.Error("a different machine's key must not decrypt the secret")
	}
	if _, err := Decrypt(keyA, "short"); err == nil {
		t.Error("truncated ciphertext must fail")
	}
	if _, err := Decrypt(keyA, "@@@not-base64@@@"); err == nil {
		t.Error("invalid base64 must fail")
	}
}

func TestMachineIdentityIncludesHostFacts(t *testing.T) {
	id := MachineIdentity()
	if id == "" {
		t.Fatal("identity must not be empty")
	}
	if !strings.Contains(id, "os=") || !strings.Contains(id, "user=") {
		t.Errorf("identity looks incomplete: %q", id)
	}
	if strings.Contains(id, "\n") {
		t.Errorf("identity must be single line: %q", id)
	}
}
