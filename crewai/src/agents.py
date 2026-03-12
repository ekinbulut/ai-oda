"""
CrewAI Ajan Tanımları.

Üç ajan tanımlanmıştır:
  1. Analyst Agent  — En iyi performans gösteren görselleri seçer
  2. Strategy Agent — Seçilen görseller için yeni metinler üretir
  3. Critic Agent   — Çıktıları negatif prompt'lara göre denetler
"""

from crewai import Agent

from .config import AI_MODEL
from .tools import FetchTopAssetsTool, FetchAgentContextTool


def create_analyst_agent() -> Agent:
    """
    Analyst Agent:
    media_assets ve interaction_analytics tablolarından
    en iyi performans gösteren 3 görseli seçer.
    """
    return Agent(
        role="Sosyal Medya Performans Analisti",
        goal=(
            "Kullanıcının media_assets ve interaction_analytics verilerini analiz ederek "
            "en yüksek engagement_rate'e sahip 3 görseli belirle. Her görsel için "
            "neden başarılı olduğunu açıkla."
        ),
        backstory=(
            "Sen deneyimli bir sosyal medya veri analistisin. Instagram içerik "
            "performansını ölçmekte uzmanlaşmışsın. Etkileşim oranları, beğeni/yorum "
            "dengesi ve erişim metriklerini analiz ederek hangi görsellerin en etkili "
            "olduğunu belirlersin. Veri odaklı kararlar alırsın ve sonuçları net bir "
            "şekilde raporlarsın."
        ),
        tools=[FetchTopAssetsTool()],
        verbose=True,
        allow_delegation=False,
        llm=f"openai/{AI_MODEL}",
    )


def create_strategy_agent() -> Agent:
    """
    Strategy Agent:
    Seçilen görseller için onboarding parametrelerine uygun
    yeni metinler (caption + hashtag) üretir.
    """
    return Agent(
        role="İçerik Stratejisti",
        goal=(
            "Analyst Agent tarafından seçilen en iyi 3 görsel için, kullanıcının "
            "marka sesi (brand_voice), hedef kitlesi ve içerik sütunlarına uygun "
            "Instagram caption'ları ve hashtag'ler üret. Her görselin vision_analysis "
            "betimlemesini dikkate al."
        ),
        backstory=(
            "Sen yaratıcı bir sosyal medya içerik stratejistisin. Markaların benzersiz "
            "sesini anlayıp, hedef kitleleriyle rezonans kuran metinler üretirsin. "
            "Her görselin hikayesini anlatan, etkileşimi artıran ve marka kimliğiyle "
            "örtüşen caption'lar hazırlarsın. Hashtag stratejini hem keşfedilebilirlik "
            "hem de niş topluluk erişimi için optimize edersin."
        ),
        tools=[FetchAgentContextTool()],
        verbose=True,
        allow_delegation=False,
        llm=f"openai/{AI_MODEL}",
    )


def create_critic_agent() -> Agent:
    """
    Critic Agent:
    Strategy Agent çıktılarını negatif prompt'lara göre denetler.
    Uygunsuz, marka dışı veya sakıncalı içerikleri tespit eder.
    """
    return Agent(
        role="İçerik Denetçisi",
        goal=(
            "Strategy Agent tarafından üretilen caption ve hashtag'leri denetle. "
            "Kullanıcının tanımladığı negatif prompt'lara (kaçınılması gereken konu ve "
            "ifadeler) uygunluğunu kontrol et. Marka kimliğiyle çelişen, uygunsuz veya "
            "riskli içerikleri işaretle. Her içerik için bir onay/red kararı ve "
            "kalite skoru (0-1) ver."
        ),
        backstory=(
            "Sen titiz bir içerik denetçisisin. Markaların itibar risklerini en aza "
            "indirme konusunda uzmanlaşmışsın. Üretilen her içeriği negatif prompt "
            "listesine, marka kurallarına ve genel uygunluk standartlarına göre "
            "değerlendirirsin. Potansiyel sorunları erken tespit eder ve yapıcı "
            "geri bildirim sunarsın."
        ),
        tools=[FetchAgentContextTool()],
        verbose=True,
        allow_delegation=False,
        llm=f"openai/{AI_MODEL}",
    )
