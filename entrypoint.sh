#!/bin/sh
# CloudOracle Action entrypoint (Hito 16.4).
#
# Translates GitHub Action `inputs:` (delivered as INPUT_* env vars) into
# `oracle pr-check` flags, then exec's the binary so its exit code is the
# Action's exit code (1=input, 2=pricing, 3=output, 4=github).
#
# POSIX-only — no bashisms — because the alpine base ships /bin/sh as
# busybox ash. Run shellcheck under -s sh to catch regressions.
set -eu

# --- Required input -------------------------------------------------------
if [ -z "${INPUT_PLAN_FILE:-}" ]; then
    echo "::error::plan-file input is required" >&2
    exit 1
fi

# --- Build the argv incrementally ----------------------------------------
# `set -- ...` rewrites positional parameters; each `set -- "$@" ...` line
# appends to the existing argv. This is the POSIX-portable way to build
# a list when arrays aren't available.
set -- --plan-file="${INPUT_PLAN_FILE}"
set -- "$@" --region="${INPUT_REGION:-us-east-2}"
set -- "$@" --marker="${INPUT_MARKER:-cloudoracle-pr-v1}"

if [ "${INPUT_NO_LLM:-false}" = "true" ]; then
    set -- "$@" --no-llm
fi

if [ -n "${INPUT_OUTPUT_FILE:-}" ]; then
    set -- "$@" --output="${INPUT_OUTPUT_FILE}"
fi

# --- Auto-post on pull_request[_target] events ---------------------------
# GitHub sets GITHUB_EVENT_NAME and GITHUB_REF for us. For PR events,
# GITHUB_REF takes the form `refs/pull/{N}/merge` (default) or
# `refs/pull/{N}/head` (when checkout-merge-commit:false is configured).
# We strip the `refs/pull/` prefix, then strip the trailing `/merge` or
# `/head` to get just the PR number. If the ref doesn't match that
# shape, the substitution is a no-op (`PR_REF == GITHUB_REF`) and we
# skip posting with a warning rather than blindly POSTing with a bad
# value.
event="${GITHUB_EVENT_NAME:-}"
if [ "$event" = "pull_request" ] || [ "$event" = "pull_request_target" ]; then
    PR_REF="${GITHUB_REF#refs/pull/}"
    PR_NUMBER="${PR_REF%/*}"

    if [ -n "$PR_NUMBER" ] && [ "$PR_REF" != "${GITHUB_REF:-}" ]; then
        set -- "$@" --post
        set -- "$@" --repo="${GITHUB_REPOSITORY}"
        set -- "$@" --pr="${PR_NUMBER}"
        if [ -n "${INPUT_GITHUB_TOKEN:-}" ]; then
            set -- "$@" --token="${INPUT_GITHUB_TOKEN}"
        fi
    else
        echo "::warning::Could not extract PR number from GITHUB_REF=${GITHUB_REF:-<unset>}; rendering only, not posting." >&2
    fi
else
    echo "::notice::Not a pull_request event (event=${event:-<unset>}); rendering only, not posting." >&2
fi

exec /usr/local/bin/oracle pr-check "$@"
