package collect

import "math/big"

// Request represents a manual collect request.
type Request struct {
	WalletID string
	ChainID  int
	TokenID  int
	Amount   *big.Int // Amount in wei
}
