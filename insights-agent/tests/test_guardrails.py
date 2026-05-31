"""Guardrails: layered validation, deterministic fallback, guarded runner."""

from __future__ import annotations

from typing import Any

import pytest
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult

from insights_agent.graph.basic import AgentResult
from insights_agent.guardrails import runner as runner_mod
from insights_agent.guardrails.fallback import deterministic_answer
from insights_agent.guardrails.runner import run_guarded
from insights_agent.guardrails.validation import (
    deterministic_grounding,
    extract_money_figures,
    validate_answer,
)


class _JudgeModel(BaseChatModel):
    verdict: str = "PASS"

    @property
    def _llm_type(self) -> str:
        return "judge-fake"

    def _generate(
        self, messages: list[BaseMessage], stop: list[str] | None = None,
        run_manager: Any = None, **kwargs: Any,
    ) -> ChatResult:
        return ChatResult(
            generations=[ChatGeneration(message=AIMessage(content=self.verdict))]
        )

    async def _agenerate(
        self, messages: list[BaseMessage], stop: list[str] | None = None,
        run_manager: Any = None, **kwargs: Any,
    ) -> ChatResult:
        return self._generate(messages)


def _obs(name: str, output: str) -> dict[str, str]:
    return {"name": name, "output": output}


class TestExtractFigures:
    def test_dollar_and_usd_forms(self) -> None:
        figs = extract_money_figures("We spent $1,234.56 and 50 USD, plus $150.")
        assert figs == [1234.56, 150.0, 50.0]

    def test_ignores_plain_numbers(self) -> None:
        # Years / counts without a currency anchor are not money figures.
        assert extract_money_figures("In 2026 you had 12 instances.") == []


class TestDeterministicGrounding:
    def test_grounded_when_figure_in_observations(self) -> None:
        obs = [_obs("cloudoracle_cost_summary", '{"grand_total_usd": 150.0}')]
        r = deterministic_grounding("You spent about $150 on AWS.", obs)
        assert r.grounded and r.ungrounded == []

    def test_ungrounded_figure_is_flagged(self) -> None:
        obs = [_obs("cloudoracle_cost_summary", '{"grand_total_usd": 150.0}')]
        r = deterministic_grounding("You spent $999 on AWS.", obs)
        assert not r.grounded
        assert r.ungrounded == [999.0]

    def test_no_figures_is_grounded(self) -> None:
        r = deterministic_grounding("Rightsizing matches capacity to demand.", [])
        assert r.grounded and r.figures == []

    def test_rounding_tolerance(self) -> None:
        obs = [_obs("t", '{"total_usd": 149.99}')]
        assert deterministic_grounding("about $150", obs).grounded


class TestValidateAnswer:
    async def test_ungrounded_fails_deterministically_without_judge(self) -> None:
        judge = _JudgeModel(verdict="PASS")
        obs = [_obs("t", '{"total_usd": 150.0}')]
        res = await validate_answer("You spent $999.", obs, judge_model=judge)
        assert not res.valid
        assert res.layer == "deterministic"
        assert "999" in res.reason

    async def test_grounded_with_figures_escalates_to_judge_pass(self) -> None:
        judge = _JudgeModel(verdict="PASS")
        obs = [_obs("t", '{"total_usd": 150.0}')]
        res = await validate_answer("You spent $150.", obs, judge_model=judge)
        assert res.valid and res.layer == "judge"

    async def test_grounded_with_figures_judge_fail(self) -> None:
        judge = _JudgeModel(verdict="FAIL: the $150 is GCP, not AWS")
        obs = [_obs("t", '{"total_usd": 150.0}')]
        res = await validate_answer("You spent $150 on AWS.", obs, judge_model=judge)
        assert not res.valid and res.layer == "judge"
        assert "GCP" in res.reason

    async def test_no_judge_model_accepts_grounded(self) -> None:
        obs = [_obs("t", '{"total_usd": 150.0}')]
        res = await validate_answer("You spent $150.", obs, judge_model=None)
        assert res.valid and res.layer == "deterministic"

    async def test_no_figures_skips_judge(self) -> None:
        # Judge would fail, but with no numeric claims it's never consulted.
        judge = _JudgeModel(verdict="FAIL: should not be called")
        res = await validate_answer("Rightsizing matches capacity.", [], judge_model=judge)
        assert res.valid and res.layer == "deterministic"


class TestFallback:
    def test_renders_observations(self) -> None:
        out = deterministic_answer(
            "spend?", [_obs("cost", '{"grand_total_usd": 150.0}')], reason="run failed"
        )
        assert "run failed" in out
        assert "cost: " in out
        assert "150" in out

    def test_no_observations_message(self) -> None:
        out = deterministic_answer("spend?", [], reason="boom")
        assert "No data was retrieved" in out

    def test_truncates_long_output(self) -> None:
        out = deterministic_answer("q", [_obs("t", "x" * 2000)], reason="r")
        assert "(truncated)" in out


class TestRunGuarded:
    async def test_happy_path_no_fallback(self, monkeypatch: pytest.MonkeyPatch) -> None:
        async def fake_ask(graph: Any, q: str) -> AgentResult:
            return AgentResult(
                answer="You spent $150 on AWS.",
                tool_calls=[{"name": "cloudoracle_cost_summary", "args": {}}],
                observations=[_obs("cloudoracle_cost_summary", '{"grand_total_usd": 150.0}')],
            )

        monkeypatch.setattr(runner_mod, "ask_supervisor", fake_ask)
        result = await run_guarded(object(), "spend?", validate=True, judge_model=None)
        assert not result.fallback_used
        assert "$150" in result.answer
        assert result.validation is not None and result.validation.valid

    async def test_exception_triggers_fallback(self, monkeypatch: pytest.MonkeyPatch) -> None:
        async def boom(graph: Any, q: str) -> AgentResult:
            raise RuntimeError("gemini quota exceeded")

        monkeypatch.setattr(runner_mod, "ask_supervisor", boom)
        result = await run_guarded(object(), "spend?")
        assert result.fallback_used
        assert result.error is not None and "quota" in result.error
        assert "couldn't return a verified answer" in result.answer

    async def test_invalid_answer_triggers_fallback(self, monkeypatch: pytest.MonkeyPatch) -> None:
        async def fake_ask(graph: Any, q: str) -> AgentResult:
            return AgentResult(
                answer="You spent $999 on AWS.",  # not in observations
                tool_calls=[{"name": "cloudoracle_cost_summary", "args": {}}],
                observations=[_obs("cloudoracle_cost_summary", '{"grand_total_usd": 150.0}')],
            )

        monkeypatch.setattr(runner_mod, "ask_supervisor", fake_ask)
        result = await run_guarded(object(), "spend?", validate=True)
        assert result.fallback_used
        assert result.validation is not None and not result.validation.valid
        # The honest fallback surfaces the real data ($150), not the bad claim.
        assert "150" in result.answer

    async def test_validation_disabled_skips_checks(self, monkeypatch: pytest.MonkeyPatch) -> None:
        async def fake_ask(graph: Any, q: str) -> AgentResult:
            return AgentResult(answer="$999 ungrounded", observations=[])

        monkeypatch.setattr(runner_mod, "ask_supervisor", fake_ask)
        result = await run_guarded(object(), "q", validate=False)
        assert not result.fallback_used
        assert result.validation is None
        assert result.answer == "$999 ungrounded"
