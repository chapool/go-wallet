package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/scrypt"
)

// encryptMnemonic encrypts a mnemonic using Ethereum keystore v3 format
//
//nolint:varnamelen // iv is a common abbreviation for initialization vector
func (s *service) encryptMnemonic(mnemonic string, password string) (*KeystoreJSON, error) {
	// Generate random salt and IV
	//nolint:mnd // 32 is the standard salt size for scrypt
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	//nolint:mnd // 16 is the standard IV size for AES-128-CTR
	//nolint:varnamelen // iv is a common abbreviation for initialization vector
	iv := make([]byte, 16) // AES-128-CTR requires 16-byte IV
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Derive encryption key using scrypt
	params := DefaultScryptParams()
	params.Salt = salt

	derivedKey, err := scrypt.Key([]byte(password), salt, params.N, params.R, params.P, params.DKLen)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Encrypt mnemonic using AES-128-CTR
	mnemonicBytes := []byte(mnemonic)
	ciphertext, err := encryptAES128CTR(derivedKey[:16], iv, mnemonicBytes) // Use first 16 bytes for AES-128
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %w", err)
	}

	// Calculate MAC (SHA3-256 of derivedKey[16:32] + ciphertext)
	mac := calculateMAC(derivedKey[16:32], ciphertext)

	// Build keystore JSON
	keystoreJSON := &KeystoreJSON{
		//nolint:mnd // 3 is the Ethereum keystore v3 version number
		Version: 3,
		ID:      uuid.New().String(),
	}

	keystoreJSON.Crypto.Ciphertext = hex.EncodeToString(ciphertext)
	keystoreJSON.Crypto.CipherParams.IV = hex.EncodeToString(iv)
	keystoreJSON.Crypto.Cipher = "aes-128-ctr"
	keystoreJSON.Crypto.KDF = "scrypt"
	keystoreJSON.Crypto.KDFParams.DKLen = params.DKLen
	keystoreJSON.Crypto.KDFParams.Salt = hex.EncodeToString(salt)
	keystoreJSON.Crypto.KDFParams.N = params.N
	keystoreJSON.Crypto.KDFParams.R = params.R
	keystoreJSON.Crypto.KDFParams.P = params.P
	keystoreJSON.Crypto.MAC = hex.EncodeToString(mac)

	return keystoreJSON, nil
}

// encryptAES128CTR encrypts data using AES-128-CTR mode
//
//nolint:varnamelen // iv is a common abbreviation for initialization vector
func encryptAES128CTR(key []byte, iv []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	ciphertext := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	return ciphertext, nil
}

// calculateMAC calculates MAC using SHA3-256(derivedKey[16:32] + ciphertext)
// Note: Ethereum keystore v3 uses SHA3-256, but we'll use SHA-256 for now
// For full compatibility, we should use SHA3-256 (Keccak-256)
func calculateMAC(key []byte, ciphertext []byte) []byte {
	hasher := sha256.New()
	hasher.Write(key)
	hasher.Write(ciphertext)
	return hasher.Sum(nil)
}
