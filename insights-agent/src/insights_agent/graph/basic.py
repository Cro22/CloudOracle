"""Basic ReAct graph: question → tool call(s) → natural-language answer.

Uses `langgraph.prebuilt.create_react_agent` for the first end-to-end
round-trip. Sub-hito 8.4 will replace this with a hand-rolled supervisor
pattern; until then, `create_react_agent` gives us:

  - A tool-aware LLM call (bind_tools is invoked under the hood).
  - A loop that runs tool calls until the LLM emits a final answer or hits
    the recursion limit.
  - Built-in tool error surfacing — exceptions from the cloudoracle tools
    become tool messages the LLM can read and react to.

The system prompt is deliberately short: long instructions in this repo
have tended to drift from the actual model behavior (see the v1 LLM
narrator's slow growth) so we lean on the tool docstrings to carry the
domain-specific guidance.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langchain_core.tools import BaseTool
from langgraph.prebuilt import create_react_agent

SYSTEM_PROMPT = """You are CloudOracle's FinOps assistant. You help engineers and finance teams \
understand cloud costs.

Use the tools when the user asks for numbers — never invent or estimate \
costs yourself. If a tool returns `data_source: "snapshots_approximation"`, \
tell the user the figures are approximations from periodic snapshots, not \
billing-API truth, when accuracy matters for the answer.

Reply in the same language the user used.

If a question is outside cloud cost / FinOps scope (e.g. general coding help, \
weather, personal advice), politely decline and explain what you do cover."""


@dataclass
class AgentResult:
    """Compact, JSON-friendly view of an agent turn.

    `tool_calls` is a list of ordered {name, args} dicts pulled from every
    AIMessage in the run — useful for --verbose CLI output and tests that
    want to assert tool-selection behavior without inspecting LangChain
    message objects directly.
    """

    answer: str
    tool_calls: list[dict[str, Any]] = field(default_factory=list)
    messages: list[Any] = field(default_factory=list)


def build_graph(llm: BaseChatModel, tools: list[BaseTool]) -> Any:
    """Compile a ReAct agent bound to `tools`.

    We pass the system prompt as a `prompt` argument so it's prepended to
    every model call inside the graph (rather than baked into the input
    messages — that would make turn 2+ duplicate it)."""
    return create_react_agent(model=llm, tools=tools, prompt=SystemMessage(content=SYSTEM_PROMPT))


async def ask(graph: Any, question: str) -> AgentResult:
    """Run one user question through the graph and return a compact result."""
    state: dict[str, Any] = await graph.ainvoke({"messages": [HumanMessage(content=question)]})

    messages: list[Any] = state.get("messages", [])
    tool_calls: list[dict[str, Any]] = []
    answer = ""
    for msg in messages:
        if isinstance(msg, AIMessage):
            for call in getattr(msg, "tool_calls", []) or []:
                tool_calls.append({"name": call.get("name"), "args": call.get("args", {})})
            content = _stringify_content(msg.content)
            if content:
                answer = content  # The last AI content wins — that's the final answer.

    return AgentResult(answer=answer, tool_calls=tool_calls, messages=messages)


def _stringify_content(content: Any) -> str:
    """AIMessage.content can be str or a list of content blocks (Gemini multimodal).

    Flatten the list-of-blocks form to plain text so callers don't have to
    care about the variant.
    """
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for block in content:
            if isinstance(block, str):
                parts.append(block)
            elif isinstance(block, dict) and block.get("type") == "text":
                text = block.get("text")
                if isinstance(text, str):
                    parts.append(text)
        return "".join(parts)
    return ""
