-- +migrate Up
-- Remove the unique constraint that prevents users from having multiple wallets on the same chain
-- This allows users to create multiple wallets (e.g., multiple hot wallets, or multiple user wallets)
ALTER TABLE wallets
    DROP CONSTRAINT IF EXISTS wallets_user_chain_unique;

-- Add a unique constraint on address and chain_id to ensure address uniqueness per chain
-- This prevents duplicate addresses on the same chain
ALTER TABLE wallets
    ADD CONSTRAINT wallets_address_chain_unique UNIQUE (address, chain_id);

-- +migrate Down
-- Restore the original constraint
ALTER TABLE wallets
    DROP CONSTRAINT IF EXISTS wallets_address_chain_unique;

ALTER TABLE wallets
    ADD CONSTRAINT wallets_user_chain_unique UNIQUE (user_id, chain_id);

