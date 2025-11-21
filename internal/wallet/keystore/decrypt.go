package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// decryptMnemonic decrypts a mnemonic from Ethereum keystore v3 format
func (s *service) decryptMnemonic(keystoreJSON *KeystoreJSON, password string) (string, error) {
	// Decode hex strings
	salt, err := hex.DecodeString(keystoreJSON.Crypto.KDFParams.Salt)
	if err != nil {
		return "", fmt.Errorf("failed to decode salt: %w", err)
	}

	//nolint:varnamelen // iv is a common abbreviation for initialization vector
	iv, err := hex.DecodeString(keystoreJSON.Crypto.CipherParams.IV)
	if err != nil {
		return "", fmt.Errorf("failed to decode IV: %w", err)
	}

	ciphertext, err := hex.DecodeString(keystoreJSON.Crypto.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	expectedMAC, err := hex.DecodeString(keystoreJSON.Crypto.MAC)
	if err != nil {
		return "", fmt.Errorf("failed to decode MAC: %w", err)
	}

	// Derive encryption key using scrypt
	derivedKey, err := scrypt.Key(
		[]byte(password),
		salt,
		keystoreJSON.Crypto.KDFParams.N,
		keystoreJSON.Crypto.KDFParams.R,
		keystoreJSON.Crypto.KDFParams.P,
		keystoreJSON.Crypto.KDFParams.DKLen,
	)
	if err != nil {
		return "", fmt.Errorf("failed to derive key: %w", err)
	}

	// Verify MAC
	mac := calculateMAC(derivedKey[16:32], ciphertext)
	if !constantTimeCompare(mac, expectedMAC) {
		return "", fmt.Errorf("invalid password: MAC mismatch")
	}

	// Decrypt mnemonic using AES-128-CTR
	plaintext, err := decryptAES128CTR(derivedKey[:16], iv, ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt mnemonic: %w", err)
	}

	return string(plaintext), nil
}

// decryptAES128CTR decrypts data using AES-128-CTR mode
//
//nolint:varnamelen // iv is a common abbreviation for initialization vector
func decryptAES128CTR(key []byte, iv []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	plaintext := make([]byte, len(ciphertext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(plaintext, ciphertext)

	return plaintext, nil
}

// constantTimeCompare performs constant-time comparison of two byte slices
//
//nolint:varnamelen // a and b are standard parameter names for comparison functions
func constantTimeCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	result := 0
	for i := 0; i < len(a); i++ {
		result |= int(a[i] ^ b[i])
	}

	return result == 0
}
