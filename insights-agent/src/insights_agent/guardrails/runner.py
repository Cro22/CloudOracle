"""Guarded agent run: orchestrate the graph, validation, and fallback.

`run_guarded` is the single entry point the CLI and the HTTP surface both use,
so the guardrail policy lives in one place:

  1. run the supervisor graph (cost caps enforced inside it);
  2. validate the answer against the tool observations (layered);
  3. on a run exception *or* a failed validation, replace the answer with a
     deterministic, no-LLM fallback rendered from whatever data is available.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from langchain_core.language_models import BaseChatModel

from insights_agent.graph.supervisor import ask_supervisor
from insights_agent.guardrails.fallback import deterministic_answer
from insights_agent.guardrails.validation import ValidationResult, validate_answer


@dataclass
class GuardedResult:
    """What the CLI / HTTP layer renders: the final (possibly fallback) answer
    plus the metadata needed for --json output and observability."""

    answer: str
    tool_calls: list[dict[str, Any]] = field(default_factory=list)
    observations: list[dict[str, Any]] = field(default_factory=list)
    validation: ValidationResult | None = None
    fallback_used: bool = False
    error: str | None = None


async def run_guarded(
    graph: Any,
    question: str,
    *,
    validate: bool = True,
    judge_model: BaseChatModel | None = None,
) -> GuardedResult:
    """Run `question` through `graph` with validation + deterministic fallback.

    `judge_model` enables the LLM judge layer when supplied; pass None to use
    only the deterministic grounding check.
    """
    try:
        result = await ask_supervisor(graph, question)
    except Exception as e:  # the run itself failed (quota, timeout, bug)
        return GuardedResult(
            answer=deterministic_answer(
                question, [], reason=f"the assistant run failed ({e})"
            ),
            fallback_used=True,
            error=str(e),
        )

    verdict: ValidationResult | None = None
    if validate:
        verdict = await validate_answer(
            result.answer,
            result.observations,
            judge_model=judge_model,
            question=question,
        )
        if not verdict.valid:
            return GuardedResult(
                answer=deterministic_answer(
                    question,
                    result.observations,
                    reason=f"the answer failed {verdict.layer} validation "
                    f"({verdict.reason})",
                ),
                tool_calls=result.tool_calls,
                observations=result.observations,
                validation=verdict,
                fallback_used=True,
            )

    return GuardedResult(
        answer=result.answer,
        tool_calls=result.tool_calls,
        observations=result.observations,
        validation=verdict,
        fallback_used=False,
    )
