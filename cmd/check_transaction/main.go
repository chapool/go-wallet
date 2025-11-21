//go:build tools
// +build tools

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/lib/pq"
)

func main() {
	var (
		txHash    = flag.String("tx", "", "Transaction hash to check")
		chainID   = flag.Int("chain", 97, "Chain ID (default: 97 for BSC Testnet)")
		userID    = flag.String("user", "", "User ID to check")
		toAddress = flag.String("to", "", "To address to check")
		rpcURL    = flag.String("rpc", "", "RPC URL for blockchain")
	)
	flag.Parse()

	if *txHash == "" {
		fmt.Println("Error: transaction hash is required")
		flag.Usage()
		os.Exit(1)
	}

	dbURL := mustDatabaseURL()
	if *rpcURL == "" {
		// Default BSC Testnet RPC
		*rpcURL = "https://data-seed-prebsc-1-s1.binance.org:8545"
	}

	ctx := context.Background()

	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Connect to RPC
	client, err := ethclient.Dial(*rpcURL)
	if err != nil {
		fmt.Printf("Error connecting to RPC: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Get transaction
	txHashBytes := common.HexToHash(*txHash)
	tx, isPending, err := client.TransactionByHash(ctx, txHashBytes)
	if err != nil {
		fmt.Printf("Error getting transaction: %v\n", err)
		os.Exit(1)
	}

	if isPending {
		fmt.Println("Transaction is still pending")
		os.Exit(1)
	}

	// Get receipt
	receipt, err := client.TransactionReceipt(ctx, txHashBytes)
	if err != nil {
		fmt.Printf("Error getting receipt: %v\n", err)
		os.Exit(1)
	}

	// Get block
	block, err := client.BlockByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		fmt.Printf("Error getting block: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Transaction Hash: %s\n", *txHash)
	fmt.Printf("Chain ID: %d\n", *chainID)
	fmt.Printf("Block Number: %s\n", receipt.BlockNumber.String())
	fmt.Printf("Block Hash: %s\n", block.Hash().Hex())
	fmt.Printf("Status: %s\n", receipt.Status)
	fmt.Println()

	// Check if block is scanned
	var blockCount int64
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM blocks 
		WHERE chain_id = $1 AND number = $2
	`, *chainID, receipt.BlockNumber.Int64()).Scan(&blockCount)

	if err != nil {
		fmt.Printf("Error checking block in database: %v\n", err)
	} else {
		if blockCount > 0 {
			fmt.Println("✅ Block exists in database")
		} else {
			fmt.Printf("❌ Block %s does NOT exist in database\n", receipt.BlockNumber.String())
			fmt.Printf("   You can manually scan this block using:\n")
			fmt.Printf("   go run cmd/scan_block/main.go -chain %d -block %s -db \"%s\"\n",
				*chainID, receipt.BlockNumber.String(), *dbURL)
		}
	}
	fmt.Println()

	// Check if user has wallet
	if *userID != "" {
		var walletAddress string
		err = db.QueryRowContext(ctx, `
			SELECT address 
			FROM wallets 
			WHERE user_id = $1 AND chain_id = $2
		`, *userID, *chainID).Scan(&walletAddress)

		if err != nil {
			if err == sql.ErrNoRows {
				fmt.Printf("❌ User %s does NOT have a wallet on chain %d\n", *userID, *chainID)
			} else {
				fmt.Printf("Error checking wallet: %v\n", err)
			}
		} else {
			fmt.Printf("✅ User %s has wallet: %s\n", *userID, walletAddress)
			walletAddressLower := strings.ToLower(walletAddress)

			// Check transaction
			if *toAddress != "" {
				toAddressLower := strings.ToLower(*toAddress)
				if walletAddressLower == toAddressLower {
					fmt.Printf("✅ Wallet address matches transaction 'to' address\n")
				} else {
					fmt.Printf("❌ Wallet address does NOT match transaction 'to' address\n")
					fmt.Printf("   Wallet: %s\n", walletAddressLower)
					fmt.Printf("   Transaction To: %s\n", toAddressLower)
				}
			}
		}
		fmt.Println()
	}

	// Analyze transaction
	fmt.Println("Analyzing transaction...")

	// Get sender
	chainIDBig := big.NewInt(int64(*chainID))
	from, err := types.Sender(types.LatestSignerForChainID(chainIDBig), tx)
	if err != nil {
		fmt.Printf("Error getting sender: %v\n", err)
	} else {
		fmt.Printf("From: %s\n", strings.ToLower(from.Hex()))
	}

	to := tx.To()
	if to != nil {
		fmt.Printf("To: %s\n", strings.ToLower(to.Hex()))
	} else {
		fmt.Println("To: Contract Creation")
	}

	fmt.Printf("Value: %s wei\n", tx.Value().String())
	fmt.Println()

	// Check ERC20 transfers
	fmt.Println("Checking ERC20 Transfer events...")
	transferEventSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	foundERC20 := false

	for i, logEntry := range receipt.Logs {
		if len(logEntry.Topics) < 3 {
			continue
		}

		if logEntry.Topics[0] == transferEventSig {
			foundERC20 = true
			fromAddr := common.BytesToAddress(logEntry.Topics[1].Bytes())
			toAddr := common.BytesToAddress(logEntry.Topics[2].Bytes())
			amount := new(big.Int).SetBytes(logEntry.Data)
			tokenAddr := logEntry.Address.Hex()

			fmt.Printf("  Log #%d: ERC20 Transfer\n", i)
			fmt.Printf("    Token: %s\n", strings.ToLower(tokenAddr))
			fmt.Printf("    From: %s\n", strings.ToLower(fromAddr.Hex()))
			fmt.Printf("    To: %s\n", strings.ToLower(toAddr.Hex()))
			fmt.Printf("    Amount: %s\n", amount.String())

			// Check if token is mUSDT
			if strings.ToLower(tokenAddr) == "0x312fc28767329faf567f3ad61943b447a53d09d6" {
				fmt.Printf("    ✅ This is mUSDT!\n")
			}

			// Check if 'to' address is user wallet
			if *userID != "" {
				var walletAddress string
				err = db.QueryRowContext(ctx, `
					SELECT address 
					FROM wallets 
					WHERE user_id = $1 AND chain_id = $2
				`, *userID, *chainID).Scan(&walletAddress)

				if err == nil {
					walletAddressLower := strings.ToLower(walletAddress)
					toAddrLower := strings.ToLower(toAddr.Hex())
					if walletAddressLower == toAddrLower {
						fmt.Printf("    ✅ 'To' address matches user wallet!\n")
					} else {
						fmt.Printf("    ❌ 'To' address does NOT match user wallet\n")
						fmt.Printf("       Wallet: %s\n", walletAddressLower)
						fmt.Printf("       Transfer To: %s\n", toAddrLower)
					}
				}
			}
			fmt.Println()
		}
	}

	if !foundERC20 {
		fmt.Println("  No ERC20 Transfer events found")
		fmt.Println()
	}

	// Check if transaction exists in database
	var txCount int64
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM transactions 
		WHERE chain_id = $1 AND tx_hash = $2
	`, *chainID, strings.ToLower(*txHash)).Scan(&txCount)

	if err != nil {
		fmt.Printf("Error checking transaction in database: %v\n", err)
	} else {
		if txCount > 0 {
			fmt.Println("✅ Transaction exists in database")
		} else {
			fmt.Println("❌ Transaction does NOT exist in database")
			fmt.Println("   This transaction may not have been scanned yet.")
		}
	}
}

func mustDatabaseURL() string {
	if val := os.Getenv("DATABASE_URL"); val != "" {
		return val
	}
	fmt.Fprintln(os.Stderr, "Error: DATABASE_URL environment variable is required")
	os.Exit(1)
	return ""
}
