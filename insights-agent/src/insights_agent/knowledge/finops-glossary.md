# FinOps glossary

Concise definitions of terms the agent may need when explaining cost concepts.

- **FinOps.** An operational practice that brings financial accountability to
  the variable spend of cloud, through collaboration between engineering,
  finance, and product. Built on three phases: Inform, Optimize, Operate.

- **Unit economics / unit cost.** Cloud cost divided by a business metric
  (cost per order, per active user, per GB processed). Lets cost be judged
  against value rather than in absolute dollars — spend can rise while unit
  cost falls.

- **Amortization.** Spreading an upfront or committed cost (e.g. a 1-year
  Reserved Instance paid all-upfront) evenly across the period it covers,
  instead of booking it all on the purchase day. Amortized views give a
  smoother, more comparable monthly cost.

- **Blended vs unblended cost (AWS).** Unblended is the actual rate each
  account paid; blended averages rates across a consolidated billing family.
  Most cost analysis uses unblended (or amortized) cost.

- **Cost anomaly.** A statistically unusual jump in spend versus the recent
  baseline — often a misconfiguration, a runaway job, or a forgotten resource.
  Detecting anomalies early limits surprise bills.

- **Idle resource.** A provisioned resource doing little or no useful work
  (very low utilization). Distinct from an *orphaned* resource, which is
  unattached and does no work at all.

- **Rightsizing.** Adjusting provisioned capacity to match real demand. See
  the rightsizing note for signals and how to act.

- **Commitment-based discount.** A lower rate in exchange for a 1–3 year usage
  or spend commitment (Reserved Instances, Savings Plans, Committed Use
  Discounts). See the commitment-discounts note.

- **Showback / chargeback.** Reporting (showback) versus actually billing back
  (chargeback) cloud cost to the responsible team. See the cost-allocation
  note.

- **Data source (CloudOracle).** A field on every API response
  (`snapshots_approximation`, `heuristic_rules`, `live_inventory`) describing
  how the numbers were produced and how much to trust them. See the
  data-sources note.
