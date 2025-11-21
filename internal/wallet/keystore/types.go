package keystore

import (
	"github/chapool/go-wallet/internal/models"
)

// Keystore represents the keystore data structure
type Keystore struct {
	*models.Keystore
}

// KeystoreJSON represents the Ethereum keystore v3 JSON structure
//
//nolint:revive // KeystoreJSON is the standard name for Ethereum keystore JSON structure
type KeystoreJSON struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Crypto  struct {
		Ciphertext   string `json:"ciphertext"`
		CipherParams struct {
			IV string `json:"iv"`
		} `json:"cipherparams"`
		Cipher    string `json:"cipher"`
		KDF       string `json:"kdf"`
		KDFParams struct {
			DKLen int    `json:"dklen"`
			Salt  string `json:"salt"`
			N     int    `json:"n"`
			R     int    `json:"r"`
			P     int    `json:"p"`
		} `json:"kdfparams"`
		MAC string `json:"mac"`
	} `json:"crypto"`
}

// ScryptParams defines scrypt KDF parameters
type ScryptParams struct {
	DKLen int // Derived key length (32 bytes)
	Salt  []byte
	N     int // CPU/memory cost parameter (262144)
	R     int // Block size parameter (8)
	P     int // Parallelization parameter (1)
}

// DefaultScryptParams returns default scrypt parameters for Ethereum keystore v3
func DefaultScryptParams() *ScryptParams {
	const (
		scryptDKLen = 32     // Derived key length (32 bytes)
		scryptN     = 262144 // CPU/memory cost parameter (2^18)
		scryptR     = 8      // Block size parameter
		scryptP     = 1      // Parallelization parameter
	)

	return &ScryptParams{
		DKLen: scryptDKLen,
		N:     scryptN,
		R:     scryptR,
		P:     scryptP,
	}
}
