package signer

import "context"

// Service provides transaction signing functionality
type Service interface {
	// SignEVMTransaction signs an EVM transaction (EIP-1559)
	SignEVMTransaction(ctx context.Context, req *SignEVMRequest) (*SignEVMResponse, error)
}

// SignEVMRequest represents a request to sign an EVM transaction
type SignEVMRequest struct {
	ChainID              int64  // Chain ID (1 for Ethereum mainnet, 137 for Polygon, etc.)
	To                   string // Recipient address (hex string with 0x prefix)
	Value                string // Amount in wei (as string to avoid precision loss)
	GasLimit             uint64 // Gas limit
	MaxFeePerGas         string // Max fee per gas (EIP-1559, in wei, as string)
	MaxPriorityFeePerGas string // Max priority fee per gas (EIP-1559, in wei, as string)
	Nonce                uint64 // Transaction nonce
	Data                 []byte // Transaction data (for contract calls)
	FromAddress          string // Address to sign from (hex string with 0x prefix)
	DerivationPath       string // BIP44 derivation path (e.g., "m/44'/60'/0'/0/0")
}

// SignEVMResponse represents a signed EVM transaction
type SignEVMResponse struct {
	RawTransaction []byte // RLP-encoded signed transaction
	TxHash         string // Transaction hash (hex string with 0x prefix)
}
