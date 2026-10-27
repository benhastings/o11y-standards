# o11y-standards

The SRE org's telemetry standard, the Weaver gate that enforces it, and the
libraries that make following it the easy path.

| Page | Read it if you're… |
|---|---|
| [00 — Team notes](00-team-notes.md) | New here: goals, decisions, placeholders, open questions |
| [01 — Metadata](01-telemetry-standard.md) | Instrumenting or querying anything: every attribute, where it goes (resource, wide event, span), and what to keep for metrics |
| [02 — Wide events](02-logging-standard-wide-events.md) | Emitting or querying wide events, or moving off span events |
| [03 — The Weaver gate](03-weaver-gate.md) | Adding the gate to a pipeline, or fixing a finding |
| [04 — Go guide](04-go-guide.md) | Building a Go API with otelkit |
| [05 — Front-end guide](05-frontend-guide.md) | Building a React UI with otel-web |

**Guides for service teams.** Standalone guidance, not tied to this
repository's tooling, for teams adopting the same practices anywhere:

| Guide | Covers |
|---|---|
| [Metadata standard](guides/metadata-standard.md) | Which attributes to set, naming your own, sensitive data, cardinality |
| [Wide events](guides/wide-events.md) | One log-based event per unit of work: what goes in, how to emit and query it |
| [OpenTelemetry Weaver](guides/weaver.md) | Defining a registry, checking it, and checking live telemetry in CI |

The registry in `weaver/registry/` is the source of truth for attribute
names. The code, registry and CI live in the
[repository](https://github.com/benhastings/o11y-standards).
