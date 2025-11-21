-- +migrate Up
-- Block status enum
CREATE TYPE block_status AS ENUM (
    'confirmed',
    'safe',
    'finalized',
    'orphaned'
);

-- Transaction status enum
CREATE TYPE transaction_status AS ENUM (
    'confirmed',
    'safe',
    'finalized',
    'failed'
);

-- Transaction type enum
CREATE TYPE transaction_type AS ENUM (
    'deposit',
    'withdraw',
    'collect',
    'rebalance'
);

-- Credit status enum
CREATE TYPE credit_status AS ENUM (
    'pending',
    'confirmed',
    'finalized',
    'failed',
    'frozen'
);

-- Credit type enum
CREATE TYPE credit_type AS ENUM (
    'deposit',
    'withdraw',
    'collect',
    'rebalance',
    'freeze',
    'unfreeze'
);

-- Business type enum
CREATE TYPE business_type AS ENUM (
    'blockchain',
    'internal_transfer',
    'admin_adjust'
);

-- Reference type enum
CREATE TYPE reference_type AS ENUM (
    'blockchain_tx',
    'withdraw',
    'collect',
    'rebalance'
);

-- Withdraw status enum
CREATE TYPE withdraw_status AS ENUM (
    'user_withdraw_request',
    'signing',
    'pending',
    'processing',
    'confirmed',
    'failed'
);

-- +migrate Down
DROP TYPE IF EXISTS withdraw_status;

DROP TYPE IF EXISTS reference_type;

DROP TYPE IF EXISTS business_type;

DROP TYPE IF EXISTS credit_type;

DROP TYPE IF EXISTS credit_status;

DROP TYPE IF EXISTS transaction_type;

DROP TYPE IF EXISTS transaction_status;

DROP TYPE IF EXISTS block_status;

