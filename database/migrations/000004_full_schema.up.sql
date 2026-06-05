-- ============================================================
-- NAFAS BOT FULL SCHEMA V1.0
-- Migration: 000004_full_schema
-- Deskripsi: Schema lengkap sesuai DATABASE.md V1.0
-- ============================================================

-- Extension UUID
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
--1. USERS
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    telegram_id BIGINT UNIQUE NOT NULL,
    username VARCHAR(100),
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    status VARCHAR(20) DEFAULT 'active',
    wch_balance DECIMAL(36, 18) DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);

-- ============================================================
-- 2. API& EXCHANGE
-- ============================================================
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exchange VARCHAR(50) NOT NULL,
    encrypted_api_key TEXT NOT NULL,
    encrypted_api_secret TEXT NOT NULL,
    encrypted_passphrase TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, exchange)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_exchange ON api_keys(exchange);

CREATE TABLE IF NOT EXISTS exchange_accounts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exchange VARCHAR(50) NOT NULL,
    account_type VARCHAR(50) DEFAULT 'main',
    is_active BOOLEAN DEFAULT true,
    last_sync_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_exchange_accounts_user_id ON exchange_accounts(user_id);

-- ============================================================
-- 3. CONFIGURATION
-- ============================================================
CREATE TABLE IF NOT EXISTS system_settings (
    key VARCHAR PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    max_risk_per_trade DECIMAL(10, 4) DEFAULT 1.00,
    daily_loss_limit DECIMAL(18, 8) DEFAULT 5.00,
    max_open_positions INTEGER DEFAULT 3,
    notify_on_trade BOOLEAN DEFAULT true,
    notify_on_error BOOLEAN DEFAULT true,
    daily_report_time TIME DEFAULT '00:00:00',
    auto_trade_enabled BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS trading_pairs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    symbol VARCHAR(20) NOT NULL,
    base_asset VARCHAR(20) NOT NULL,
    quote_asset VARCHAR(20) NOT NULL,
    priority INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_trading_pairs_user_id ON trading_pairs(user_id);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_symbol ON trading_pairs(symbol);

CREATE TABLE IF NOT EXISTS asset_targets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset VARCHAR(20) NOT NULL,
    target_amount DECIMAL(36, 18) NOT NULL,
    current_amount DECIMAL(36, 18) DEFAULT 0,
    priority INTEGER DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 4. MARKET DATA
-- ============================================================
CREATE TABLE IF NOT EXISTS market_candles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    symbol VARCHAR(20) NOT NULL,
    interval VARCHAR(10) NOT NULL,
    open DECIMAL(36, 18) NOT NULL,
    high DECIMAL(36, 18) NOT NULL,
    low DECIMAL(36, 18) NOT NULL,
    close DECIMAL(36, 18) NOT NULL,
    volume DECIMAL(36, 18) NOT NULL,
    candle_time TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_market_candles_symbol_interval_time
    ON market_candles(symbol, interval, candle_time);
CREATE INDEX IF NOT EXISTS idx_market_candles_candle_time
    ON market_candles(candle_time);

CREATE TABLE IF NOT EXISTS market_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    symbol VARCHAR(20) UNIQUE NOT NULL,
    last_price DECIMAL(36, 18) NOT NULL,
    volume_24h DECIMAL(36, 18),
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_market_snapshots_symbol ON market_snapshots(symbol);

-- ============================================================
-- 5. SIGNAL ENGINE
-- ============================================================
CREATE TABLE IF NOT EXISTS signal_history (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    symbol VARCHAR(20) NOT NULL,
    rsi DECIMAL(10, 4),
    macd DECIMAL(18, 8),
    volume_score DECIMAL(10, 4),
    trend_score DECIMAL(10, 4),
    momentum_score DECIMAL(10, 4),
    signal_strength DECIMAL(10, 4),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_signal_history_symbol_time
    ON signal_history(symbol, created_at DESC);

-- ============================================================
-- 6. STRATEGIES
-- ============================================================
CREATE TABLE IF NOT EXISTS strategies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    parameters JSONB NOT NULL,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS strategy_runs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    strategy_id UUID REFERENCES strategies(id),
    symbol VARCHAR(20) NOT NULL,
    decision_source VARCHAR(30) NOT NULL,
    status VARCHAR(20) NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_strategy_runs_strategy_id ON strategy_runs(strategy_id);
CREATE INDEX IF NOT EXISTS idx_strategy_runs_symbol ON strategy_runs(symbol);
CREATE INDEX IF NOT EXISTS idx_strategy_runs_status ON strategy_runs(status);

-- ============================================================
-- 7. ORDERS
-- ============================================================
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exchange VARCHAR(50) NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    side VARCHAR(10) NOT NULL,
    order_type VARCHAR(20) NOT NULL,
    price DECIMAL(36, 18),
    quantity DECIMAL(36, 18) NOT NULL,
    executed_quantity DECIMAL(36, 18) DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    exchange_order_id VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_symbol ON orders(symbol);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_exchange_order_id ON orders(exchange_order_id);

CREATE TABLE IF NOT EXISTS trade_executions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    price DECIMAL(36, 18) NOT NULL,
    quantity DECIMAL(36, 18) NOT NULL,
    fee DECIMAL(36, 18),
    fee_asset VARCHAR(20),
    executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 8. PORTFOLIO
-- ============================================================
CREATE TABLE IF NOT EXISTS asset_inventory (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset VARCHAR(20) NOT NULL,
    balance DECIMAL(36, 18) DEFAULT 0,
    locked_balance DECIMAL(36, 18) DEFAULT 0,
    btc_equivalent DECIMAL(36, 18),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, asset)
);

CREATE INDEX IF NOT EXISTS idx_asset_inventory_user_id ON asset_inventory(user_id);

-- ============================================================
-- 9. BTC TREASURY
-- ============================================================
CREATE TABLE IF NOT EXISTS btc_treasury_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    btc_balance DECIMAL(36, 18) NOT NULL,
    btc_locked DECIMAL(36, 18) DEFAULT 0,
    btc_total DECIMAL(36, 18) NOT NULL,
    usd_value DECIMAL(36, 18),
    snapshot_time TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_btc_treasury_user_time
    ON btc_treasury_snapshots(user_id, snapshot_time DESC);

CREATE TABLE IF NOT EXISTS btc_accumulation_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source VARCHAR(50) NOT NULL,
    asset VARCHAR(20) NOT NULL,
    amount_asset DECIMAL(36, 18) NOT NULL,
    btc_received DECIMAL(36, 18) NOT NULL,
    fee_btc DECIMAL(36, 18) DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_btc_accumulation_user_time
    ON btc_accumulation_ledger(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_btc_accumulation_source
    ON btc_accumulation_ledger(source);

CREATE TABLE IF NOT EXISTS btc_accumulation_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period VARCHAR(20) NOT NULL,
    starting_btc DECIMAL(36, 18) NOT NULL,
    ending_btc DECIMAL(36, 18) NOT NULL,
    btc_growth DECIMAL(36, 18) NOT NULL,
    growth_percent DECIMAL(18, 8),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_btc_metrics_user_period
    ON btc_accumulation_metrics(user_id, period, created_at DESC);

-- ============================================================
-- 10. AI PROVIDERS
-- ============================================================
CREATE TABLE IF NOT EXISTS ai_providers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    model VARCHAR(100) NOT NULL,
    encrypted_api_key TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_providers_active ON ai_providers(is_active);

-- ============================================================
-- 11. AI PROMPTS
-- ============================================================
CREATE TABLE IF NOT EXISTS ai_prompt_versions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    version VARCHAR(50) NOT NULL,
    system_prompt TEXT NOT NULL,
    is_active BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, version)
);

