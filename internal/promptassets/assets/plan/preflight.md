## Plan Compilation Contract

Compile the user request into exactly one valid `plan_load` call. Return the
function call only; do not return prose. Runtime will validate and load the
Plan before normal ReAct begins.

### Positive Shape

Prefer this canonical object shape:

```json
{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"report":{"input":"report"}},"edges":{"inspect":["report"]}}
```

### Runtime Boundaries

- Effort: `{{.Effort}}`
- Node limit: {{.NodeLimit}}
- Topology: {{.Topology}}
- Execution concurrency: {{.Concurrency}}
- Verification: {{.Verification}}

### Task Templates

- For a code review or architecture audit, use the exact node IDs `inspect`,
  `verify`, and `report`: entry `inspect`, then `inspect -> verify -> report`.
  The `inspect` node reads source; `verify` checks each material claim through
  a call path, test, or observed runtime evidence; `report` summarizes the
  result. A planning document alone is not proof.
  Emit this complete canonical skeleton before filling the inputs:

  ```json
  {"entry":"inspect","nodes":{"inspect":{"input":"read source"},"verify":{"input":"verify claims"},"report":{"input":"report findings"}},"edges":{"inspect":["verify"],"verify":["report"]}}
  ```
- For a code change, plan `inspect -> implement -> verify -> report`.
- For research, plan `collect evidence -> cross-check material claims ->
  report uncertainty`.
- For a simple reply-only request, use the smallest valid Plan instead of
  inventing implementation or test work.

If task classification is uncertain, prefer an inspect node followed by a
bounded verification node. Before calling `plan_load`, self-check: is `entry`
a node key, is every edge complete, and can every node's completion be
observed?

### Invalid Shape

- **Never** put node IDs such as `inspect` or `verify` beside `entry`,
  `nodes`, and `edges` at the top level.
- **Never** use arrays for `nodes` or `edges`; the preflight tool requires the
  object shape shown above.
- **Never** encode arrows as an `edges` string. Use the adjacency object from
  the audit skeleton instead.
- **Never** use an outline in prose instead of the required function call.
- **Never** set `"entry":"inspect"` while naming the actual node
  `inspect_source`, `inspect-files`, or another different ID. Node IDs are
  exact keys, not descriptive phrases.
