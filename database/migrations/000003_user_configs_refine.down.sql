DROP TRIGGER IF EXISTS update_user_configs_modtime ON user_configs;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS user_configs;

-- Revert to generic if needed
CREATE TABLE user_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    config_data JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id)
);
