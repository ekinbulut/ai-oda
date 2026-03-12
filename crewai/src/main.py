"""
CrewAI Bridge — Ana Giriş Noktası.

Redis Pub/Sub üzerinden Go backend'den gelen strateji taleplerini dinler,
CrewAI pipeline'ını çalıştırır ve sonuçları Redis üzerinden geri gönderir.

Kullanım:
    python -m src.main
"""

import json
import logging
import signal
import sys
from datetime import datetime, timezone

import redis

from .config import (
    REDIS_HOST,
    REDIS_PORT,
    REDIS_PASSWORD,
    REDIS_DB,
    CHANNEL_STRATEGY_REQUEST,
    CHANNEL_STRATEGY_RESPONSE,
    OPENAI_API_KEY,
    SUPABASE_URL,
    SUPABASE_SERVICE_KEY,
)
from .crew import run_strategy_pipeline

# ── Logging ──────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("crewai-bridge")

# ── Global State ─────────────────────────────────────────────────
_running = True


def _signal_handler(sig, frame):
    """Graceful shutdown: SIGINT/SIGTERM yakalandığında döngüyü durdur."""
    global _running
    logger.info("🛑 Sinyal alındı (%s). Kapatılıyor...", sig)
    _running = False


def _validate_config():
    """Gerekli ortam değişkenlerinin varlığını kontrol eder."""
    errors = []
    if not OPENAI_API_KEY:
        errors.append("AI_API_KEY ortam değişkeni ayarlanmamış")
    if not SUPABASE_URL:
        errors.append("SUPABASE_URL ortam değişkeni ayarlanmamış")
    if not SUPABASE_SERVICE_KEY:
        errors.append("SUPABASE_SERVICE_KEY ortam değişkeni ayarlanmamış")
    if errors:
        for e in errors:
            logger.error("❌ %s", e)
        sys.exit(1)


def _create_redis_client() -> redis.Redis:
    """Redis bağlantısı oluşturur ve doğrular."""
    client = redis.Redis(
        host=REDIS_HOST,
        port=REDIS_PORT,
        password=REDIS_PASSWORD or None,
        db=REDIS_DB,
        decode_responses=True,
    )
    # Bağlantıyı doğrula
    client.ping()
    logger.info("✅ Redis bağlantısı başarılı: %s:%d", REDIS_HOST, REDIS_PORT)
    return client


def _handle_request(redis_client: redis.Redis, message: dict):
    """
    Gelen strateji talebini işler:
    1. Mesajı parse et
    2. CrewAI pipeline'ını çalıştır
    3. Sonucu Redis'e yayınla
    """
    try:
        data = json.loads(message["data"])
        request_id = data.get("request_id", "unknown")
        user_id = data.get("user_id", "")
        task_id = data.get("task_id", "")

        logger.info(
            "📥 Strateji talebi alındı — request_id=%s, user_id=%s, task_id=%s",
            request_id, user_id, task_id,
        )

        if not user_id:
            _publish_error(redis_client, request_id, "user_id boş olamaz")
            return

        # CrewAI pipeline'ını çalıştır
        result = run_strategy_pipeline(user_id)

        # Sonucu Redis'e yayınla
        response = {
            "request_id": request_id,
            "status": result.get("status", "success"),
            "top_assets": _format_top_assets(result.get("top_assets", [])),
            "contents": result.get("contents", []),
            "critic_review": result.get("critic_review"),
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

        redis_client.publish(
            CHANNEL_STRATEGY_RESPONSE,
            json.dumps(response, ensure_ascii=False),
        )

        logger.info(
            "📡 Strateji yanıtı gönderildi — request_id=%s, status=%s",
            request_id, response["status"],
        )

    except json.JSONDecodeError as e:
        logger.error("⚠️ Geçersiz JSON mesajı: %s", e)
    except Exception as e:
        logger.error("❌ Strateji talebi işlenirken hata: %s", e, exc_info=True)
        try:
            request_id = json.loads(message["data"]).get("request_id", "unknown")
            _publish_error(redis_client, request_id, str(e))
        except Exception:
            pass


def _publish_error(redis_client: redis.Redis, request_id: str, error_message: str):
    """Hata yanıtını Redis'e yayınlar."""
    response = {
        "request_id": request_id,
        "status": "error",
        "error": error_message,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }
    redis_client.publish(
        CHANNEL_STRATEGY_RESPONSE,
        json.dumps(response, ensure_ascii=False),
    )
    logger.warning("⚠️ Hata yanıtı gönderildi — request_id=%s: %s", request_id, error_message)


def _format_top_assets(assets: list) -> list:
    """Analyst çıktısını bridge response formatına dönüştürür."""
    formatted = []
    for a in assets:
        formatted.append({
            "asset_id": a.get("asset_id", ""),
            "storage_url": a.get("storage_url", ""),
            "engagement_rate": float(a.get("engagement_rate", 0)),
            "total_likes": int(a.get("likes", 0)),
            "total_comments": int(a.get("comments", 0)),
            "description": a.get("description", ""),
            "mood": a.get("mood", ""),
        })
    return formatted


def main():
    """Ana giriş noktası — Redis'i dinle, strateji taleplerini işle."""
    logger.info("🤖 CrewAI Bridge başlatılıyor...")

    # Yapılandırmayı doğrula
    _validate_config()

    # Sinyal yakalayıcıları kur
    signal.signal(signal.SIGINT, _signal_handler)
    signal.signal(signal.SIGTERM, _signal_handler)

    # Redis bağlantısını kur
    try:
        redis_client = _create_redis_client()
    except redis.ConnectionError as e:
        logger.error("❌ Redis bağlantısı kurulamadı: %s", e)
        sys.exit(1)

    # Pub/Sub aboneliği
    pubsub = redis_client.pubsub()
    pubsub.subscribe(CHANNEL_STRATEGY_REQUEST)

    logger.info(
        "👂 Strateji talep kanalı dinleniyor: %s",
        CHANNEL_STRATEGY_REQUEST,
    )
    logger.info("✅ CrewAI Bridge hazır. Bekleniyor...")

    # Ana dinleme döngüsü
    try:
        while _running:
            message = pubsub.get_message(ignore_subscribe_messages=True, timeout=1.0)
            if message and message["type"] == "message":
                _handle_request(redis_client, message)
    except KeyboardInterrupt:
        logger.info("🛑 Klavye kesintisi. Kapatılıyor...")
    finally:
        pubsub.unsubscribe()
        pubsub.close()
        redis_client.close()
        logger.info("🔌 Redis bağlantısı kapatıldı. CrewAI Bridge durduruldu.")


if __name__ == "__main__":
    main()
