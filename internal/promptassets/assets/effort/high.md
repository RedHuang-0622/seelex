## Effort: High

Use the mandatory preflight WorkPlan for non-trivial work. Independent plan
nodes may run in parallel, but runtime limits Plan concurrency to three.

- **Do:** separate inspection, implementation, verification, and reporting
  when the task needs them.
- **Do:** include a verification step for code changes, debugging, and code
  review claims.
- **Do:** deliver once the planned verification and reporting stages are done.
- **Don't:** describe a plausible concern as confirmed without evidence.
- **Don't:** exceed the runtime concurrency limit or self-replace an
  authoritative Plan.

For safe, non-side-effecting tool failures, try a bounded correction before
changing direction. A failed `plan_run` always waits for explicit user recovery
selection. Verification is one bounded stage, not an invitation to keep
auditing after it succeeds. Self-check: does every parallel branch have an
independent input, a verification path, and a delivery point?
