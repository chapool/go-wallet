-- +migrate Up
-- Insert BSC Testnet native token (tBNB)
-- Note: token_address is omitted for native tokens (will be empty)
INSERT INTO tokens (chain_type, chain_id, token_symbol, token_name, decimals, is_native, token_type, withdraw_fee, min_withdraw_amount, is_active)
    VALUES ('evm', 97, 'tBNB', 'BSC Testnet Native Token', 18, TRUE, 'erc20', '0', '0', TRUE);

-- Insert BSC Testnet Mock USDT (mUSDT)
INSERT INTO tokens (chain_type, chain_id, token_address, token_symbol, token_name, decimals, is_native, token_type, withdraw_fee, min_withdraw_amount, is_active)
    VALUES ('evm', 97, '0x312fc28767329faf567f3ad61943b447a53d09d6', 'mUSDT', 'Mock USDT', 6, FALSE, 'erc20', '0', '0', TRUE);

-- +migrate Down
DELETE FROM tokens
WHERE chain_id = 97
    AND ((is_native = TRUE
            AND token_symbol = 'tBNB')
        OR (token_address = '0x312fc28767329faf567f3ad61943b447a53d09d6'));

