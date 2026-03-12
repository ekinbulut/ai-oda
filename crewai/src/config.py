"""
Yapılandırma modülü.
Tüm ortam değişkenleri ve sabitler burada tanımlanır.
"""

import os
from dotenv import load_dotenv

load_dotenv()

# ── Redis ────────────────────────────────────────────────────────
REDIS_HOST = os.getenv("REDIS_HOST", "localhost")
REDIS_PORT = int(os.getenv("REDIS_PORT", "6379"))
REDIS_PASSWORD = os.getenv("REDIS_PASSWORD", "")
REDIS_DB = int(os.getenv("REDIS_DB", "0"))

# Redis Pub/Sub kanal adları (Go tarafıyla eşlenmeli)
CHANNEL_STRATEGY_REQUEST = "crewai:strategy:request"
CHANNEL_STRATEGY_RESPONSE = "crewai:strategy:response"

# ── Supabase ─────────────────────────────────────────────────────
SUPABASE_URL = os.getenv("SUPABASE_URL", "")
SUPABASE_SERVICE_KEY = os.getenv("SUPABASE_SERVICE_KEY", "")

# ── OpenAI ───────────────────────────────────────────────────────
OPENAI_API_KEY = os.getenv("AI_API_KEY", "")
AI_MODEL = os.getenv("AI_MODEL", "gpt-4o")
