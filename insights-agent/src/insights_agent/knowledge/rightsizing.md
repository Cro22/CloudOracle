# Rightsizing cloud resources

Rightsizing means matching a resource's provisioned capacity to its actual
demand. It is usually the largest source of recoverable cloud waste because
most teams over-provision "to be safe" and never revisit the decision.

## Signals that a resource is a rightsizing candidate

- **Low average CPU / memory utilization.** A compute instance averaging
  under ~5% CPU over a sustained window (e.g. a week or more) is effectively
  idle. CloudOracle flags these as `ec2-idle` (High severity).
- **Sustained low database utilization.** A managed database averaging under
  ~10% CPU is likely oversized; the next smaller instance tier typically
  covers the real load. CloudOracle flags these as `rds-oversized` (Medium).
- **Orphaned storage.** A disk volume with zero usage / no attachment is pure
  waste — nothing reads or writes it. CloudOracle flags these as `ebs-orphan`
  (High). The fix is deletion (after a snapshot if the data may be needed).
- **Over-provisioned serverless.** A function configured with far more memory
  than its invocations use pays for headroom it never touches. CloudOracle
  flags these as `lambda-over-provisioned` (Low).

## How to act

1. **Confirm the signal against real usage.** A heuristic flag is a starting
   point, not proof. Check a longer utilization window and peak (not just
   average) demand before resizing — a nightly batch job can look idle for
   23 hours a day.
2. **Resize down one step at a time.** Drop to the next smaller instance
   class or tier, then re-measure. Aggressive jumps risk throttling or OOM.
3. **Prefer architectural fixes for structural waste.** Idle instances that
   exist only for occasional work are better moved to autoscaling, scheduled
   shutdown, or serverless than merely shrunk.
4. **Delete, don't shrink, orphans.** Orphaned volumes and unattached IPs have
   no smaller size — the only rightsizing is removal.

## Savings expectation

Rightsizing savings are an estimated upper bound: shutting down a flagged idle
instance recovers its full monthly cost; downsizing a tier typically recovers
~50%. Realized savings depend on the workload tolerating the smaller footprint,
so validate before acting.
