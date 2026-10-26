# 05 — Front-end Instrumentation Guide (otel-web)

`@kfo11y/otel-web` instruments a React UI so that:

- page loads and API calls are traced, and API calls carry `traceparent` to
  the Go services, so one trace spans browser → API → database;
- each meaningful user action emits one `kfo11y.ui.interaction.summary` wide
  event, tied to that trace.

Browser semantic conventions are still in development upstream, so the
front-end standard leans on org attributes (`kfo11y.ui.*`) and keeps the
surface small.

## 1. Initialize before React renders

```ts
// main.tsx
import { initTelemetry } from '@kfo11y/otel-web';

initTelemetry({
  serviceName: 'search-ui',
  serviceNamespace: 'search',
  serviceVersion: import.meta.env.VITE_APP_VERSION,
  team: 'search-team',
  environment: import.meta.env.VITE_DEPLOYMENT_ENVIRONMENT, // production | staging | ...
  gitSha: import.meta.env.VITE_GIT_SHA,
  propagateTo: [/^https:\/\/api\.search\.example\.com\//],
});

createRoot(document.getElementById('root')!).render(<App />);
```

**Export path.** The SDK posts OTLP/HTTP to `${otlpBaseUrl}/v1/traces` and
`${otlpBaseUrl}/v1/logs`. For now the assumption is simply that this reaches
an OpenTelemetry Collector's OTLP/HTTP receiver:

- **Locally:** run `dev/` (see the README) and set
  `otlpBaseUrl: 'http://localhost:4318'`. Its collector allows CORS from
  `localhost`.
- **Deployed:** the default `/otlp` expects the UI's ingress to route
  `/otlp/` to a collector, which avoids CORS. How browser telemetry is
  authenticated and rate-limited in shared environments is still open; it
  may need a small ingest service in front of the collector
  ([00](00-team-notes.md)).

**`propagateTo`** lists the API origins that should receive `traceparent`.
Only list your own APIs; the APIs' CORS config must allow the `traceparent`
header.

## 2. Tell it who the user is

```ts
import { setUser } from '@kfo11y/otel-web';

// Opaque user ID (never an email or employee number) and the tenant.
onSignIn((session) => setUser(session.userId, session.teamId));
onSignOut(() => setUser(undefined));
```

The tenant becomes `kfo11y.user.team.id` on every interaction event, so
front-end impact can be broken down by tenant like the APIs.

## 3. Track interactions

Wrap each user action that matters in `track`. Use the route **template**
from your router config, not `location.pathname`.

```tsx
import { useTrackedAction } from '@kfo11y/otel-web';

export function SearchBox() {
  const track = useTrackedAction('/search');

  const onSearch = (q: string) =>
    track('run_search', async () => {
      const res = await fetch(`${API}/v1/search?q=${encodeURIComponent(q)}`);
      if (!res.ok) throw new Error(`search failed: ${res.status}`);
      setResults(await res.json()); // an empty list is still success
    });

  return <SearchInput onSubmit={onSearch} />;
}
```

What you get for each call:

- an INTERNAL span `ui.run_search` whose children are the `fetch` CLIENT
  spans (which continue into the Go API's SERVER span);
- one `kfo11y.ui.interaction.summary` with `kfo11y.ui.action`, `kfo11y.ui.route`,
  `kfo11y.duration`, `kfo11y.outcome`, `kfo11y.ui.api_call.count`,
  `kfo11y.ui.api_call.error.count`, `user.id`, `kfo11y.user.team.id`, `session.id`,
  and `error.type` on failure.

Outcome rules: `success` if nothing failed; `upstream_error` if any API call
failed (network error or status ≥ 400); `ui_error` if your code threw without
an API failure; or whatever you set with `ix.setOutcome`. A search that finds nothing is
`success`.

Need extra context? `ix.set({ 'kfo11y.search.index.name': 'documents' })`. Keys
must exist in the registry.

## What to track

| Track | Don't track |
|---|---|
| Saves, submits, deletes, approvals | Every click or keystroke |
| Loading a view's data | Hover, scroll, focus |
| Searches and filters that call the API | Purely local UI state changes |

A good test: if a user would report "X didn't work," X is an interaction.

## Don'ts

- Don't put form contents, search queries, or tokens in attributes.
- Don't use IDs in `action` or `route`: `'/documents/:documentId'`, not
  `'/documents/doc-1842'`.
- Don't start a `fetch` outside `track` if you can help it: it becomes a
  CLIENT root span, which the hygiene query in [03](03-weaver-gate.md) flags.

## Notes

- **This is the 80% version.** It gets traces and one wide event per
  interaction flowing so teams can start; fidelity improves later.
- `ZoneContextManager` (zone.js) keeps the interaction span active across
  callbacks and promises, which makes API calls children of the interaction.
  zone.js can't follow **native** `async`/`await`, so with a modern build
  target, calls after the first `await` in a tracked action may become
  separate traces. The wide event is still emitted, with the right counts.
- API call counts come from a stack of active interactions. Two interactions
  in flight at once can attribute each other's calls. Fine for a start.
- A failed interaction without a thrown error gets `error.type=_OTHER`.
- Current fetch/XHR instrumentation emits the stable HTTP attribute names
  (`http.request.method`, `url.full`) by default.
- Web vitals are deliberately out of scope for v0.1: semconv v1.44 reworked
  the `browser.web_vital` event as a breaking change, and we're pinned to
  v1.43. Revisit when the registry moves.
