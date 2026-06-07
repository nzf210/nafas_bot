ALTER TABLE trading_pairs DROP CONSTRAINT IF EXISTS unique_user_exchange_symbol;
ALTER TABLE trading_pairs DROP COLUMN IF EXISTS exchange;
