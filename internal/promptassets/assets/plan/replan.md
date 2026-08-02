## Recovery Plan Contract

The prior WorkPlan failed. Produce exactly one valid replacement `plan_load`
call for remaining work only; return the function call and no prose. Runtime
will load it but will not run it automatically.

**Use this recovery path when:** a loaded Plan has factual failure evidence
and the remaining work needs a new, safe task structure. **Do not use it when:**
the original task was a direct reply, a small correction can proceed without a
new DAG, or the failed action may have had an unobserved side effect. In the
last case, ask the user for a decision instead of inventing a retry.

### Positive Recovery

Copy this complete recovery shape first, then replace only the `input` text:

```json
{"entry":"diagnose","nodes":{"diagnose":{"input":"use the failure and completed-node evidence to identify the next safe action"},"decide":{"input":"present the safe recovery or the required user decision","kind":"manual"}},"edges":{"diagnose":["decide"]}}
```

- Preserve completed work; do not repeat a completed node merely because it
  appears in the old Plan.
- Add a diagnosis or safe alternative before any user-approved retry.
- Use `kind:"manual"` when no automatic recovery is safe.

Do not retry a failed side effect automatically or return prose in place of
the required `plan_load` call.

### Runtime Boundaries

- Effort: `{{.Effort}}`
- Node limit: {{.NodeLimit}}
- Topology: {{.Topology}}
- Execution concurrency: {{.Concurrency}}
- Verification: {{.Verification}}

If the evidence cannot distinguish a safe recovery from an unsafe retry, keep
the `decide` manual node. Before calling `plan_load`, self-check: are `entry`,
`nodes`, and `edges` the only top-level keys; does the Plan preserve completed
work; does it name an observable next step; and does it stop before an
unapproved side effect?
