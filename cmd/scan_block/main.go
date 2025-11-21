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

	_ "github.com/lib/pq"
	"github/chapool/go-wallet/internal/wallet/chain"
	"github/chapool/go-wallet/internal/wallet/deposit"
	"github/chapool/go-wallet/internal/wallet/scan"
)

func main() {
	var (
		chainID = flag.Int("chain", 97, "Chain ID (default: 97 for BSC Testnet)")
		blockNo = flag.Int64("block", 0, "Block number to scan")
	)
	flag.Parse()

	if *blockNo == 0 {
		fmt.Println("Error: block number is required")
		flag.Usage()
		os.Exit(1)
	}

	dbURL := mustDatabaseURL()
	ctx := context.Background()

	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize chain service
	chainService := chain.NewService(db)

	// Initialize deposit service
	depositService := deposit.NewService(db)

	// Initialize scan service
	scanService := scan.NewService(db, chainService, depositService, 0, 0)

	// Scan the block
	blockNumber := big.NewInt(*blockNo)
	fmt.Printf("Scanning block %d on chain %d...\n", *blockNo, *chainID)

	err = scanService.ScanChainBlock(ctx, *chainID, blockNumber)
	if err != nil {
		fmt.Printf("Error scanning block: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Block scanned successfully!")
}

func mustDatabaseURL() string {
	if val := os.Getenv("DATABASE_URL"); val != "" {
		return val
	}
	fmt.Fprintln(os.Stderr, "Error: DATABASE_URL environment variable is required")
	os.Exit(1)
	return ""
}
