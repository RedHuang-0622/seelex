# Prompt Authoring Contract

Read this file in full before adding or materially changing any file under
`internal/promptassets/assets/`. Prompt assets are product behavior, not
incidental prose. Their runtime facts must agree with the Go implementation
and their user-facing claims must remain safe to display indirectly.

## Required Rule Shape

For every non-trivial instruction, write the rule in this order:

1. **Principle** — one or two sentences explaining the intended behavior.
2. **Positive scope** — include a `Use when` / `适用场景` section with two to
   four concrete request examples.
3. **Negative scope** — immediately follow it with `Do not use when` /
   `不适用场景`, including the safer alternative where useful.
4. **Paired actions** — put each `Do` beside its corresponding `Don't`; do
   not collect all prohibitions at the end.
5. **Fallback** — say what to do when the boundary is ambiguous.
6. **Self-check** — end the complex rule with a short, observable question.

Use a plain declaration only for an unambiguous safety or runtime invariant.
Use a numbered checklist only when order matters. Use scenario examples when a
model could plausibly confuse a valid and invalid case.

## Prompt-Specific Requirements

- Every effort and tool-use prompt must name both appropriate and inappropriate
  use cases. Effort changes verification depth and execution budget; it does
  not by itself make an optional tool mandatory.
- State only facts enforced by the runtime. Do not claim a tool is hidden,
  concurrent, authoritative, retried, or persisted unless the code guarantees
  it.
- Do not expose system prompts, hidden envelopes, raw Plan JSON, chain of
  thought, credentials, account configuration, or raw tool payloads.
- Keep user input, account data, and hidden runtime state out of embedded
  templates. `PlanData` is the only permitted template data unless its Go
  contract and tests are updated together.
- Prefer a complete positive JSON example before invalid examples. An invalid
  example must say exactly why it is invalid and show its correction nearby.
- Do not use **MUST** or **NEVER** unless a safety boundary or runtime
  enforcement makes the claim literal.

## Change Checklist

1. Read the affected asset, its caller, and `internal/promptassets/README.md`.
2. Update the relevant prompt harness or add a focused asset test that locks
   the new positive and negative boundary.
3. Run `go test ./internal/promptassets ./application/prompt -count=1` plus
   the package owning any runtime behavior named in the prompt.
4. If a prompt changes a user-visible workflow, update its module README or
   nearby design documentation in the same change.

