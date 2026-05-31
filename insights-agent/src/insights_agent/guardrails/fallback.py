"""Deterministic, no-LLM fallback answer.

Used when the LLM path fails (quota, timeout) or the answer fails validation.
The guiding principle is *honesty over fluency*: never fabricate a narrative.
Either hand the user the raw tool data they can verify, or tell them plainly
that nothing could be retrieved.
"""

from __future__ import annotations

# Cap a single observation's rendered length so the fallback stays readable
# even if a tool returned a large payload.
_MAX_OUTPUT_CHARS = 600


def _truncate(text: str) -> str:
    if len(text) <= _MAX_OUTPUT_CHARS:
        return text
    return text[:_MAX_OUTPUT_CHARS] + " …(truncated)"


def deterministic_answer(
    question: str,
    observations: list[dict[str, str]],
    *,
    reason: str,
) -> str:
    """Render a safe answer from whatever grounded data the run produced."""
    lines = [
        "I couldn't return a verified answer to your question"
        + (f' ("{question}")' if question else "")
        + ".",
        f"Reason: {reason}.",
    ]
    if observations:
        lines.append("")
        lines.append(
            "Here is the raw data the tools returned, which you can verify directly:"
        )
        for obs in observations:
            name = obs.get("name", "tool")
            output = _truncate(str(obs.get("output", "")))
            lines.append(f"- {name}: {output}")
    else:
        lines.append(
            "No data was retrieved from CloudOracle. Check that the server is "
            "reachable and your API key is correct, then try again."
        )
    return "\n".join(lines)
