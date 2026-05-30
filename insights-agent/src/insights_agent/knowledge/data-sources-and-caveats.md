# CloudOracle data sources and their caveats

Every CloudOracle API response carries a `data_source` field. It tells you how
the numbers were produced and therefore how much to trust them. The agent
should surface the matching caveat whenever accuracy materially affects an
answer.

## `snapshots_approximation` — the cost endpoints

Used by cost-summary, cost-by-service, and cost-trends.

- **What it is.** CloudOracle periodically records each provider/service's
  *projected monthly cost rate* into a `cost_snapshots` table. A period total
  is the average of those snapshot rates over the period, scaled to the
  period length (`average monthly rate × days / 30`).
- **What it is NOT.** It is not billed spend from a Cost Explorer / billing
  API. It will not match an invoice to the cent, and it cannot see
  one-off charges, taxes, credits, or refunds.
- **How to phrase it.** "Based on snapshot approximations, roughly $X." The
  real billing integration lands in a later milestone (8.7).

## `heuristic_rules` — the recommendations endpoint

- **What it is.** Rule-based analysis over the current resource inventory
  (idle, oversized, orphaned, over-provisioned). Each rule estimates a
  monthly saving.
- **What it is NOT.** Not a guarantee. `monthly_savings_usd` is an *upper
  bound* assuming the resource can be removed or downsized without impact.
- **How to phrase it.** "Estimated savings of up to $X — validate against real
  usage before acting."

## `live_inventory` — the inventory endpoint

- **What it is.** Counts and cost from the latest resource scan, aggregated by
  provider and service.
- **What it is NOT.** `monthly_cost_usd` is the sum of per-resource *projected
  monthly rates* at scan time, not billed spend, and it reflects only what the
  scan discovered.

## Why this matters

Conflating these leads to wrong conclusions — e.g. treating a recommendation's
upper-bound saving as money already banked, or comparing a snapshot
approximation directly against an invoice. Always read `data_source` first.
