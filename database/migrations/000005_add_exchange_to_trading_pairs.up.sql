ALTER TABLE trading_pairs ADD COLUMN IF NOT EXISTS exchange VARCHAR(50) DEFAULT 'Binance';
ALTER TABLE trading_pairs DROP CONSTRAINT IF EXISTS unique_user_exchange_symbol;
ALTER TABLE trading_pairs ADD CONSTRAINT unique_user_exchange_symbol UNIQUE (user_id, exchange, symbol);
