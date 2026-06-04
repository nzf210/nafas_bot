CREATE TABLE ai_strategies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    parameters JSONB NOT NULL, -- To store strategies.json data
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ai_signal_weights (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    signal_name VARCHAR(255) NOT NULL,
    weight DECIMAL(5, 4) NOT NULL, -- To store signal-weights.json data
    context JSONB,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ai_decision_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    trade_pair VARCHAR(50),
    decision VARCHAR(50) NOT NULL,
    confidence_score DECIMAL(5, 4),
    reasoning TEXT,
    context_data JSONB, -- To store decision-log.json data
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ai_lessons (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    topic VARCHAR(255) NOT NULL,
    lesson_learned TEXT NOT NULL,
    metrics JSONB, -- To store lessons.json data
    applied_to_strategy UUID REFERENCES ai_strategies(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    config_data JSONB NOT NULL, -- To store user-config.json & config.json data
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id)
);

CREATE TABLE pool_memories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    pool_address VARCHAR(255) NOT NULL,
    chain VARCHAR(50) NOT NULL,
    memory_data JSONB NOT NULL, -- To store pool-memory.json data
    last_analyzed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(pool_address, chain)
);
