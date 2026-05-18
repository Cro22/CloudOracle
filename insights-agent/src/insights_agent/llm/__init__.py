"""LLM provider abstraction.

The `LLMProvider` ABC isolates LangGraph from any specific vendor SDK so that
swapping Gemini for Claude or OpenAI later (sub-hito 8.4+) doesn't touch the
graph code — only requires adding a new provider class + a selector in main.
"""

from insights_agent.llm.base import LLMProvider
from insights_agent.llm.gemini import GeminiProvider

__all__ = ["GeminiProvider", "LLMProvider"]
