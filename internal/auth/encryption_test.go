// ============================================================
// MODULE: auth
// Deskripsi: Unit tests untuk encryption package
// ============================================================

package auth

import (
	"os"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// Setup encryption key
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")
	if err := InitEncryption(); err != nil {
		t.Fatalf("InitEncryption() error = %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple text", "hello world"},
		{"api key", "binance_api_key_12345"},
		{"secret", "super_secret_api_secret_key_67890"},
		{"empty", ""},
		{"unicode", "halo dunia 你好世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			if ciphertext == "" {
				t.Error("Encrypt() returned empty ciphertext")
			}
			if ciphertext == tt.plaintext {
				t.Error("Encrypt() did not change plaintext")
			}

			// Decrypt
			decrypted, err := Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncrypt_DifferentOutputs(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")
	InitEncryption()

	// Same plaintext should produce different ciphertext (due to random nonce)
	plaintext := "same text"
	cipher1, _ := Encrypt(plaintext)
	cipher2, _ := Encrypt(plaintext)

	if cipher1 == cipher2 {
		t.Error("Expected different ciphertexts for same plaintext (different nonces)")
	}

	// But both should decrypt to same value
	dec1, _ := Decrypt(cipher1)
	dec2, _ := Decrypt(cipher2)
	if dec1 != dec2 || dec1 != plaintext {
		t.Error("Decrypted values should match original plaintext")
	}
}

func TestEncrypt_NotInitialized(t *testing.T) {
	// Reset EncryptionKey
	EncryptionKey = nil

	_, err := Encrypt("test")
	if err == nil {
		t.Error("Expected error when encryption not initialized")
	}
}

func TestDecrypt_InvalidCiphertext(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")
	InitEncryption()

	tests := []struct {
		name       string
		ciphertext string
	}{
		{"invalid base64", "not-valid-base64!!!"},
		{"too short", "YWJj"}, // "abc" in base64, but too short for nonce + data
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decrypt(tt.ciphertext)
			if err == nil {
				t.Errorf("Expected error for invalid ciphertext: %s", tt.ciphertext)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name     string
		secret   string
		visible  int
		expected string
	}{
		{"short secret", "abc", 2, "***"},
		{"normal secret", "mysecretkey", 2, "my*******ey"}, // visible(2) + stars(7) + visible(2) = 11 chars
		{"long secret", "verylongsecretkey12345", 3, "ver****************345"}, // visible(3) + stars(18) + visible(3) = 24 chars
		{"exact length", "ab", 1, "**"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSecret(tt.secret, tt.visible)
			if got != tt.expected {
				t.Errorf("MaskSecret(%q, %d) = %q, want %q", tt.secret, tt.visible, got, tt.expected)
			}
		})
	}
}

func TestGenerateEncryptionKey(t *testing.T) {
	key1, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}
	if len(key1) == 0 {
		t.Error("GenerateEncryptionKey() returned empty key")
	}

	// Should generate different keys each time
	key2, _ := GenerateEncryptionKey()
	if key1 == key2 {
		t.Error("Expected different keys on each generation")
	}

	// Key should be base64 encoded (44 chars for 32 bytes)
	if len(key1) != 44 {
		t.Errorf("Expected base64 key length 44, got %d", len(key1))
	}
}