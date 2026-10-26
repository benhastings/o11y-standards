package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
	"github.com/benhastings/o11y-standards/otelkit/wide"
)

// indexName is the search index this service queries. One value per index,
// so it is safe as a rollup dimension.
const indexName = "documents"

type document struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type app struct {
	mu       sync.RWMutex
	cache    map[string]document
	db       map[string]document // stands in for MariaDB/Postgres
	index    []document          // stands in for the live search index snapshot
	indexRev string
}

func newApp() *app {
	return &app{
		cache: map[string]document{},
		db: map[string]document{
			"doc-1": {ID: "doc-1", Title: "Getting started"},
			"doc-2": {ID: "doc-2", Title: "Release notes"},
		},
	}
}

// exampleSnapshot stands in for fetching a search index snapshot from its
// source.
func exampleSnapshot() ([]document, error) {
	return []document{
		{ID: "doc-1", Title: "Getting started"},
		{ID: "doc-2", Title: "Release notes"},
	}, nil
}

// activateIndex loads an index snapshot and makes it live. It is a
// point-in-time change of state, not a request, so it is recorded as one
// named event rather than a wide event. On failure the previous snapshot
// stays live.
func (a *app) activateIndex(ctx context.Context, rev string, load func() ([]document, error)) error {
	start := time.Now()
	a.mu.RLock()
	prev := a.indexRev
	a.mu.RUnlock()

	kvs := []attribute.KeyValue{
		attrs.Kfo11ySearchIndexName.String(indexName),
		attrs.Kfo11ySearchIndexRevision.String(rev),
	}
	if prev != "" {
		kvs = append(kvs, attrs.Kfo11ySearchIndexPreviousRevision.String(prev))
	}

	docs, err := load()
	if err != nil {
		wide.Log(ctx, attrs.EventSearchIndexActivation, otellog.SeverityError, append(kvs,
			attrs.Kfo11yOutcome.String(string(attrs.OutcomeUpstreamError)),
			attrs.ErrorType.String(wide.ErrorType(err)),
			attrs.Kfo11yDuration.Float64(time.Since(start).Seconds()),
		)...)
		return err
	}

	a.mu.Lock()
	a.index, a.indexRev = docs, rev
	a.mu.Unlock()
	wide.Log(ctx, attrs.EventSearchIndexActivation, otellog.SeverityInfo, append(kvs,
		attrs.Kfo11ySearchIndexDocumentCount.Int(len(docs)),
		attrs.Kfo11yOutcome.String(string(attrs.OutcomeSuccess)),
		attrs.Kfo11yDuration.Float64(time.Since(start).Seconds()),
	)...)
	return nil
}

func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /v1/documents/{id}", identify(http.HandlerFunc(a.getDocument)))
	mux.Handle("GET /v1/search", identify(http.HandlerFunc(a.search)))
	return mux
}

// identify stands in for however the service learns who is calling (a
// validated token, a gateway header). The pattern that matters: as soon as
// the caller is known, put it on the wide event.
func identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-User-Id")
		if user == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		team := map[string]string{"usr_1": "platform", "usr_2": "partner-a"}[user]
		wide.FromContext(r.Context()).Set(
			attrs.UserID.String(user),
			attrs.Kfo11yUserTeamID.String(team),
		)
		next.ServeHTTP(w, r)
	})
}

func (a *app) getDocument(w http.ResponseWriter, r *http.Request) {
	d, err := a.lookup(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		wide.FromContext(r.Context()).SetOutcome(attrs.OutcomeUpstreamError)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(d)
}

// indexError is a search failure with its own low-cardinality error.type.
type indexError string

func (e indexError) Error() string     { return string(e) }
func (e indexError) ErrorType() string { return string(e) }

const errIndexNotLoaded = indexError("index_not_loaded")

func (a *app) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ev := wide.FromContext(ctx)

	q := strings.ToLower(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	ev.Set(attrs.Kfo11ySearchIndexName.String(indexName))

	var hits []document
	// A step worth its own span in the trace; the wide event is still ev.
	err := wide.Step(ctx, "search.query", func(context.Context) error {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if a.index == nil {
			return errIndexNotLoaded
		}
		for _, d := range a.index {
			if strings.Contains(strings.ToLower(d.Title), q) {
				hits = append(hits, d)
			}
		}
		return nil
	})
	if err != nil {
		ev.SetError(err)
		ev.SetOutcome(attrs.OutcomeServerError)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	// No results is a correct answer, not an error: the outcome stays success.
	ev.Set(attrs.Kfo11ySearchResultCount.Int(len(hits)))
	_ = json.NewEncoder(w).Encode(hits)
}

var errNotFound = errors.New("not found")

func (a *app) lookup(ctx context.Context, id string) (document, error) {
	ev := wide.FromContext(ctx)

	a.mu.RLock()
	d, hit := a.cache[id]
	a.mu.RUnlock()
	ev.Set(attrs.Kfo11yCacheHit.Bool(hit))
	if hit {
		return d, nil
	}

	done := ev.Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)
	time.Sleep(2 * time.Millisecond) // simulated query
	a.mu.RLock()
	d, ok := a.db[id]
	a.mu.RUnlock()
	done()
	if !ok {
		return document{}, errNotFound
	}

	a.mu.Lock()
	a.cache[id] = d
	a.mu.Unlock()
	return d, nil
}

// refreshLoop shows wide.Run for background work.
func (a *app) refreshLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = wide.Run(ctx, "search-cache-refresh", func(ctx context.Context) error {
				a.mu.Lock()
				a.cache = map[string]document{}
				a.mu.Unlock()
				return nil
			})
		}
	}
}
