# NAFAS TRADING SYSTEM

## SYSTEM OVERVIEW
NAFAS is an AI-assisted crypto asset accumulation system.

It is NOT:
- Signal group
- Copy trading platform
- High-frequency trading bot
- Gambling system

It IS:
- Long-term asset accumulation engine
- AI-orchestrated trading system
- Risk-first decision architecture
- Telegram-first product

Target assets:
- BTC (primary)
- ETH (secondary)
- SOL (tertiary)

---

## CORE PRINCIPLE

1. Risk > Opportunity
2. Consistency > Profit spikes
3. Accumulation > Speculation
4. Data > Emotion
5. System > Manual decisions

---

## ARCHITECTURE SUMMARY

Backend is Modular Monolith:

internal/
- auth
- user
- bot
- exchange
- scanner
- strategy
- risk
- execution
- ai
- learning
- telegram

DO NOT start with microservices.

---

## AI SYSTEM DESIGN

NAFAS uses multi-agent AI architecture.

Core component:
- AI Coordinator (master orchestrator)

Agents:
- Market Analyst → market regime detection
- Pair Analyst → ranking trading pairs
- Risk Guardian → block unsafe trades
- Trade Reviewer → validate execution logic
- Execution Advisor → define execution plan
- Learning Engine → improve strategies over time

IMPORTANT:
AI never trades directly.
AI only produces structured JSON outputs.

---

## TRADING LOGIC

Pipeline:

Market Data → Scanner → Pair Ranking → AI Review → Risk Engine → Execution → Learning

Rules:
- No emotional trading
- No prediction certainty
- Every trade must pass Risk Guardian
- BTC accumulation is default priority

---

## RISK MANAGEMENT RULES

- Always limit exposure in volatile markets
- Reject trades if data is incomplete
- Reduce position size in uncertainty
- No overtrading allowed
- Every trade must have stop loss

Risk Guardian is FINAL AUTHORITY.

---

## TELEGRAM AS PRIMARY UI

All user interaction happens in Telegram:

/dashboard
/portfolio
/positions
/report
/settings

Telegram is NOT optional — it is core UI layer.

---

## DATABASE PRINCIPLES

Use PostgreSQL as core database.

AI memory layer uses:
- trade_memories
- market_memories
- strategy_memories
- pgvector (future)

All trades and decisions must be logged.

---

## SECURITY RULES

- Exchange API keys must be encrypted (AES-256)
- No plaintext secrets allowed
- All actions must be audited
- All executions must be traceable

---

## DEPLOYMENT PRINCIPLE

Start simple:

- Single VPS
- Docker Compose
- Postgres + Redis + NATS + App + AI (Ollama optional)

Do NOT over-engineer infra early.

---

## DEVELOPMENT RULE

If unsure:
→ choose simplicity
→ choose modular monolith
→ choose observable system

---

## FUTURE VISION

System will evolve into:

- Cross-chain trading
- DEX integration
- AI portfolio rotation engine
- Agent marketplace
- Fully autonomous accumulation system

---

## NON-NEGOTIABLE

- No gambling behavior
- No reckless leverage
- No emotional AI decisions
- No undocumented trading logic

System must remain predictable, auditable, and deterministic in behavior.