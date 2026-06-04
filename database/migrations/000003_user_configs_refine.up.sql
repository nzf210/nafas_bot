-- Drop the generic user_configs table from previous migration to refine it
DROP TABLE IF EXISTS user_configs;

CREATE TABLE user_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE UNIQUE NOT NULL,
    
    -- Risk settings
    max_risk_per_trade DECIMAL(5, 2) DEFAULT 1.00, -- e.g. 1% of portfolio
    daily_loss_limit DECIMAL(5, 2) DEFAULT 5.00,
    max_open_positions INTEGER DEFAULT 3,
    
    -- Notification settings
    notify_on_trade BOOLEAN DEFAULT true,
    notify_on_error BOOLEAN DEFAULT true,
    daily_report_time TIME DEFAULT '00:00:00',
    
    -- Trading preferences
    preferred_pairs TEXT[] DEFAULT ARRAY['BTC/USDT', 'ETH/USDT']::TEXT[],
    auto_trade_enabled BOOLEAN DEFAULT false,
    
    -- Flexible storage for future AI-specific user configs
    metadata JSONB DEFAULT '{}'::JSONB,
    
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_user_configs_modtime
    BEFORE UPDATE ON user_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
