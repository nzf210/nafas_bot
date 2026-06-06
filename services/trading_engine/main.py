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
            from tradingagents import TradingAgentsConfig, TradingAgentsGraph, set_config

            # Get LLM config from environment
            llm_base_url = os.getenv("LLM_BASE_URL", "https://api.openai.com/v1")
            llm_api_key = os.getenv("LLM_API_KEY", "")
            llm_model = os.getenv("LLM_MODEL", "gpt-4o")

            os.environ["OPENAI_BASE_URL"] = llm_base_url
            os.environ["OPENAI_API_BASE"] = llm_base_url
            os.environ["OPENAI_API_KEY"] = llm_api_key

            config = TradingAgentsConfig(
                llm_provider="openai",
                deep_think_llm=llm_model,
                quick_think_llm=llm_model,
                max_debate_rounds=3,
                max_risk_discuss_rounds=3,
                max_recur_limit=30,
            )
            set_config(config)

            ta_instance = TradingAgentsGraph(config=config)
            logger.info(f"TradingAgents initialized with model: {llm_model}")
        except Exception as e:
            logger.error(f"Failed to initialize TradingAgents: {e}")
            raise HTTPException(
                status_code=500,
                detail=f"TradingAgents init failed: {e}"
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

        # Apply LLM config override to environment if provided
        if request.llm_config:
            if request.llm_config.base_url:
                os.environ["OPENAI_BASE_URL"] = request.llm_config.base_url
                os.environ["OPENAI_API_BASE"] = request.llm_config.base_url
            if request.llm_config.api_key:
                os.environ["OPENAI_API_KEY"] = request.llm_config.api_key

        # Run TradingAgents analysis in a thread pool to avoid blocking the event loop
        import asyncio
        state, decision = await asyncio.to_thread(ta.propagate, request.ticker, request.date)

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
