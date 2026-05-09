# CloudOracle Action — Workflow Examples

These two YAML files are runnable references for wiring the CloudOracle
Action into a Terraform repository. Pick whichever matches your auth
posture and LLM appetite, copy it under `.github/workflows/` in your
target repo, and adjust the paths and IAM ARN.

## Which example do I want?

| File | AWS auth | LLM narrative | Best for |
|------|----------|---------------|----------|
| [`terraform-plan.yml`](terraform-plan.yml) | OIDC (recommended) | Yes (Anthropic / Gemini / OpenAI) | Production setups, security-conscious orgs |
| [`terraform-plan-no-llm.yml`](terraform-plan-no-llm.yml) | Static access keys | No (templated text) | Quick start, no LLM procurement, air-gapped CI |

Both produce a single PR comment that updates in place across pushes
(via the `cloudoracle-pr-v1` HTML marker), so the conversation thread
stays clean.

## Required permissions

The workflow needs the following at minimum:

```yaml
permissions:
  pull-requests: write   # to post/update the comment
  contents: read         # to checkout the repo
  id-token: write        # ONLY when using OIDC (omit for static keys)
```

GitHub's default workflow permissions vary by org policy; declaring
them explicitly makes the workflow portable.

## AWS IAM setup for OIDC

The OIDC example assumes a role trust policy of the form:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Federated": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
      },
      "StringLike": {
        "token.actions.githubusercontent.com:sub": "repo:YOUR_ORG/YOUR_REPO:pull_request"
      }
    }
  }]
}
```

The role only needs `pricing:GetProducts` on `*` — CloudOracle does not
read or modify any AWS resources beyond Pricing API metadata:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": "pricing:GetProducts",
    "Resource": "*"
  }]
}
```

GitHub's OIDC setup guide:
https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/configuring-openid-connect-in-amazon-web-services

## LLM key as a secret

Set one of the following as a repository or organisation secret:

- `ANTHROPIC_API_KEY` (Claude — used by default in v2)
- `GEMINI_API_KEY`
- `OPENAI_API_KEY`

Pass it via `env:` in the step that uses the Action (see
`terraform-plan.yml`). Without any key the Action degrades silently to
the templated narrative — the PR comment is still posted, just less
narrated.

## Action inputs

| Input | Required | Default | What it does |
|-------|----------|---------|--------------|
| `plan-file` | yes | — | Path to `terraform show -json` output |
| `region` | no | `us-east-2` | AWS region for pricing |
| `output-file` | no | `` (empty) | Also write the Markdown to this file (e.g. for artefact upload) |
| `marker` | no | `cloudoracle-pr-v1` | HTML comment marker for upsert |
| `no-llm` | no | `false` | Force templated narrative |
| `github-token` | no | `${{ github.token }}` | Token for the comment POST/PATCH |

## Behaviour notes

- The Action only **posts** when `GITHUB_EVENT_NAME` is `pull_request`
  or `pull_request_target`. Other events render the Markdown and exit;
  use `output-file` to capture it elsewhere.
- The Action exits with differentiated codes:
  - `0`: success
  - `1`: input error (missing plan file, bad flags)
  - `2`: pricing error (AWS API failure)
  - `3`: output error (file write failed)
  - `4`: GitHub error (post/update failed)
