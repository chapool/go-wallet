package withdraw

import "math/big"

// Request 提现请求参数
type Request struct {
	ToAddress string
	TokenID   int
	Amount    *big.Float
}
