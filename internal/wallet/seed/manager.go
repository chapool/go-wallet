package seed

import (
	"crypto/sha512"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// manager implements seed management with thread-safe access
type manager struct {
	seed        []byte
	mu          sync.RWMutex
	initialized bool
}

// NewManager creates a new SeedManager
//
//nolint:ireturn // Returning interface is intentional for dependency injection
func NewManager() Manager {
	return &manager{
		seed:        nil,
		initialized: false,
	}
}

// Initialize initializes the seed manager with mnemonic and password
// This converts mnemonic to seed using PBKDF2 (BIP39 standard)
func (m *manager) Initialize(mnemonic string, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Convert mnemonic to seed using PBKDF2
	// BIP39: seed = PBKDF2(mnemonic, "mnemonic" + password, 2048, 64, SHA512)
	const (
		pbkdf2Iterations = 2048 // BIP39 standard iterations
		pbkdf2KeyLength  = 64   // BIP39 standard key length (512 bits)
	)

	seed := pbkdf2.Key(
		[]byte(mnemonic),
		[]byte("mnemonic"+password),
		pbkdf2Iterations,
		pbkdf2KeyLength,
		sha512.New,
	)

	m.seed = seed
	m.initialized = true

	return nil
}

// GetSeed gets the seed (returns a copy to prevent external modification)
func (m *manager) GetSeed() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.initialized || m.seed == nil {
		return nil
	}

	// Return a copy to prevent external modification
	seedCopy := make([]byte, len(m.seed))
	copy(seedCopy, m.seed)
	return seedCopy
}

// IsInitialized checks if seed is initialized
func (m *manager) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.initialized
}

// Clear clears the seed from memory
func (m *manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.seed != nil {
		// Clear seed from memory
		for i := range m.seed {
			m.seed[i] = 0
		}
		m.seed = nil
	}
	m.initialized = false
}
