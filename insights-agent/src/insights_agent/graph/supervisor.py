"""Hand-rolled supervisor multi-agent graph (milestone 8.4).

Replaces `create_react_agent` (graph/basic.py) with an explicit `StateGraph`:

    START → supervisor → {worker} → supervisor → … → synthesize → END

- **supervisor** routes by *tool call*: it is bound with one routing tool per
  specialist plus `finish`, and the tool it calls names the next hop. Routing
  via tool calls (rather than `with_structured_output`) keeps the node driveable
  by the same scripted fake model the rest of the suite uses, and mirrors how a
  real LLM hands off control.
- **workers** are three specialists, each a *hand-rolled* ReAct loop
  (`_run_react`) over a subset of the tools — this is the actual
  create_react_agent replacement. A worker contributes a single summarizing
  `AIMessage` back to the shared transcript; its own tool churn stays local so
  the supervisor and synthesizer see a clean conversation.
- **synthesize** composes the final user-facing answer from the specialists'
  findings.

A hop cap bounds the supervisor loop so a model that never emits `finish` still
terminates.
"""

from __future__ import annotations

import json
import operator
from collections.abc import Sequence
from typing import Annotated, Any, TypedDict

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import (
    AIMessage,
    BaseMessage,
    HumanMessage,
    SystemMessage,
    ToolMessage,
)
from langchain_core.tools import BaseTool, StructuredTool
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages

from insights_agent.graph.basic import AgentResult, _stringify_content

# Worker identifiers double as graph node names and routing-tool names.
COST_ANALYST = "cost_analyst"
SAVINGS_ADVISOR = "savings_advisor"
CONCEPT_EXPERT = "concept_expert"
FINISH = "finish"

WORKER_NAMES: tuple[str, ...] = (COST_ANALYST, SAVINGS_ADVISOR, CONCEPT_EXPERT)

# Which tools (by name) each specialist may use. Names that aren't present in
# the supplied tool list are simply skipped — e.g. finops_knowledge_search is
# absent when RAG is disabled, leaving concept_expert to answer from the model.
WORKER_TOOLS: dict[str, frozenset[str]] = {
    COST_ANALYST: frozenset(
        {
            "cloudoracle_cost_summary",
            "cloudoracle_cost_by_service",
            "cloudoracle_cost_trends",
            "cloudoracle_inventory",
        }
    ),
    SAVINGS_ADVISOR: frozenset(
        {"cloudoracle_recommendations", "finops_knowledge_search"}
    ),
    CONCEPT_EXPERT: frozenset({"finops_knowledge_search"}),
}

# Upper bound on supervisor decisions, so a model that never says `finish`
# still terminates. Three workers + a finish is the expected worst case; the
# cap sits above that as a safety net, not a normal path.
MAX_HOPS = 6

# Per-worker ReAct iterations (model call → tool calls → model call …).
MAX_WORKER_ITERS = 6


class SupervisorState(TypedDict):
    messages: Annotated[list[BaseMessage], add_messages]
    tool_calls: Annotated[list[dict[str, Any]], operator.add]
    route: str
    hops: int


def build_supervisor_graph(llm: BaseChatModel, tools: Sequence[BaseTool]) -> Any:
    """Compile the supervisor graph over `tools` (the same flat tool list)."""
    tool_list = list(tools)
    routing_tools = _build_routing_tools()

    async def supervisor(state: SupervisorState) -> dict[str, Any]:
        router = llm.bind_tools(routing_tools)
        resp = await router.ainvoke([SystemMessage(_SUPERVISOR_PROMPT), *state["messages"]])
        calls = getattr(resp, "tool_calls", None) or []
        route = calls[0]["name"] if calls else FINISH
        return {"route": route, "hops": state["hops"] + 1}

    def decide(state: SupervisorState) -> str:
        if state["hops"] > MAX_HOPS:
            return "synthesize"
        return state["route"] if state["route"] in WORKER_NAMES else "synthesize"

    async def synthesize(state: SupervisorState) -> dict[str, Any]:
        resp = await llm.ainvoke([SystemMessage(_SYNTHESIZE_PROMPT), *state["messages"]])
        return {"messages": [resp]}

    graph = StateGraph(SupervisorState)
    graph.add_node("supervisor", supervisor)
    graph.add_node("synthesize", synthesize)
    for name in WORKER_NAMES:
        graph.add_node(name, _make_worker_node(llm, tool_list, name))

    graph.add_edge(START, "supervisor")
    graph.add_conditional_edges(
        "supervisor",
        decide,
        {**{n: n for n in WORKER_NAMES}, "synthesize": "synthesize"},
    )
    for name in WORKER_NAMES:
        graph.add_edge(name, "supervisor")
    graph.add_edge("synthesize", END)
    return graph.compile()


async def ask_supervisor(graph: Any, question: str) -> AgentResult:
    """Run one question through the supervisor graph and return a compact result."""
    state: dict[str, Any] = await graph.ainvoke(
        {
            "messages": [HumanMessage(content=question)],
            "tool_calls": [],
            "route": "",
            "hops": 0,
        }
    )

    messages: list[Any] = state.get("messages", [])
    answer = ""
    for msg in messages:
        if isinstance(msg, AIMessage):
            content = _stringify_content(msg.content)
            if content:
                answer = content  # last non-empty AI content = synthesizer output
    return AgentResult(
        answer=answer,
        tool_calls=list(state.get("tool_calls", [])),
        messages=messages,
    )


