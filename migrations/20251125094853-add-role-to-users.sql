-- +migrate Up
-- Add role column to users table
-- Role can be 'admin' or 'user', default IS 'user' (set in application layer)
-- Note: Using DEFAULT '' to comply with SQLBoiler requirements (empty string for NOT NULL strings)
ALTER TABLE users
ADD COLUMN role VARCHAR(50) NOT NULL DEFAULT '';
-- Update existing users to have 'user' role
UPDATE
    users
SET
    ROLE = 'user'
WHERE
    ROLE = '';

-- Create index for role column for faster queries
CREATE INDEX idx_users_role ON users (ROLE);

-- Add constraint to ensure role is either 'admin' or 'user'
ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (ROLE IN ('admin', 'user'));

-- +migrate Down
DROP INDEX IF EXISTS idx_users_role;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users
    DROP COLUMN IF EXISTS ROLE;

