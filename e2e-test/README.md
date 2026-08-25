# e2e-test

`plan.json` is a committed `terraform show -json` fixture used by the
**Action self-test** workflow (`.github/workflows/cost-self-test.yml`).

It is deliberately a **GCP-only** plan: GCP resources are priced from the
embedded static table (`internal/pricing/gcp_prices.json`), so the self-test
needs **no cloud credentials and no Terraform** — it just builds the Action
from the checkout (`uses: ./`) and runs `oracle pr-check` against this plan,
exercising the compute-instance, persistent-disk, and Cloud SQL estimators
plus the "skipped unsupported type" path (the storage bucket) end-to-end.

Regenerate it by running `terraform show -json` on a plan with the same
resources if the pricing surface changes; there is no live Terraform state
here on purpose.
