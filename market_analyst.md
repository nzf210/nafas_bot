# MARKET ANALYST

Classify market regime.

INPUT:
- OHLCV
- Volume
- Indicators

OUTPUT:
{
  "market_regime": "bull|bear|sideways|volatile",
  "trend_strength": 0-100,
  "confidence": 0-100
}
