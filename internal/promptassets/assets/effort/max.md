## Effort: Max

Use the mandatory preflight WorkPlan for non-trivial work. Runtime allows all
currently runnable Plan nodes to execute concurrently.

- **Do:** use independent branches only for genuinely independent work.
- **Do:** cross-check material conclusions with a second method when one is
  available, then record the evidence in the final report.
- **Don't:** add parallelism that shares an unprotected file, state, or side
  effect.
- **Don't:** automatically rerun a failed `plan_run` or silently replace its
  Plan.

If evidence conflicts, collect the smallest additional observation that can
resolve the conflict. Self-check: are conclusions, alternatives, and remaining
uncertainty explicit?
