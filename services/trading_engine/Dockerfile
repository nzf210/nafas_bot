FROM python:3.11-slim

# Set working directory
WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

# Upgrade pip tooling (tanpa ribet versi berlebihan)
RUN pip install --no-cache-dir --upgrade pip setuptools wheel

# Clone TradingAgents source langsung dari GitHub
RUN git clone https://github.com/tauricresearch/tradingagents.git /opt/tradingagents

# Copy requirements
COPY requirements.txt .

# Install dependencies aplikasi utama
RUN pip install --no-cache-dir -r requirements.txt

# Install TradingAgents dari source (editable mode lebih fleksibel)
RUN pip install -e /opt/tradingagents

# Copy source code aplikasi
COPY . .

# Expose port API (ubah jika perlu)
EXPOSE 8000

# Jalankan API
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]