package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
	"github.com/benhastings/o11y-standards/otelkit/wide"
)

type policy struct {
	ID      string            `json:"id"`
	Allowed map[string]string `json:"-"` // action -> required team
}

type app struct {
	mu    sync.RWMutex
	cache map[string]policy
	db    map[string]policy // stands in for MariaDB/Postgres
}

func newApp() *app {
	p := policy{ID: "p-1", Allowed: map[string]string{"read": "platform", "delete": "admins"}}
	return &app{
		cache: map[string]policy{},
		db:    map[string]policy{"p-1": p, "r1": p},
	}
}

func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /v1/policies/{id}", authn(http.HandlerFunc(a.getPolicy)))
	mux.Handle("POST /v1/authz/allow", authn(http.HandlerFunc(a.allow)))
	return mux
}

// authn stands in for the real identity check (validated JWT / mesh
// identity). The pattern that matters: as soon as the caller is known, put
// it on the wide event.
func authn(next http.Handler) http.Handler {
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

func (a *app) getPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := a.lookup(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		wide.FromContext(r.Context()).SetOutcome(attrs.OutcomeUpstreamError)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

func (a *app) allow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ev := wide.FromContext(ctx)

	var req struct{ Resource, Action string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, err := a.lookup(ctx, req.Resource)
	if err != nil {
		ev.Set(attrs.Kfo11yAuthzDecision.String(string(attrs.DecisionIndeterminate)))
		http.Error(w, "indeterminate", http.StatusServiceUnavailable)
		return
	}

	ev.Set(attrs.Kfo11yAuthzPolicyID.String(p.ID))
	team := map[string]string{"usr_1": "platform", "usr_2": "partner-a"}[r.Header.Get("X-User-Id")]
	if p.Allowed[req.Action] != team {
		// A deny is a correct answer, not an error.
		ev.Set(attrs.Kfo11yAuthzDecision.String(string(attrs.DecisionDeny)))
		ev.SetOutcome(attrs.OutcomeDenied)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	ev.Set(attrs.Kfo11yAuthzDecision.String(string(attrs.DecisionAllow)))
	w.WriteHeader(http.StatusOK)
}

var errNotFound = errors.New("not found")

func (a *app) lookup(ctx context.Context, id string) (policy, error) {
	ev := wide.FromContext(ctx)

	a.mu.RLock()
	p, hit := a.cache[id]
	a.mu.RUnlock()
	ev.Set(attrs.Kfo11yCacheHit.Bool(hit))
	if hit {
		return p, nil
	}

	done := ev.Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)
	time.Sleep(2 * time.Millisecond) // simulated query
	p, ok := a.db[id]
	done()
	if !ok {
		return policy{}, errNotFound
	}

	a.mu.Lock()
	a.cache[id] = p
	a.mu.Unlock()
	return p, nil
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
			_ = wide.Run(ctx, "policy-cache-refresh", func(ctx context.Context) error {
				a.mu.Lock()
				a.cache = map[string]policy{}
				a.mu.Unlock()
				return nil
			})
		}
	}
}
