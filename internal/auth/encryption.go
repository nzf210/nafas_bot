// ============================================================
// MODULE: auth/encryption
// Deskripsi: AES-256 encryption utilities untuk API keys
// ============================================================

package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EncryptionKey adalah32-byte AES-256 key dari environment
var EncryptionKey []byte

// InitEncryption inisialisasi encryption key dari environment variable
// Nama Function: InitEncryption
// Deskripsi: Mengambil dan memvalidasi ENCRYPTION_KEY dari environment.
// Parameter/Value Input:
//   - Tidak ada parameter langsung, membaca dari os.Getenv("ENCRYPTION_KEY")
// Function yang Dipanggil/Dikonsumsi:
//   - os.Getenv: dipanggil untuk mengambil ENCRYPTION_KEY
//   - base64.StdEncoding.DecodeString: dipanggil untuk decode base64 key
// Output/Return Value:
//   - error: error jika key tidak valid atau tidak ada
func InitEncryption() error {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		return errors.New("ENCRYPTION_KEY environment variable is required")
	}

	// Support base64 encoded atau raw string
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil || len(key) != 32 {
		// Jika bukan base64 valid sepanjang 32 byte, gunakan sebagai raw string
		key = []byte(keyStr)
	}

	if len(key) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	EncryptionKey = key
	return nil
}

// Encrypt encrypt plaintext menggunakan AES-256-GCM
// Nama Function: Encrypt
// Deskripsi: Mengenkripsi plaintext ke ciphertext menggunakan AES-256-GCM.
// Parameter/Value Input:
//   - plaintext: string — teks yang akan dienkripsi
// Function yang Dipanggil/Dikonsumsi:
//   - crypto/aes.NewCipher: dipanggil untuk buat cipher
//   - crypto/cipher.NewGCM: dipanggil untuk buat GCM mode
//   - crypto/rand.Read: dipanggil untuk generate nonce
// Output/Return Value:
//   - string: ciphertext dalam format base64 (nonce + ciphertext)
//   - error: error jika encrypt gagal
func Encrypt(plaintext string) (string, error) {
	if EncryptionKey == nil {
		return "", errors.New("encryption not initialized, call InitEncryption first")
	}

	block, err := aes.NewCipher(EncryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypt ciphertext menggunakan AES-256-GCM
// Nama Function: Decrypt
// Deskripsi: Mendekripsi ciphertext ke plaintext menggunakan AES-256-GCM.
// Parameter/Value Input:
//   - ciphertext: string — ciphertext dalam format base64
// Function yang Dipanggil/Dikonsumsi:
//   - crypto/aes.NewCipher: dipanggil untuk buat cipher
//   - crypto/cipher.NewGCM: dipanggil untuk buat GCM mode
//   - base64.StdEncoding.DecodeString: dipanggil untuk decode ciphertext
// Output/Return Value:
//   - string: plaintext yang sudah didekripsi
//   - error: error jika decrypt gagal
func Decrypt(ciphertext string) (string, error) {
	if EncryptionKey == nil {
		return "", errors.New("encryption not initialized, call InitEncryption first")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(EncryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// GenerateEncryptionKey generate random 32-byte key untuk pertama kali
// Nama Function: GenerateEncryptionKey
// Deskripsi: Membuat random32-byte key untuk ENCRYPTION_KEY.
// Parameter/Value Input:
//   - Tidak ada parameter langsung
// Function yang Dipanggil/Dikonsumsi:
//   - crypto/rand.Read: dipanggil untuk generate random bytes
//   - base64.StdEncoding.EncodeToString: dipanggil untuk encode ke base64
// Output/Return Value:
//   - string: base64 encoded 32-byte key
//   - error: error jika generation gagal
func GenerateEncryptionKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// MaskSecret membuat masked version dari secret untuk logging
// Nama Function: MaskSecret
// Deskripsi: Membuat masked version dari secret untuk aman di-log.
// Parameter/Value Input:
//   - secret: string — secret yang akan di-mask
//   - visible: int — jumlah karakter yang visible di awal dan akhir
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - string: masked secret (contoh: "B*******d")
func MaskSecret(secret string, visible int) string {
	if len(secret) <= visible*2 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:visible] + strings.Repeat("*", len(secret)-visible*2) + secret[len(secret)-visible:]
}
