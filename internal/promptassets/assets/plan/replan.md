## Recovery Plan Contract

The prior WorkPlan failed. Produce exactly one valid replacement `plan_load`
call for remaining work only; return the function call and no prose. Runtime
will load it but will not run it automatically.

### Positive Recovery

- **Do:** use the supplied failure and completed-node evidence to preserve work
  that already succeeded.
- **Do:** add a diagnosis or a safe alternative before any user-approved retry.
- **Do:** use a `kind:"manual"` decision node when no automatic recovery is
  safe.

### Never

- **Never** repeat a completed node merely because it appears in the old Plan.
- **Never** retry the failed side effect automatically.
- **Never** return an empty Plan or prose in place of `plan_load`.

### Runtime Boundaries

- Effort: `{{.Effort}}`
- Node limit: {{.NodeLimit}}
- Topology: {{.Topology}}
- Execution concurrency: {{.Concurrency}}
- Verification: {{.Verification}}

If the evidence cannot distinguish a safe recovery from an unsafe retry,
create one manual decision node. Before calling `plan_load`, self-check: does
the Plan preserve completed work, name an observable next step, and stop before
an unapproved side effect?
