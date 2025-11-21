-- +migrate Up
-- Insert Ethereum Mainnet configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (1, 'Ethereum Mainnet', 'evm', 'https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY,https://mainnet.infura.io/v3/YOUR_PROJECT_ID', 'https://etherscan.io', 'ETH', 12, 12, 32, TRUE);

-- Insert Polygon configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (137, 'Polygon', 'evm', 'https://polygon-rpc.com,https://rpc.ankr.com/polygon', 'https://polygonscan.com', 'MATIC', 2, 12, 32, TRUE);

-- Insert BSC configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (56, 'BSC', 'evm', 'https://bsc-dataseed.binance.org,https://bsc-dataseed1.defibit.io', 'https://bscscan.com', 'BNB', 3, 12, 32, TRUE);

-- Insert Arbitrum One configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (42161, 'Arbitrum One', 'evm', 'https://arb1.arbitrum.io/rpc,https://arbitrum-mainnet.infura.io/v3/YOUR_PROJECT_ID', 'https://arbiscan.io', 'ETH', 1, 12, 32, TRUE);

-- Insert Optimism configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (10, 'Optimism', 'evm', 'https://mainnet.optimism.io,https://optimism-mainnet.infura.io/v3/YOUR_PROJECT_ID', 'https://optimistic.etherscan.io', 'ETH', 2, 12, 32, TRUE);

-- Insert Avalanche C-Chain configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (43114, 'Avalanche C-Chain', 'evm', 'https://api.avax.network/ext/bc/C/rpc,https://avalanche-mainnet.infura.io/v3/YOUR_PROJECT_ID', 'https://snowtrace.io', 'AVAX', 2, 12, 32, TRUE);

-- Insert Base configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (8453, 'Base', 'evm', 'https://mainnet.base.org,https://base-mainnet.infura.io/v3/YOUR_PROJECT_ID', 'https://basescan.org', 'ETH', 2, 12, 32, TRUE);

-- Insert BSC Testnet configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (97, 'BSC Testnet', 'evm', 'https://data-seed-prebsc-1-s1.binance.org:8545,https://data-seed-prebsc-2-s1.binance.org:8545,https://bsc-testnet.public.blastapi.io', 'https://testnet.bscscan.com', 'BNB', 3, 12, 32, FALSE);

-- Insert Sepolia Testnet configuration
INSERT INTO chains (chain_id, chain_name, chain_type, rpc_url, explorer_url, native_token_symbol, block_time_seconds, confirmation_blocks, finalized_blocks, is_active)
    VALUES (11155111, 'Sepolia', 'evm', 'https://sepolia.infura.io/v3/YOUR_PROJECT_ID,https://rpc.sepolia.org,https://ethereum-sepolia-rpc.publicnode.com', 'https://sepolia.etherscan.io', 'ETH', 12, 12, 32, FALSE);

-- +migrate Down
DELETE FROM chains
WHERE chain_id IN (1, 137, 56, 42161, 10, 43114, 8453, 97, 11155111);

