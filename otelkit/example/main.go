// Command example is a reference policy API showing the otelkit patterns:
// setup and shutdown, the wide HTTP middleware, enrichment from auth, cache,
// database and authorization code, and a background job.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.example.com/sre/otelkit"
	"gitlab.example.com/sre/otelkit/wide"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := otelkit.Setup(ctx, otelkit.Config{
		ServiceName:      "example-policy-api",
		ServiceNamespace: "policy",
		Team:             "policy-team",
	})
	if err != nil {
		log.Fatalf("telemetry setup: %v", err)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(sctx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	app := newApp()
	go app.refreshLoop(ctx)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           wide.Handler(app.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}
