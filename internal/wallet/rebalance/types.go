package rebalance

import "math/big"

// Request represents a manual rebalance operation between hot wallets.
type Request struct {
	ChainID     int
	FromAddress string
	ToAddress   string
	Amount      *big.Int // Amount in wei
}
