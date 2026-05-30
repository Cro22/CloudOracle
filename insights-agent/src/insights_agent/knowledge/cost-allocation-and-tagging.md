# Cost allocation, tagging, showback and chargeback

You cannot manage what you cannot attribute. Cost allocation is the practice of
mapping each dollar of cloud spend to the team, product, environment, or
customer responsible for it.

## Tagging is the foundation

- **Tags / labels** are key-value metadata on resources (e.g. `team=payments`,
  `env=prod`, `cost-center=4812`). Allocation quality is capped by tag
  coverage and consistency.
- **Untagged or inconsistently tagged resources** fall into an "unallocated"
  bucket that no one owns — the first thing a FinOps practice tries to shrink.
- **A tagging policy** defines a small set of mandatory keys and allowed
  values, enforced at provisioning time (IaC, policy-as-code) rather than
  cleaned up after the fact.

## Showback vs chargeback

- **Showback** reports each team its share of spend for visibility, without
  moving money. Low-friction; drives awareness and behavior change.
- **Chargeback** actually bills the cost back to the team's budget. Higher
  accountability but needs accurate allocation and organizational buy-in.

Most organizations start with showback and graduate to chargeback once
allocation is trusted.

## Shared and unallocable costs

Some costs resist direct tagging — shared clusters, data transfer, support
fees, committed-discount amortization. Common approaches:

- **Proportional split** by a driver (e.g. each team's tagged compute share).
- **Even split** across consuming teams.
- **Dedicated "platform" cost center** that owns shared infrastructure.

Document the method; an explainable split beats a perfectly "fair" but opaque
one.

## Relationship to CloudOracle

CloudOracle resources carry tags and an account id; the inventory and cost
breakdowns aggregate by provider and service. Per-team allocation builds on top
of that by grouping on the tag keys your organization standardizes.