def _make_worker_node(
    llm: BaseChatModel, tools: list[BaseTool], name: str
) -> Any:
    system = _WORKER_PROMPTS[name]
    worker_tools = [t for t in tools if t.name in WORKER_TOOLS[name]]

    async def node(state: SupervisorState) -> dict[str, Any]:
        answer, calls = await _run_react(llm, worker_tools, system, state["messages"])
        contribution = AIMessage(content=answer or "(no findings)", name=name)
        return {"messages": [contribution], "tool_calls": calls}

    return node


async def _run_react(
    llm: BaseChatModel,
    tools: list[BaseTool],
    system_prompt: str,
    conversation: Sequence[BaseMessage],
) -> tuple[str, list[dict[str, Any]]]:
    """A minimal ReAct loop: the hand-rolled replacement for create_react_agent.

    Returns the worker's final text plus the ordered {name, args} tool calls it
    made (for --verbose / assertions). Tools already convert their own errors to
    observations (handle_tool_error=True), so a failed tool feeds the model a
    message instead of aborting the loop.
    """
    model = llm.bind_tools(tools) if tools else llm
    by_name = {t.name: t for t in tools}
    messages: list[BaseMessage] = [SystemMessage(system_prompt), *conversation]
    collected: list[dict[str, Any]] = []

    for _ in range(MAX_WORKER_ITERS):
        ai = await model.ainvoke(messages)
        messages.append(ai)
        calls = getattr(ai, "tool_calls", None) or []
        if not calls:
            return _stringify_content(ai.content), collected

        for call in calls:
            collected.append({"name": call["name"], "args": call.get("args", {})})
            tool = by_name.get(call["name"])
            if tool is None:
                observation: Any = f"error: unknown tool {call['name']!r}"
            else:
                observation = await tool.ainvoke(call.get("args", {}))
            messages.append(
                ToolMessage(
                    content=_to_text(observation),
                    tool_call_id=call.get("id", call["name"]),
                    name=call["name"],
                )
            )

    # Iteration budget exhausted — return whatever the last AI message said.
    last = messages[-1]
    return (_stringify_content(last.content) if isinstance(last, AIMessage) else ""), collected


def _to_text(value: Any) -> str:
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=False)
    except (TypeError, ValueError):
        return str(value)


def _noop() -> None:  # pragma: no cover - routing tools are never executed
    """Placeholder body for routing tools; the supervisor only reads their name."""


def _build_routing_tools() -> list[StructuredTool]:
    specs = {
        COST_ANALYST: "Route to the cost & inventory analyst for spend totals, "
        "per-service breakdowns, cost trends over time, or resource inventory.",
        SAVINGS_ADVISOR: "Route to the savings advisor for optimization / "
        "rightsizing recommendations and where money can be saved.",
        CONCEPT_EXPERT: "Route to the FinOps concept expert for definitions, "
        "policy, and how-to questions answered from the knowledge base.",
        FINISH: "Call when the specialists have gathered enough to answer (or "
        "the question is out of scope) — hands off to final synthesis.",
    }
    return [
        StructuredTool.from_function(func=_noop, name=name, description=desc)
        for name, desc in specs.items()
    ]


_SUPERVISOR_PROMPT = """You are the supervisor of CloudOracle's FinOps assistant. \
You coordinate three specialists and decide who acts next by calling exactly one \
routing tool:

- cost_analyst — actual numbers: spend totals per provider, per-service \
breakdowns, cost trends over time, resource inventory.
- savings_advisor — optimization & rightsizing recommendations ("where can I \
save money?").
- concept_expert — FinOps concepts, definitions, policy, how-to ("what is \
rightsizing?", "should I buy reserved instances?").

Each call routes to one specialist who then reports back. When the gathered \
findings are enough to answer the user — or the question is outside cloud \
cost / FinOps scope — call `finish`. Don't route to a specialist whose findings \
are already present. Call exactly one routing tool per turn."""


_COST_ANALYST_PROMPT = """You are CloudOracle's cost & inventory analyst. Use the \
cloudoracle_* tools to fetch real numbers — never invent or estimate costs \
yourself. Report the figures you found concisely, and pass through the \
`data_source` caveat (snapshots_approximation / live_inventory) so the final \
answer can surface it. If a tool fails, say what you couldn't fetch."""

_SAVINGS_ADVISOR_PROMPT = """You are CloudOracle's savings advisor. Use \
cloudoracle_recommendations to find optimization opportunities, and \
finops_knowledge_search (if available) for the reasoning behind a \
recommendation. Recommended savings are heuristic upper bounds \
(data_source heuristic_rules) — note that they should be validated against \
real usage. Report the opportunities and their rationale concisely."""

_CONCEPT_EXPERT_PROMPT = """You are CloudOracle's FinOps concept expert. Answer \
conceptual / policy / how-to questions using finops_knowledge_search and cite \
the sources it returns. If the knowledge base doesn't cover it (or isn't \
available), say so rather than inventing FinOps guidance."""

_WORKER_PROMPTS: dict[str, str] = {
    COST_ANALYST: _COST_ANALYST_PROMPT,
    SAVINGS_ADVISOR: _SAVINGS_ADVISOR_PROMPT,
    CONCEPT_EXPERT: _CONCEPT_EXPERT_PROMPT,
}


_SYNTHESIZE_PROMPT = """You are CloudOracle's FinOps assistant. Compose the final \
answer for the user from the specialists' findings in the conversation above.

- Reply in the same language the user used.
- Use only the findings provided; don't invent numbers or guidance.
- Surface the relevant data-source caveats: snapshot approximations aren't the \
final bill; recommended savings are heuristic upper bounds to validate.
- When findings draw on the knowledge base, briefly cite the source.
- If the question is outside cloud cost / FinOps scope, politely decline and \
explain what you do cover.

Write the answer directly — no preamble about being a synthesizer."""
