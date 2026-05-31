# Commitment-based discounts: Reserved Instances, Savings Plans, CUDs

Cloud providers sell capacity cheaper in exchange for a usage commitment.
These are pricing levers, not architectural changes — they lower the rate you
pay for the same resources, so they apply *after* you have rightsized.

## The main instruments

- **Reserved Instances (RIs) — AWS, Azure.** Commit to a specific instance
  family/region for 1 or 3 years. Largest discount (up to ~70%) but least
  flexible: the commitment is tied to the instance shape.
- **Savings Plans — AWS.** Commit to a dollar-per-hour spend level for 1 or 3
  years. More flexible than RIs (applies across instance families and, for
  Compute Savings Plans, across regions and to Fargate/Lambda) at a slightly
  smaller discount.
- **Committed Use Discounts (CUDs) — GCP.** Commit to a level of vCPU/RAM (or
  spend, for flexible CUDs) for 1 or 3 years.

## When commitments make sense

- The workload is **steady-state** — a predictable baseline that runs
  24/7/365. Commit to the baseline, leave the spiky top on on-demand.
- You have already **rightsized**. Committing to oversized capacity locks in
  waste for 1–3 years; rightsize first, then commit to the smaller footprint.
- You can forecast usage with reasonable confidence over the term. Under-using
  a commitment wastes the unused portion; the break-even is typically around
  60–70% utilization of the commitment.

## When to avoid them

- Bursty, seasonal, or declining workloads — on-demand or spot is safer.
- Architectures you expect to change within the term (migration, refactor).
- Before rightsizing: never commit to capacity you are about to shrink.

## Relationship to CloudOracle

CloudOracle's recommendations cover **architectural / sizing** waste (idle,
oversized, orphaned), not pricing-model selection. Commitment planning is a
complementary lever applied to whatever footprint remains after rightsizing.
