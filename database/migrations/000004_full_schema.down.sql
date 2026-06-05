-- ============================================================
-- ROLLBACK:000004_full_schema
-- Menghapus semua tabel dari schema V1.0
-- ============================================================

-- Hapus triggers terlebih dahulu
DROP TRIGGER IF EXISTS update_wch_transactions_modtime ON wch_transactions;
DROP TRIGGER IF EXISTS update_ai_providers_modtime ON ai_providers;
DROP TRIGGER IF EXISTS update_strategies_modtime ON strategies;
DROP TRIGGER IF EXISTS update_orders_modtime ON orders;
DROP TRIGGER IF EXISTS update_asset_inventory_modtime ON asset_inventory;
DROP TRIGGER IF EXISTS update_trading_pairs_modtime ON trading_pairs;
DROP TRIGGER IF EXISTS update_user_configs_modtime ON user_configs;
DROP TRIGGER IF EXISTS update_system_settings_modtime ON system_settings;
DROP TRIGGER IF EXISTS update_exchange_accounts_modtime ON exchange_accounts;
DROP TRIGGER IF EXISTS update_api_keys_modtime ON api_keys;
DROP TRIGGER IF EXISTS update_users_modtime ON users;

-- Hapus fungsi trigger
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Hapus semua tabel (urutan reverse karena foreign key)
DROP TABLE IF EXISTS daily_reports;
DROP TABLE IF EXISTS system_logs;
DROP TABLE IF EXISTS wch_transactions;
DROP TABLE IF EXISTS ai_memory;
DROP TABLE IF EXISTS ai_feedback;
DROP TABLE IF EXISTS ai_decisions;
DROP TABLE IF EXISTS ai_prompt_versions;
DROP TABLE IF EXISTS ai_providers;
DROP TABLE IF EXISTS btc_accumulation_metrics;
DROP TABLE IF EXISTS btc_accumulation_ledger;
DROP TABLE IF EXISTS btc_treasury_snapshots;
DROP TABLE IF EXISTS asset_inventory;
DROP TABLE IF EXISTS trade_executions;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS strategy_runs;
DROP TABLE IF EXISTS strategies;
DROP TABLE IF EXISTS signal_history;
DROP TABLE IF EXISTS market_snapshots;
DROP TABLE IF EXISTS market_candles;
DROP TABLE IF EXISTS asset_targets;
DROP TABLE IF EXISTS trading_pairs;
DROP TABLE IF EXISTS user_configs;
DROP TABLE IF EXISTS system_settings;
DROP TABLE IF EXISTS exchange_accounts;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS users;

-- Hapus extension
DROP EXTENSION IF EXISTS "uuid-ossp";
