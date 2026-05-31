"""Production guardrails around the agent run (milestone 8.5).

- cost/usage caps live with the graph (`graph.supervisor.RunLimits`).
- `validation` checks the answer is grounded in the tool observations
  (deterministic number check, then an optional LLM judge).
- `fallback` renders a deterministic, no-LLM answer when the run fails or the
  answer can't be verified.
- `runner.run_guarded` ties them together.
"""

from insights_agent.guardrails.fallback import deterministic_answer
from insights_agent.guardrails.runner import GuardedResult, run_guarded
from insights_agent.guardrails.validation import (
    ValidationResult,
    validate_answer,
)

__all__ = [
    "GuardedResult",
    "ValidationResult",
    "deterministic_answer",
    "run_guarded",
    "validate_answer",
]
