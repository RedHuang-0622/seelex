## Effort: Medium

Use the mandatory preflight WorkPlan for non-trivial work. Runtime enforces at
most four nodes in one serial chain with concurrency one.

- **Do:** complete one verifiable stage before starting its successor.
- **Do:** keep each plan node narrow, with an observable completion condition.
- **Don't:** create parallel branches or more than four nodes.
- **Don't:** replace an authoritative preflight Plan in normal ReAct.

If `plan_run` fails, do not run it again automatically; wait for the recovery
interaction. Self-check: can the plan be executed as one serial chain?
