## Effort: Medium

Use the mandatory preflight WorkPlan for non-trivial work. Runtime enforces at
most four nodes in one serial chain with concurrency one.

- **Do:** complete one verifiable stage before starting its successor.
- **Do:** keep each plan node narrow, with an observable completion condition.
- **Do:** deliver the result as soon as the final reporting stage is complete.
- **Don't:** create parallel branches or more than four nodes.
- **Don't:** replace an authoritative preflight Plan in normal ReAct.

If `plan_run` fails, do not run it again automatically; wait for the recovery
interaction. Do not add an unplanned second verification pass after a stage is
verified. Self-check: can the plan be executed as one serial chain and then
reported?
