package scan

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// RPCClient 封装以太坊 RPC 客户端，支持多个 URL 和故障转移
type RPCClient struct {
	urls    []string
	clients []*ethclient.Client
	mu      sync.RWMutex
	current int // 当前使用的客户端索引
}

// NewRPCClient 创建新的 RPC 客户端
func NewRPCClient(urls []string) (*RPCClient, error) {
	if len(urls) == 0 {
		return nil, errors.New("at least one RPC URL is required")
	}

	clients := make([]*ethclient.Client, 0, len(urls))
	for _, url := range urls {
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Warn().
				Str("url", url).
				Err(err).
				Msg("Failed to connect to RPC node, will retry on use")
			// 继续尝试其他 URL，不立即失败
			clients = append(clients, nil)
			continue
		}
		clients = append(clients, client)
	}

	if len(clients) == 0 || allClientsNil(clients) {
		return nil, errors.New("failed to connect to any RPC node")
	}

	return &RPCClient{
		urls:    urls,
		clients: clients,
		current: 0,
	}, nil
}

// allClientsNil 检查所有客户端是否都是 nil
func allClientsNil(clients []*ethclient.Client) bool {
	for _, client := range clients {
		if client != nil {
			return false
		}
	}
	return true
}

// Close 关闭所有客户端连接
func (c *RPCClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, client := range c.clients {
		if client != nil {
			client.Close()
		}
	}
}

// GetLatestBlockNumber 获取最新区块号
func (c *RPCClient) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	client, err := c.getClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get RPC client")
	}

	blockNumber, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest block number")
	}

	// Check for overflow before conversion (int64 max value is 9223372036854775807)
	const maxInt64 = 9223372036854775807
	if blockNumber > maxInt64 {
		return nil, errors.New("block number exceeds int64 maximum")
	}

	return big.NewInt(int64(blockNumber)), nil
}

// GetBlockByNumber 根据区块号获取区块
func (c *RPCClient) GetBlockByNumber(ctx context.Context, blockNumber *big.Int) (*types.Block, error) {
	client, err := c.getClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get RPC client")
	}

	block, err := client.BlockByNumber(ctx, blockNumber)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get block by number")
	}

	return block, nil
}

// GetTransactionReceipt 获取交易回执
func (c *RPCClient) GetTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	client, err := c.getClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get RPC client")
	}

	receipt, err := client.TransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get transaction receipt")
	}

	return receipt, nil
}

// GetChainID 获取链 ID
func (c *RPCClient) GetChainID(ctx context.Context) (*big.Int, error) {
	client, err := c.getClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get RPC client")
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get chain ID")
	}

	return chainID, nil
}

// FilterLogs 过滤日志（用于 ERC20 转账事件）
func (c *RPCClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	client, err := c.getClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get RPC client")
	}

	logs, err := client.FilterLogs(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter logs")
	}

	return logs, nil
}

// getClient 获取当前可用的客户端，如果失败则尝试下一个
func (c *RPCClient) getClient(ctx context.Context) (*ethclient.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 尝试从当前索引开始
	for i := 0; i < len(c.clients); i++ {
		idx := (c.current + i) % len(c.clients)
		client := c.clients[idx]

		if client != nil {
			// 简单健康检查：尝试获取链 ID
			_, err := client.ChainID(ctx)
			if err == nil {
				// 更新当前索引
				if idx != c.current {
					c.mu.RUnlock()
					c.mu.Lock()
					c.current = idx
					c.mu.Unlock()
					c.mu.RLock()
				}
				return client, nil
			}

			// 连接失败，尝试重新连接
			log.Warn().
				Str("url", c.urls[idx]).
				Err(err).
				Msg("RPC client health check failed, will try to reconnect")
		}

		// 尝试重新连接
		c.mu.RUnlock()
		c.mu.Lock()
		if c.clients[idx] == nil {
			client, err := ethclient.Dial(c.urls[idx])
			if err == nil {
				c.clients[idx] = client
				c.current = idx
				c.mu.Unlock()
				c.mu.RLock()
				return client, nil
			}
		}
		c.mu.Unlock()
		c.mu.RLock()
	}

	return nil, errors.New("all RPC clients are unavailable")
}

// WithTimeout 为操作添加超时控制
func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}
