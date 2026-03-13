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
    üzerinden en az 3 ay geçmiş ve en iyi performans gösteren 3 görseli seçer.
    """
    return Agent(
        role="İçerik Geri Dönüşüm Uzmanı",
        goal=(
            "Kullanıcının media_assets ve interaction_analytics verilerini analiz ederek "
            "en yüksek etkileşimli ama üzerinden en az 3 ay geçmiş 3 görseli getir. "
            "Her görsel için neden başarılı olduğunu açıkla."
        ),
        backstory=(
            "Sen deneyimli bir içerik geri dönüşüm uzmanısın. Instagram içerik "
            "performansını ölçmekte ve eski içerikleri yeniden değerlendirmekte uzmanlaşmışsın. "
            "Etkileşim oranları, beğeni/yorum dengesi ve erişim metriklerini analiz ederek hangi "
            "eski görsellerin yeniden kullanıma en uygun olduğunu belirlersin. Veri odaklı "
            "kararlar alırsın ve sonuçları net bir şekilde raporlarsın."
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
    Strategy Agent çıktılarını ve ilgili görsel meta-verilerini marka sesine 
    (brand_voice) ve negatif prompt'lara göre denetler. Uygunsuz içerikleri tespit eder.
    """
    return Agent(
        role="İçerik Denetçisi",
        goal=(
            "Strategy Agent tarafından üretilen caption ve hashtag'leri denetle. "
            "Kullanıcının marka sesine (brand_voice) ve tanımladığı negatif prompt'lara "
            "(kaçınılması gereken konu/ifadeler) uygunluğunu SADECE metin için değil, "
            "ilgili görselin meta-verisi (vision_analysis vb.) için de kontrol et. "
            "Marka kimliğiyle çelişen, uygunsuz veya riskli içerikleri işaretle. "
            "Her içerik için bir onay/red kararı ve kalite skoru (0-1) ver."
        ),
        backstory=(
            "Sen titiz bir içerik denetçisisin. Markaların itibar risklerini en aza "
            "indirme konusunda uzmanlaşmışsın. Üretilen metni ve görsel meta-verilerini "
            "marka sesine, negatif prompt listesine ve marka kurallarına göre "
            "değerlendirirsin. Potansiyel sorunları erken tespit eder ve yapıcı "
            "geri bildirim sunarsın."
        ),
        tools=[FetchAgentContextTool()],
        verbose=True,
        allow_delegation=False,
        llm=f"openai/{AI_MODEL}",
    )
