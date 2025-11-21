package scan

import (
	"context"
	"math/big"
)

// Service 定义区块链扫描服务接口
type Service interface {
	// StartMultiChainScan 启动多链并发扫描
	StartMultiChainScan(ctx context.Context) error

	// StartChainScan 启动指定链的扫描
	StartChainScan(ctx context.Context, chainID int) error

	// ScanChainBlock 扫描指定链的单个区块
	ScanChainBlock(ctx context.Context, chainID int, blockNumber *big.Int) error

	// GetScanProgress 获取扫描进度
	GetScanProgress(ctx context.Context, chainID int) (*ScanProgress, error)
}

// Progress 扫描进度
//
//nolint:revive // Keep ScanProgress for backward compatibility
type ScanProgress struct {
	ChainID     int
	LatestBlock *big.Int
	ScannedTo   *big.Int
	Status      string
}

// BlockInfo 区块信息
type BlockInfo struct {
	Hash       string
	ChainID    int
	ParentHash string
	Number     *big.Int
	Timestamp  int64
}
