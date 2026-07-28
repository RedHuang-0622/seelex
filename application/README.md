# Application Core

`application` is the stable public facade consumed by the composition root,
TUI, GUI, and adapters. Its exported API remains unchanged while the
implementation is organized by responsibility:

| Directory | Responsibility |
| --- | --- |
| `model/` | Versioned snapshot, event-facing DTOs, and safe copy helpers. |
| `event/` | Event envelope, subscriptions, and fan-out hub. |
| `approval/` | Asynchronous approval request lifecycle. |
| `contract/` | Application-owned interfaces for engine, runtime, plugins, sessions, and workspaces. |
| `prompt/` | Prompt-layer stack and effort policy. |
| `search/` | Web-search provider implementation. |
| `core/` | Service use cases: chat, commands, input, sessions, diagnostics, and tool hooks. |

Dependencies flow inward: `core` depends on contracts and focused capability
packages; adapters and frontends depend only on the `application` facade.
