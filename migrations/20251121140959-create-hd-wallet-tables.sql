-- +migrate Up
-- Note: This migration depends on 20251121140958-create-wallet-enums.sql
-- Create chains table (链配置表) - must be created first as other tables may reference it
CREATE TABLE chains (
    id serial PRIMARY KEY,
    chain_id integer UNIQUE NOT NULL, -- 链ID：1(以太坊主网), 137(Polygon), 56(BSC), 42161(Arbitrum), 10(Optimism) 等
    chain_name varchar(100) NOT NULL, -- 链名称：Ethereum Mainnet, Polygon, BSC, Arbitrum One, Optimism 等
    chain_type varchar(50) NOT NULL, -- 固定为 'evm'
    rpc_url text NOT NULL, -- RPC 节点 URL（支持多个，用逗号分隔）
    explorer_url text, -- 区块浏览器 URL
    native_token_symbol varchar(50) NOT NULL, -- 原生代币符号：ETH, MATIC, BNB, ETH(Arbitrum), ETH(Optimism)
    block_time_seconds integer DEFAULT 12, -- 平均出块时间（秒）
    confirmation_blocks integer DEFAULT 12, -- 确认区块数（safe 状态）
    finalized_blocks integer DEFAULT 32, -- 终结区块数（finalized 状态）
    is_active boolean NOT NULL, -- 是否启用
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_chains_chain_id ON chains (chain_id);

CREATE INDEX idx_chains_is_active ON chains (is_active);

CREATE INDEX idx_chains_chain_type ON chains (chain_type);

-- Create keystore table (单一 Keystore 存储表)
CREATE TABLE keystore (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    keystore_data jsonb NOT NULL, -- 加密的助记词 keystore JSON 数据
    version integer NOT NULL, -- Keystore 版本
    cipher varchar(50) NOT NULL, -- 加密算法
    kdf varchar(50) NOT NULL, -- 密钥派生函数
    device_name varchar(255), -- 设备名称（可选）
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT keystore_single_row CHECK (id = '00000000-0000-0000-0000-000000000001'::uuid)
);

-- 确保只有一条记录
CREATE UNIQUE INDEX idx_keystore_single ON keystore ((1));

-- Create address_indexes table (地址索引表)
CREATE TABLE address_indexes (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    chain_type varchar(50) NOT NULL, -- 固定为 'evm'（当前仅支持 EVM）
    current_index integer NOT NULL, -- 当前最大索引（所有 EVM 链共享）
    device_name varchar(255), -- 设备名称（可选）
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT address_indexes_chain_device_unique UNIQUE (chain_type, device_name)
);

CREATE INDEX idx_address_indexes_chain_type ON address_indexes (chain_type);

-- Create wallets table (钱包信息表)
CREATE TABLE wallets (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    address varchar(255) NOT NULL,
    chain_type varchar(50) NOT NULL, -- 固定为 'evm'（当前仅支持 EVM）
    chain_id integer NOT NULL, -- 链ID：1(以太坊主网), 137(Polygon), 56(BSC), 42161(Arbitrum), 10(Optimism) 等
    derivation_path varchar(255) NOT NULL, -- BIP44 路径，所有 EVM 链都是 "m/44'/60'/0'/0/{index}"
    address_index integer NOT NULL, -- 地址索引（所有 EVM 链共享相同的索引空间）
    wallet_type varchar(50) NOT NULL, -- 'user', 'hot', 'cold'
    device_name varchar(255), -- 设备名称（可选）
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT wallets_user_chain_unique UNIQUE (user_id, chain_id)
);

CREATE INDEX idx_wallets_user_id ON wallets (user_id);

CREATE INDEX idx_wallets_address ON wallets (address);

CREATE INDEX idx_wallets_chain_id ON wallets (chain_id);

CREATE INDEX idx_wallets_user_chain ON wallets (user_id, chain_id);

-- Create tokens table (代币信息表)
CREATE TABLE tokens (
    id serial PRIMARY KEY,
    chain_type varchar(50) NOT NULL, -- 链类型：'evm'
    chain_id integer NOT NULL, -- 链ID：1(以太坊主网), 5(Goerli), 137(Polygon), 56(BSC) 等
    token_address varchar(255), -- 代币合约地址（原生代币为空）
    token_symbol varchar(50) NOT NULL, -- 代币符号：USDC/ETH/USDT 等
    token_name varchar(255), -- 代币名称
    decimals integer NOT NULL, -- 代币精度
    is_native boolean NOT NULL, -- 是否原生代币
    token_type varchar(50) DEFAULT 'erc20', -- 代币类型：'erc20', 'erc721', 'erc1155'
    withdraw_fee text DEFAULT '0', -- 提现手续费
    min_withdraw_amount text DEFAULT '0', -- 最小提现金额
    is_active boolean NOT NULL, -- 是否启用
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT tokens_chain_address_unique UNIQUE (chain_id, token_address)
);

CREATE INDEX idx_tokens_chain_id ON tokens (chain_id);

CREATE INDEX idx_tokens_chain_type ON tokens (chain_type);

CREATE INDEX idx_tokens_symbol ON tokens (token_symbol);

-- Create blocks table (区块扫描进度表)
CREATE TABLE blocks (
    hash varchar(255) NOT NULL, -- 区块哈希
    chain_id integer NOT NULL, -- 链ID
    parent_hash varchar(255) NOT NULL, -- 父区块哈希
    number bigint NOT NULL, -- 区块号
    timestamp bigint NOT NULL, -- 区块时间戳
    status block_status NOT NULL, -- 'confirmed', 'safe', 'finalized', 'orphaned'
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (hash, chain_id),
    CONSTRAINT blocks_hash_chain_unique UNIQUE (hash, chain_id)
);

CREATE INDEX idx_blocks_chain_id ON blocks (chain_id);

CREATE INDEX idx_blocks_number ON blocks (chain_id, number);

CREATE INDEX idx_blocks_parent_hash ON blocks (chain_id, parent_hash);

CREATE INDEX idx_blocks_status ON blocks (chain_id, status);

-- Create transactions table (交易记录表)
CREATE TABLE transactions (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    chain_id integer NOT NULL, -- 链ID
    block_hash varchar(255) NOT NULL,
    block_no bigint NOT NULL,
    tx_hash varchar(255) NOT NULL, -- 交易哈希（不同链可能相同，需要与 chain_id 组合唯一）
    from_addr varchar(255) NOT NULL,
    to_addr varchar(255) NOT NULL,
    token_addr varchar(255), -- ERC20 合约地址，原生代币为空
    amount text NOT NULL, -- 交易金额（字符串存储，避免精度丢失）
    type transaction_type NOT NULL, -- 'deposit', 'withdraw', 'collect', 'rebalance'
    status transaction_status NOT NULL, -- 'confirmed', 'safe', 'finalized', 'failed'
    confirmation_count integer DEFAULT 0, -- 确认数
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT transactions_tx_hash_chain_unique UNIQUE (tx_hash, chain_id)
);

CREATE INDEX idx_transactions_chain_id ON transactions (chain_id);

CREATE INDEX idx_transactions_block_no ON transactions (chain_id, block_no);

CREATE INDEX idx_transactions_tx_hash ON transactions (chain_id, tx_hash);

CREATE INDEX idx_transactions_to_addr ON transactions (chain_id, to_addr);

CREATE INDEX idx_transactions_status ON transactions (chain_id, status);

CREATE INDEX idx_transactions_type ON transactions (chain_id, type);

-- Create credits table (资金流水表)
CREATE TABLE credits (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    address varchar(255) NOT NULL, -- 钱包地址
    token_id integer NOT NULL REFERENCES tokens (id) ON DELETE RESTRICT, -- 代币ID（引用 tokens 表）
    token_symbol varchar(50) NOT NULL, -- 代币符号（冗余字段）
    amount text NOT NULL, -- 金额（正数入账、负数出账，字符串存储）
    credit_type credit_type NOT NULL, -- 'deposit', 'withdraw', 'collect', 'rebalance', 'freeze', 'unfreeze'
    business_type business_type NOT NULL, -- 'blockchain', 'internal_transfer', 'admin_adjust'
    reference_id text NOT NULL, -- 关联业务ID（如 txHash_eventIndex、withdraw_id）
    reference_type reference_type NOT NULL, -- 'blockchain_tx', 'withdraw', 'collect', 'rebalance'
    chain_id integer, -- 链ID
    chain_type varchar(50), -- 链类型：'evm'
    status credit_status NOT NULL, -- 'pending', 'confirmed', 'finalized', 'failed', 'frozen'
    block_number bigint, -- 区块号（链上交易）
    tx_hash varchar(255), -- 交易哈希（链上交易）
    event_index integer DEFAULT 0, -- 事件索引（ERC20 Transfer 事件的 logIndex）
    metadata jsonb, -- JSON格式的扩展信息
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT credits_user_reference_unique UNIQUE (user_id, reference_id, reference_type, event_index)
);

CREATE INDEX idx_credits_user_id ON credits (user_id);

CREATE INDEX idx_credits_token_id ON credits (token_id);

CREATE INDEX idx_credits_address ON credits (address);

CREATE INDEX idx_credits_status ON credits (status);

CREATE INDEX idx_credits_credit_type ON credits (credit_type);

CREATE INDEX idx_credits_tx_hash ON credits (tx_hash);

CREATE INDEX idx_credits_chain_id ON credits (chain_id);

CREATE INDEX idx_credits_chain_type ON credits (chain_type);

-- Create withdraws table (提现记录表)
CREATE TABLE withdraws (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_address varchar(255) NOT NULL, -- 提现目标地址
    from_address varchar(255), -- 热钱包地址（签名时填充）
    token_id integer NOT NULL REFERENCES tokens (id) ON DELETE RESTRICT, -- 代币ID
    amount text NOT NULL, -- 提现金额（字符串存储）
    fee text NOT NULL, -- 提现手续费
    chain_id integer NOT NULL, -- 链ID
    chain_type varchar(50) NOT NULL, -- 链类型：'evm'
    tx_hash varchar(255), -- 交易哈希（发送交易后填充）
    gas_price text, -- Gas 价格
    max_fee_per_gas text, -- EIP-1559 最大费用
    max_priority_fee_per_gas text, -- EIP-1559 优先费用
    gas_used text, -- Gas 使用量（确认后填充）
    nonce integer, -- 交易 nonce（签名时填充）
    status withdraw_status NOT NULL, -- 状态：'user_withdraw_request', 'signing', 'pending', 'processing', 'confirmed', 'failed'
    error_message text, -- 错误信息（失败时填充）
    operation_id uuid, -- 操作ID（用于风控和双重签名）
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_withdraws_user_id ON withdraws (user_id);

CREATE INDEX idx_withdraws_fk_token_id ON withdraws (token_id);

CREATE INDEX idx_withdraws_status ON withdraws (status);

CREATE INDEX idx_withdraws_tx_hash ON withdraws (tx_hash);

CREATE INDEX idx_withdraws_operation_id ON withdraws (operation_id);

CREATE INDEX idx_withdraws_chain_id ON withdraws (chain_id);

CREATE INDEX idx_withdraws_chain_type ON withdraws (chain_type);

-- Create wallet_nonces table (钱包 Nonce 管理表)
CREATE TABLE wallet_nonces (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4 (),
    address varchar(255) NOT NULL, -- 钱包地址
    chain_id integer NOT NULL, -- 链ID
    nonce integer NOT NULL DEFAULT 0, -- 当前 nonce 值
    last_used_at timestamptz, -- 最后使用时间
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT wallet_nonces_address_chain_unique UNIQUE (address, chain_id)
);

CREATE INDEX idx_wallet_nonces_address ON wallet_nonces (address);

CREATE INDEX idx_wallet_nonces_chain_id ON wallet_nonces (chain_id);

CREATE INDEX idx_wallet_nonces_last_used_at ON wallet_nonces (last_used_at);

-- +migrate Down
DROP TABLE IF EXISTS wallet_nonces;

DROP TABLE IF EXISTS withdraws;

DROP TABLE IF EXISTS credits;

DROP TABLE IF EXISTS transactions;

DROP TABLE IF EXISTS blocks;

DROP TABLE IF EXISTS tokens;

DROP TABLE IF EXISTS wallets;

DROP TABLE IF EXISTS address_indexes;

DROP TABLE IF EXISTS keystore;

DROP TABLE IF EXISTS chains;

