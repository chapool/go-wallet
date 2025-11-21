package seed

// Manager provides seed management functionality
type Manager interface {
	// Initialize initializes the seed manager (called at startup)
	Initialize(mnemonic string, password string) error

	// GetSeed gets the seed (from memory)
	GetSeed() []byte

	// IsInitialized checks if seed is initialized
	IsInitialized() bool

	// Clear clears the seed from memory
	Clear()
}
