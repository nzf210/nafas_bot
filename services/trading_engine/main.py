"""
NAFAS TradingAgents Service
FastAPI wrapper untuk TauricResearch TradingAgents
"""

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from typing import Optional, List
import os
import logging

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI(
    title="NAFAS TradingAgents",
    description="Multi-agent trading analysis service",
    version="1.0.0"
)

# CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Pydantic models
class LLMConfig(BaseModel):
    base_url: Optional[str] = None
    api_key: Optional[str] = None
    model: Optional[str] = None

class AnalyzeRequest(BaseModel):
    ticker: str
    date: str
    llm_config: Optional[LLMConfig] = None

class AnalyzeResponse(BaseModel):
    action: str  # BUY/SELL/HOLD
    confidence: float
    reasoning: str
    allocation: float
    stop_loss: float
    take_profit_targets: List[float]

# Global TradingAgents instance
ta_instance = None

def get_ta_instance():
    """Get or create TradingAgents instance"""
    global ta_instance

    if ta_instance is None:
        try:
            from tradingagents.graph.trading_graph import TradingAgentsGraph
            from tradingagents.default_config import DEFAULT_CONFIG

            # Get LLM config from environment
            llm_base_url = os.getenv("LLM_BASE_URL", "https://api.openai.com/v1")
            llm_api_key = os.getenv("LLM_API_KEY", "")
            llm_model = os.getenv("LLM_MODEL", "gpt-4o")

            # Override DEFAULT_CONFIG with environment variables
            config = DEFAULT_CONFIG.copy()
            config["llm_config"] = {
                "provider": "openai",
                "base_url": llm_base_url,
                "api_key": llm_api_key,
                "model": llm_model,
            }

            ta_instance = TradingAgentsGraph(config=config)
            logger.info(f"TradingAgents initialized with model: {llm_model}")
        except ImportError as e:
            logger.error(f"Failed to import TradingAgents: {e}")
            raise HTTPException(
                status_code=500,
                detail="TradingAgents not installed. Run: pip install tradingagents"
            )

    return ta_instance

@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {"status": "ok", "service": "tradingagents"}

@app.post("/analyze", response_model=AnalyzeResponse)
async def analyze_ticker(request: AnalyzeRequest):
    """
    Analyze a ticker using TradingAgents multi-agent system

    Args:
        request: AnalyzeRequest with ticker, date, and optional LLM config

    Returns:
        AnalyzeResponse with trading decision
    """
    logger.info(f"Analyzing ticker: {request.ticker} for date: {request.date}")

    try:
        ta = get_ta_instance()

        # Build LLM config override if provided
        llm_config = None
        if request.llm_config:
            llm_config = {
                "provider": "openai",
                "base_url": request.llm_config.base_url or os.getenv("LLM_BASE_URL"),
                "api_key": request.llm_config.api_key or os.getenv("LLM_API_KEY"),
                "model": request.llm_config.model or os.getenv("LLM_MODEL"),
            }

        # Run TradingAgents analysis
        state, decision = ta.propagate(request.ticker, request.date, llm_config=llm_config)

        # Extract response
        response = AnalyzeResponse(
            action=decision.get("action", "HOLD"),
            confidence=decision.get("confidence", 0.0),
            reasoning=decision.get("reasoning", "No reasoning provided"),
            allocation=decision.get("allocation", 0.0),
            stop_loss=decision.get("stop_loss", 0.0),
            take_profit_targets=decision.get("take_profit_targets", []),
        )

        logger.info(f"Analysis complete: {response.action} (confidence: {response.confidence}%)")
        return response

    except Exception as e:
        logger.error(f"Analysis failed: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/")
async def root():
    """Root endpoint"""
    return {
        "service": "NAFAS TradingAgents",
        "version": "1.0.0",
        "docs": "/docs"
    }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