-- ============================================================
-- 12. AI DECISIONS
-- ============================================================
CREATE TABLE IF NOT EXISTS ai_decisions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    strategy_run_id UUID REFERENCES strategy_runs(id),
    provider_id UUID REFERENCES ai_providers(id),
    symbol VARCHAR(20) NOT NULL,
    decision VARCHAR(20) NOT NULL,
    confidence DECIMAL(5, 2),
    reasoning TEXT,
    prompt_version VARCHAR(50),
    input_context JSONB,
    output_context JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_decisions_symbol ON ai_decisions(symbol);
CREATE INDEX IF NOT EXISTS idx_ai_decisions_created ON ai_decisions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_decisions_strategy_run ON ai_decisions(strategy_run_id);

-- ============================================================
-- 13. AI FEEDBACK
-- ============================================================
CREATE TABLE IF NOT EXISTS ai_feedback (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    decision_id UUID NOT NULL REFERENCES ai_decisions(id) ON DELETE CASCADE,
    btc_before DECIMAL(36, 18) NOT NULL,
    btc_after DECIMAL(36, 18) NOT NULL,
    btc_delta DECIMAL(36, 18) NOT NULL,
    success BOOLEAN NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 14. AI MEMORY
-- ============================================================
CREATE TABLE IF NOT EXISTS ai_memory (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    memory_type VARCHAR(50) NOT NULL,
    content TEXT NOT NULL,
    importance_score DECIMAL(10, 4) DEFAULT 1.0,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_memory_type ON ai_memory(memory_type);
CREATE INDEX IF NOT EXISTS idx_ai_memory_importance ON ai_memory(importance_score DESC);

-- ============================================================
-- 15. WCH ECOSYSTEM
-- ============================================================
CREATE TABLE IF NOT EXISTS wch_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount DECIMAL(36, 18) NOT NULL,
    transaction_type VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    tx_hash VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_wch_transactions_user_id ON wch_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_wch_transactions_status ON wch_transactions(status);

-- ============================================================
-- 16. LOGGING
-- ============================================================
CREATE TABLE IF NOT EXISTS system_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    level VARCHAR(20) NOT NULL,
    module VARCHAR(50) NOT NULL,
    message TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs(level);
CREATE INDEX IF NOT EXISTS idx_system_logs_module ON system_logs(module);
CREATE INDEX IF NOT EXISTS idx_system_logs_created ON system_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_system_logs_user ON system_logs(user_id);

-- ============================================================
-- 17. REPORTING
-- ============================================================
CREATE TABLE IF NOT EXISTS daily_reports (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    btc_start DECIMAL(36, 18) NOT NULL,
    btc_end DECIMAL(36, 18) NOT NULL,
    btc_growth DECIMAL(36, 18) NOT NULL,
    trade_count INTEGER DEFAULT 0,
    win_rate DECIMAL(10, 4),
    report_date DATE UNIQUE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_daily_reports_user_date
    ON daily_reports(user_id, report_date DESC);

-- ============================================================
-- TRIGGERS: Auto-update updated_at
-- ============================================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Tables with auto-update triggers
CREATE TRIGGER update_users_modtime
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_api_keys_modtime
    BEFORE UPDATE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_exchange_accounts_modtime
    BEFORE UPDATE ON exchange_accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_system_settings_modtime
    BEFORE UPDATE ON system_settings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_configs_modtime
    BEFORE UPDATE ON user_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_trading_pairs_modtime
    BEFORE UPDATE ON trading_pairs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_asset_targets_modtime
    BEFORE UPDATE ON asset_targets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_asset_inventory_modtime
    BEFORE UPDATE ON asset_inventory
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_orders_modtime
    BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_strategies_modtime
    BEFORE UPDATE ON strategies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_ai_providers_modtime
    BEFORE UPDATE ON ai_providers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_wch_transactions_modtime
    BEFORE UPDATE ON wch_transactions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- SEED DATA: Initial system settings
-- ============================================================
INSERT INTO system_settings (key, value) VALUES
    ('risk_max_position_size', '{"btc": 0.01, "eth": 0.1, "sol": 1.0}'::jsonb),
    ('trading_enabled', '{"global": true, "reason": "initial_setup"}'::jsonb),
    ('ai_model_config', '{"provider": "openai", "model": "gpt-4o", "temperature": 0.3}'::jsonb),
    ('signal_thresholds', '{"rsi_oversold": 30, "rsi_overbought": 70, "min_signal_strength": 60}'::jsonb)
ON CONFLICT (key) DO NOTHING;

-- ============================================================
-- SEED DATA: Default strategies
-- ============================================================
INSERT INTO strategies (name, description, parameters, enabled) VALUES
    ('btc_accumulation', 'Default BTC accumulation strategy', '{
        "min_signal_strength": 65,
        "max_position_size_btc": 0.01,
        "stop_loss_percent": 2.0,
        "take_profit_targets": [3, 5, 8],
        "timeframes": ["4h", "1d"],
        "priority_assets": ["BTC", "ETH", "SOL"]
    }'::jsonb, true),
    ('conservative_buy', 'Conservative buying on strong signals', '{
        "min_signal_strength": 75,
        "max_position_size_btc": 0.005,
        "stop_loss_percent": 1.5,
        "timeframes": ["1d"]
    }'::jsonb, true)
ON CONFLICT DO NOTHING;
