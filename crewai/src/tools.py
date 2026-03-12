"""
CrewAI Araçları (Tools).
Ajanların Supabase veritabanına erişmesini sağlayan özel araçlar.
"""

from typing import Any
from crewai.tools import BaseTool
from pydantic import Field
from supabase import create_client, Client as SupabaseClient

from .config import SUPABASE_URL, SUPABASE_SERVICE_KEY


def _get_supabase() -> SupabaseClient:
    """Supabase istemcisi oluşturur (singleton değil — her çağrıda yeni)."""
    return create_client(SUPABASE_URL, SUPABASE_SERVICE_KEY)


# ─────────────────────────────────────────────────────────────────
# 1. Analyst Agent Aracı — En İyi Performans Gösteren Görseller
# ─────────────────────────────────────────────────────────────────
class FetchTopAssetsInput:
    """FetchTopAssetsTool girdi şeması (dokümantasyon amaçlı)."""
    user_id: str
    limit: int = 3


class FetchTopAssetsTool(BaseTool):
    """
    media_assets ve interaction_analytics tablolarından en iyi performans
    gösteren görselleri getirir. engagement_rate sıralaması kullanılır.
    """

    name: str = "fetch_top_assets"
    description: str = (
        "Belirtilen kullanıcının en iyi performans gösteren görsellerini "
        "media_assets ve interaction_analytics tablolarından sorgular. "
        "Girdi olarak JSON formatında user_id ve opsiyonel limit (varsayılan 3) alır. "
        'Örnek girdi: {"user_id": "abc-123", "limit": 3}'
    )

    def _run(self, user_id: str, limit: int = 3) -> str:
        """En iyi performanslı görselleri Supabase'den çeker."""
        import json

        sb = _get_supabase()

        # 1. Kullanıcının medya varlıklarını getir
        assets_resp = (
            sb.table("media_assets")
            .select("*")
            .eq("user_id", user_id)
            .order("created_at", desc=True)
            .execute()
        )
        assets = assets_resp.data or []

        if not assets:
            return json.dumps({"error": "Kullanıcının medya varlığı bulunamadı", "assets": []})

        # 2. Her varlığın en güncel analytik verisini getir
        enriched = []
        for asset in assets:
            analytics_resp = (
                sb.table("interaction_analytics")
                .select("*")
                .eq("asset_id", asset["id"])
                .order("fetched_at", desc=True)
                .limit(1)
                .execute()
            )
            analytics = analytics_resp.data
            if analytics:
                enriched.append({
                    "asset_id": asset["id"],
                    "storage_url": asset["storage_url"],
                    "media_type": asset["media_type"],
                    "vision_analysis": asset.get("vision_analysis", {}),
                    "engagement_rate": float(analytics[0].get("engagement_rate", 0)),
                    "likes": analytics[0].get("likes", 0),
                    "comments": analytics[0].get("comments", 0),
                    "shares": analytics[0].get("shares", 0),
                    "saves": analytics[0].get("saves", 0),
                    "impressions": analytics[0].get("impressions", 0),
                })

        # 3. engagement_rate sıralaması
        enriched.sort(key=lambda x: x["engagement_rate"], reverse=True)

        # 4. Limit uygula
        top_assets = enriched[:limit]

        return json.dumps({
            "user_id": user_id,
            "total_assets": len(assets),
            "top_assets": top_assets,
        }, ensure_ascii=False)


# ─────────────────────────────────────────────────────────────────
# 2. Strateji ve Critic Ajanları için Araç — Ajan Bağlamı
# ─────────────────────────────────────────────────────────────────
class FetchAgentContextTool(BaseTool):
    """
    agent_context tablosundan kullanıcının marka bağlamını getirir.
    Marka sesi, negatif promptlar, içerik sütunları ve görsel tercihlerini içerir.
    """

    name: str = "fetch_agent_context"
    description: str = (
        "Kullanıcının marka bağlamını (brand_voice, negative_prompts, "
        "content_pillars, visual_style_preferences) getirir. "
        'Girdi olarak JSON formatında user_id alır. Örnek: {"user_id": "abc-123"}'
    )

    def _run(self, user_id: str) -> str:
        """Kullanıcının ajan bağlamını Supabase'den çeker."""
        import json

        sb = _get_supabase()

        resp = (
            sb.table("agent_context")
            .select("*")
            .eq("user_id", user_id)
            .limit(1)
            .execute()
        )

        contexts = resp.data or []
        if not contexts:
            return json.dumps({
                "error": "Kullanıcının ajan bağlamı bulunamadı",
                "context": None,
            })

        ctx = contexts[0]
        return json.dumps({
            "user_id": user_id,
            "context": {
                "brand_voice": ctx.get("brand_voice", ""),
                "target_audience": ctx.get("target_audience", ""),
                "negative_prompts": ctx.get("negative_prompts", []),
                "content_pillars": ctx.get("content_pillars", []),
                "visual_style_preferences": ctx.get("visual_style_preferences", {}),
                "posting_frequency_preference": ctx.get("posting_frequency_preference", "daily"),
            },
        }, ensure_ascii=False)
