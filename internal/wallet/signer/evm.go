package signer

import (
	"context"
	"crypto/ecdsa"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
)

// signEIP1559Transaction signs an EIP-1559 transaction
func (s *service) signEIP1559Transaction(_ context.Context, req *SignEVMRequest, privateKey []byte) (*SignEVMResponse, error) {
	// Convert private key to ECDSA
	ecdsaPrivateKey, err := crypto.ToECDSA(privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert private key to ECDSA")
	}

	// Parse addresses
	toAddress := common.HexToAddress(req.To)
	fromAddress := common.HexToAddress(req.FromAddress)

	// Verify from address matches private key
	publicKey := ecdsaPrivateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("failed to cast public key to ECDSA")
	}

	derivedAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	if derivedAddress != fromAddress {
		return nil, errors.New("from address does not match private key")
	}

	// Parse value
	const base10 = 10
	value, ok := new(big.Int).SetString(req.Value, base10)
	if !ok {
		return nil, errors.New("invalid value format")
	}

	// Parse max fee per gas
	maxFeePerGas, ok := new(big.Int).SetString(req.MaxFeePerGas, base10)
	if !ok {
		return nil, errors.New("invalid maxFeePerGas format")
	}

	// Parse max priority fee per gas
	maxPriorityFeePerGas, ok := new(big.Int).SetString(req.MaxPriorityFeePerGas, base10)
	if !ok {
		return nil, errors.New("invalid maxPriorityFeePerGas format")
	}

	// Create EIP-1559 transaction
	//nolint:varnamelen // tx is a common abbreviation for transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(req.ChainID),
		Nonce:     req.Nonce,
		GasTipCap: maxPriorityFeePerGas,
		GasFeeCap: maxFeePerGas,
		Gas:       req.GasLimit,
		To:        &toAddress,
		Value:     value,
		Data:      req.Data,
	})

	// Sign transaction
	signer := types.NewLondonSigner(big.NewInt(req.ChainID))
	signedTx, err := types.SignTx(tx, signer, ecdsaPrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to sign transaction")
	}

	// Encode transaction to RLP
	txBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal transaction")
	}

	// Get transaction hash
	txHash := signedTx.Hash()

	return &SignEVMResponse{
		RawTransaction: txBytes,
		TxHash:         txHash.Hex(),
	}, nil
}
