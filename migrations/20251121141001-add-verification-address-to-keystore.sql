-- +migrate Up
-- Add verification_address field to keystore table for password verification
ALTER TABLE keystore
    ADD COLUMN verification_address VARCHAR(255);

CREATE INDEX idx_keystore_verification_address ON keystore (verification_address);

-- +migrate Down
DROP INDEX IF EXISTS idx_keystore_verification_address;

ALTER TABLE keystore
    DROP COLUMN IF EXISTS verification_address;

