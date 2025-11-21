//go:build tools
// +build tools

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/models"
)

func main() {
	var (
		txHash  = flag.String("tx", "", "Transaction hash")
		chainID = flag.Int("chain", 97, "Chain ID")
	)
	flag.Parse()

	if *txHash == "" {
		fmt.Fprintf(os.Stderr, "Error: transaction hash is required\n")
		flag.Usage()
		os.Exit(1)
	}

	dbURL := mustDatabaseURL()
	ctx := context.Background()

	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Get transaction
	txHashLower := strings.ToLower(*txHash)
	transaction, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(*chainID),
		models.TransactionWhere.TXHash.EQ(txHashLower),
	).One(ctx, db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "Transaction not found: %s\n", *txHash)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error getting transaction: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found transaction: %s\n", transaction.TXHash)
	fmt.Printf("  Chain ID: %d\n", transaction.ChainID)
	fmt.Printf("  To Address: %s\n", transaction.ToAddr)
	fmt.Printf("  Amount: %s\n", transaction.Amount)
	fmt.Printf("  Token Address: %s\n", transaction.TokenAddr.String)
	fmt.Println()

	// Check if credit already exists
	var creditCount int64
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM credits 
		WHERE chain_id = $1 AND tx_hash = $2
	`, *chainID, txHashLower).Scan(&creditCount)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking credit existence: %v\n", err)
		os.Exit(1)
	}

	if creditCount > 0 {
		fmt.Println("Credit already exists for this transaction")
		os.Exit(0)
	}

	// Get wallet
	var walletUserID, walletAddress string
	err = db.QueryRowContext(ctx, `
		SELECT user_id, address 
		FROM wallets 
		WHERE chain_id = $1 AND LOWER(address) = $2
	`, *chainID, strings.ToLower(transaction.ToAddr)).Scan(&walletUserID, &walletAddress)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "Wallet not found for address %s on chain %d\n", transaction.ToAddr, *chainID)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error getting wallet: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found wallet: %s (User ID: %s)\n", walletAddress, walletUserID)

	// Get token info
	tokenAddr := transaction.TokenAddr.String
	var tokenID int
	var tokenSymbol string

	if tokenAddr == "" {
		// Native token
		err = db.QueryRowContext(ctx, `
			SELECT id, token_symbol 
			FROM tokens 
			WHERE chain_id = $1 AND is_native = TRUE
		`, *chainID).Scan(&tokenID, &tokenSymbol)
	} else {
		// ERC20 token
		err = db.QueryRowContext(ctx, `
			SELECT id, token_symbol 
			FROM tokens 
			WHERE chain_id = $1 AND LOWER(token_address) = $2
		`, *chainID, strings.ToLower(tokenAddr)).Scan(&tokenID, &tokenSymbol)
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "Token not found: chain_id=%d, token_addr=%s\n", *chainID, tokenAddr)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error getting token: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found token: %s (ID: %d)\n", tokenSymbol, tokenID)
	fmt.Println()

	// Create credit
	credit := &models.Credit{
		UserID:        walletUserID,
		Address:       strings.ToLower(transaction.ToAddr),
		TokenID:       tokenID,
		TokenSymbol:   tokenSymbol,
		Amount:        transaction.Amount,
		CreditType:    "deposit",
		BusinessType:  "blockchain",
		ReferenceID:   transaction.ID,
		ReferenceType: "blockchain_tx",
		ChainID:       null.IntFrom(*chainID),
		ChainType:     null.StringFrom("evm"),
		Status:        "confirmed",
		BlockNumber:   null.Int64From(transaction.BlockNo),
		TXHash:        null.StringFrom(transaction.TXHash),
		EventIndex:    null.Int{}, // 暂时设为空
	}

	// For ERC20 transfers, try to get event index from transaction
	// (This is a simplified version, in production you might want to query the receipt)
	if tokenAddr != "" {
		credit.EventIndex = null.IntFrom(0) // Default to 0 for ERC20
	}

	if err := credit.Insert(ctx, db, boil.Infer()); err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			fmt.Println("Credit already exists (concurrent insert)")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error creating credit: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Credit created successfully!\n")
	fmt.Printf("  Credit ID: %s\n", credit.ID)
	fmt.Printf("  User ID: %s\n", credit.UserID)
	fmt.Printf("  Token: %s\n", credit.TokenSymbol)
	fmt.Printf("  Amount: %s\n", credit.Amount)
}
func mustDatabaseURL() string {
	if val := os.Getenv("DATABASE_URL"); val != "" {
		return val
	}
	fmt.Fprintln(os.Stderr, "Error: DATABASE_URL environment variable is required")
	os.Exit(1)
	return ""
}
