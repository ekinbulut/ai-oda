"""
CrewAI Crew & Task Tanımları.

Strateji pipeline'ı şu sırayla çalışır:
  1. Analyst  → En iyi 3 görseli seç
  2. Strategy → Seçilen görseller için metin üret
  3. Critic   → Çıktıları denetle ve onayla/reddet
"""

import json
import logging
from typing import Any

from crewai import Crew, Task, Process

from .agents import (
    create_analyst_agent,
    create_strategy_agent,
    create_critic_agent,
)

logger = logging.getLogger(__name__)


def build_strategy_crew(user_id: str) -> Crew:
    """
    Verilen user_id için 3 ajanlı strateji crew'u oluşturur.
    Sequential process kullanılır — her görev bir öncekinin çıktısını kullanır.
    """

    analyst = create_analyst_agent()
    strategist = create_strategy_agent()
    critic = create_critic_agent()

    # ── Görev 1: Analiz ─────────────────────────────────────────
    task_analyze = Task(
        description=(
            f"Kullanıcı ID: {user_id}\n\n"
            "1. fetch_top_assets aracını kullanarak bu kullanıcının en iyi "
            "performans gösteren 3 görselini bul.\n"
            "2. Her görsel için şunları raporla:\n"
            "   - asset_id ve storage_url\n"
            "   - engagement_rate, likes, comments, shares\n"
            "   - vision_analysis'ten gelen açıklama (varsa)\n"
            "3. Neden bu görsellerin başarılı olduğunu kısa bir analizle açıkla."
        ),
        expected_output=(
            "JSON formatında bir rapor:\n"
            "{\n"
            '  "top_assets": [\n'
            "    {\n"
            '      "asset_id": "...",\n'
            '      "storage_url": "...",\n'
            '      "engagement_rate": 0.0,\n'
            '      "likes": 0,\n'
            '      "comments": 0,\n'
            '      "description": "...",\n'
            '      "mood": "...",\n'
            '      "success_reason": "Bu görsel neden başarılı?"\n'
            "    }\n"
            "  ]\n"
            "}"
        ),
        agent=analyst,
    )

    # ── Görev 2: Strateji ───────────────────────────────────────
    task_strategize = Task(
        description=(
            f"Kullanıcı ID: {user_id}\n\n"
            "1. fetch_agent_context aracını kullanarak kullanıcının marka bağlamını al "
            "(brand_voice, target_audience, content_pillars).\n"
            "2. Analyst Agent'ın seçtiği 3 görselin her biri için:\n"
            "   - Görselin betimlemesini (description) ve ruh halini (mood) dikkate al\n"
            "   - Marka sesine uygun bir Instagram caption yaz\n"
            "   - Hedef kitleyle rezonans kuracak 5-10 hashtag öner\n"
            "3. Caption'lar etkileşimi teşvik edecek call-to-action içermeli."
        ),
        expected_output=(
            "JSON formatında üretilen içerikler:\n"
            "{\n"
            '  "contents": [\n'
            "    {\n"
            '      "asset_id": "...",\n'
            '      "caption": "Üretilen Instagram caption metni...",\n'
            '      "hashtags": "#hashtag1 #hashtag2 ..."\n'
            "    }\n"
            "  ]\n"
            "}"
        ),
        agent=strategist,
        context=[task_analyze],  # Analyst çıktısını bağlam olarak al
    )

    # ── Görev 3: Denetim ────────────────────────────────────────
    task_critique = Task(
        description=(
            f"Kullanıcı ID: {user_id}\n\n"
            "1. fetch_agent_context aracını kullanarak kullanıcının negatif prompt'larını al.\n"
            "2. Strategy Agent'ın ürettiği her caption ve hashtag setini denetle:\n"
            "   - Negatif prompt'lardaki yasaklı konu/ifadeler var mı?\n"
            "   - Marka kimliğiyle çelişen unsurlar var mı?\n"
            "   - Uygunsuz, hassas veya riskli içerik var mı?\n"
            "3. Her içerik için onay/red kararı ver.\n"
            "4. Her içerik için 0-1 arası kalite skoru ver.\n"
            "5. Sorunları spesifik olarak listele."
        ),
        expected_output=(
            "JSON formatında denetim raporu:\n"
            "{\n"
            '  "approved": true/false,\n'
            '  "overall_score": 0.0,\n'
            '  "reviews": [\n'
            "    {\n"
            '      "asset_id": "...",\n'
            '      "approved": true/false,\n'
            '      "score": 0.0,\n'
            '      "issues": ["sorun1", "sorun2"]\n'
            "    }\n"
            "  ]\n"
            "}"
        ),
        agent=critic,
        context=[task_strategize],  # Strategy çıktısını bağlam olarak al
    )

    # ── Crew ────────────────────────────────────────────────────
    crew = Crew(
        agents=[analyst, strategist, critic],
        tasks=[task_analyze, task_strategize, task_critique],
        process=Process.sequential,
        verbose=True,
    )

    return crew


def run_strategy_pipeline(user_id: str) -> dict[str, Any]:
    """
    Tam strateji pipeline'ını çalıştırır ve sonuçları yapılandırılmış
    bir dict olarak döner.
    """
    logger.info("🚀 Strateji pipeline başlatılıyor — user_id=%s", user_id)

    crew = build_strategy_crew(user_id)
    result = crew.kickoff()

    logger.info("✅ Strateji pipeline tamamlandı — user_id=%s", user_id)

    # CrewAI çıktısını parse et
    try:
        # result.raw ham metin çıktısıdır; task çıktılarını ayrıca alalım
        tasks_output = result.tasks_output if hasattr(result, "tasks_output") else []

        analyst_output = _safe_parse_json(tasks_output[0].raw if len(tasks_output) > 0 else "")
        strategy_output = _safe_parse_json(tasks_output[1].raw if len(tasks_output) > 1 else "")
        critic_output = _safe_parse_json(tasks_output[2].raw if len(tasks_output) > 2 else "")

        return {
            "status": "success",
            "top_assets": analyst_output.get("top_assets", []),
            "contents": strategy_output.get("contents", []),
            "critic_review": {
                "approved": critic_output.get("approved", False),
                "score": critic_output.get("overall_score", 0.0),
                "issues": _collect_issues(critic_output),
            },
        }

    except Exception as e:
        logger.error("❌ Pipeline çıktısı parse edilemedi: %s", e)
        return {
            "status": "success",
            "raw_output": str(result.raw) if hasattr(result, "raw") else str(result),
            "top_assets": [],
            "contents": [],
            "critic_review": {"approved": False, "score": 0.0, "issues": [str(e)]},
        }


def _safe_parse_json(text: str) -> dict:
    """JSON parse etmeye çalışır, başarısızsa boş dict döner."""
    if not text:
        return {}
    # Bazen LLM çıktısı ```json ... ``` ile sarılı gelir
    cleaned = text.strip()
    if cleaned.startswith("```"):
        lines = cleaned.split("\n")
        # İlk ve son ``` satırlarını kaldır
        lines = [l for l in lines if not l.strip().startswith("```")]
        cleaned = "\n".join(lines)
    try:
        return json.loads(cleaned)
    except json.JSONDecodeError:
        return {"raw": text}


def _collect_issues(critic_output: dict) -> list[str]:
    """Denetim çıktısından tüm sorunları toplar."""
    issues = []
    for review in critic_output.get("reviews", []):
        issues.extend(review.get("issues", []))
    return issues
