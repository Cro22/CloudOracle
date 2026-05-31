"""`insights-agent-serve` console script: run the HTTP surface with uvicorn."""

from __future__ import annotations

import sys

from insights_agent.api.app import create_app


def serve_entrypoint(argv: list[str] | None = None) -> int:  # pragma: no cover
    import uvicorn
    from pydantic import ValidationError

    from insights_agent.config import Settings

    try:
        settings = Settings()  # type: ignore[call-arg]
    except ValidationError as e:
        print(f"Configuration error:\n{e}", file=sys.stderr)
        return 2

    app = create_app(settings=settings)
    uvicorn.run(app, host=settings.agent_host, port=settings.agent_port)
    return 0


if __name__ == "__main__":  # pragma: no cover
    sys.exit(serve_entrypoint())
