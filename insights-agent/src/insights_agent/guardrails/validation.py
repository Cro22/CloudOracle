"""Layered semantic answer validation.

Two layers, cheapest first:

1. **Deterministic grounding.** Pull the monetary figures out of the answer and
   confirm each appears (within tolerance) among the numbers in the tool
   observations. A figure that matches nothing is almost certainly fabricated —
   a hard fail, no LLM needed.

2. **LLM judge.** When the deterministic layer passes *but the answer makes
   numeric claims* (so there's something to get subtly wrong — wrong
   attribution, wrong period, a real number used misleadingly), an optional
   judge model gives a second opinion grounded in the observations.

`validate_answer` orchestrates the two. The judge only runs when a model is
supplied and the deterministic layer both passed and found figures.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import SystemMessage

from insights_agent.graph.basic import _stringify_content

# `$1,234.56`, `$150`  — a currency-anchored amount.
_DOLLAR = re.compile(r"\$\s?(\d[\d,]*(?:\.\d+)?)")
# `1,234.56 USD`, `150 usd` — amount followed by the currency code.
_USD_SUFFIX = re.compile(r"(\d[\d,]*(?:\.\d+)?)\s?USD\b", re.IGNORECASE)
# Any number, for scanning the observation haystack.
_NUMBER = re.compile(r"\d[\d,]*(?:\.\d+)?")


def _to_float(raw: str) -> float | None:
    try:
        return float(raw.replace(",", ""))
    except ValueError:  # pragma: no cover - regex already constrains the input
        return None


def extract_money_figures(text: str) -> list[float]:
    """Distinct monetary figures stated in `text` (e.g. "$150", "200 USD")."""
    seen: list[float] = []
    for pattern in (_DOLLAR, _USD_SUFFIX):
        for match in pattern.findall(text):
            value = _to_float(match)
            if value is not None and value not in seen:
                seen.append(value)
    return seen


def _all_numbers(text: str) -> list[float]:
    out: list[float] = []
    for raw in _NUMBER.findall(text):
        value = _to_float(raw)
        if value is not None:
            out.append(value)
    return out


def _is_grounded(figure: float, numbers: list[float]) -> bool:
    # Tolerance absorbs rounding ("$150" vs 149.99): 1% of the figure, min 1 cent.
    tol = max(0.01, abs(figure) * 0.01)
    return any(abs(figure - n) <= tol for n in numbers)


@dataclass(frozen=True)
class GroundingResult:
    grounded: bool
    figures: list[float] = field(default_factory=list)
    ungrounded: list[float] = field(default_factory=list)


def deterministic_grounding(
    answer: str, observations: list[dict[str, str]]
) -> GroundingResult:
    """Check every monetary figure in `answer` appears in the observations."""
    figures = extract_money_figures(answer)
    if not figures:
        return GroundingResult(grounded=True)

    haystack: list[float] = []
    for obs in observations:
        haystack.extend(_all_numbers(str(obs.get("output", ""))))

    ungrounded = [f for f in figures if not _is_grounded(f, haystack)]
    return GroundingResult(
        grounded=not ungrounded, figures=figures, ungrounded=ungrounded
    )


@dataclass(frozen=True)
class ValidationResult:
    """Outcome of validating an answer. `layer` is which check decided it."""

    valid: bool
    layer: str  # "deterministic" | "judge" | "skipped"
    reason: str = ""


def _fmt(values: list[float]) -> str:
    return ", ".join(f"${v:,.2f}" for v in values)


async def validate_answer(
    answer: str,
    observations: list[dict[str, str]],
    *,
    judge_model: BaseChatModel | None = None,
    question: str = "",
) -> ValidationResult:
    """Run the deterministic check, then the LLM judge if warranted."""
    grounding = deterministic_grounding(answer, observations)
    if not grounding.grounded:
        return ValidationResult(
            valid=False,
            layer="deterministic",
            reason=(
                f"answer states {_fmt(grounding.ungrounded)} not found in any "
                "tool result"
            ),
        )

    # Deterministic layer passed. Escalate to the judge only when there are
    # numeric claims to second-guess and a judge model is available.
    if grounding.figures and judge_model is not None:
        return await _judge(judge_model, question, answer, observations)

    return ValidationResult(valid=True, layer="deterministic")


async def _judge(
    model: BaseChatModel,
    question: str,
    answer: str,
    observations: list[dict[str, str]],
) -> ValidationResult:
    obs_text = "\n".join(
        f"- {o.get('name', '?')}: {o.get('output', '')}" for o in observations
    ) or "(no tool observations)"
    prompt = _JUDGE_PROMPT.format(
        question=question or "(not provided)", answer=answer, observations=obs_text
    )
    resp = await model.ainvoke([SystemMessage(prompt)])
    verdict = _stringify_content(resp.content).strip()
    # Fail-open on an empty/garbled verdict (don't block a good answer on a
    # malformed judge reply); only an explicit FAIL rejects.
    if verdict.upper().startswith("FAIL"):
        reason = verdict.split(":", 1)[1].strip() if ":" in verdict else "judge rejected the answer"
        return ValidationResult(valid=False, layer="judge", reason=reason)
    return ValidationResult(valid=True, layer="judge")


_JUDGE_PROMPT = """You are a strict FinOps answer validator. Decide whether every \
factual and numeric claim in the assistant's answer is supported by the tool \
observations below. Watch for fabricated numbers, wrong attribution (right \
number, wrong provider/service/period), and claims with no supporting data.

User question:
{question}

Assistant answer:
{answer}

Tool observations:
{observations}

Reply with exactly "PASS" if every claim is supported, or "FAIL: <short reason>" \
if any claim is unsupported or misleading."""
