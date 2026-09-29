// Command orca serves the ORCA marine intelligence API.
//
// The service runs with zero configuration: reference data and the offline
// fallback are embedded in the binary, and the language model is optional.
// A missing or invalid OPENROUTER_API_KEY degrades the narration and the
// planning step but never the verdict.
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

	"orca/internal/agents"
	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/httpapi"
	"orca/internal/llm"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[orca] ")

	cfg := config.Load()
	data.SetOffline(cfg.Offline)
	data.SetHTTPTimeout(cfg.UpstreamTimeout)

	client := llm.New(cfg.OpenRouterKey, cfg.LLMTimeout, cfg.OpenRouterBase)
	cache := data.NewCache(cfg.CacheTTL)
	orch := agents.New(cfg, client, cache)
	srv := httpapi.New(cfg, orch)

	// Report the degraded mode at boot, so a misconfigured deploy is visible in
	// the logs immediately rather than at the first question.
	switch {
	case cfg.Offline:
		log.Printf("mode: OFFLINE — every figure comes from the baked snapshot; no outbound calls")
	case cfg.LLMAvailable():
		log.Printf("mode: full (planner=%s narrator=%s)", cfg.PlanModel, cfg.NarrateModel)
	default:
		log.Printf("mode: degraded — no OPENROUTER_API_KEY; using deterministic router and templates")
	}
	if names := data.GeoNames(cfg.CoastalPath); len(names) > 0 {
		log.Printf("coastal references: %d", len(names))
	} else {
		log.Printf("warning: coastal reference table unavailable at %q", cfg.CoastalPath)
	}
	if snap, err := data.LoadSnapshot(cfg.SnapshotPath); err != nil {
		log.Printf("warning: offline snapshot unavailable at %q: %v", cfg.SnapshotPath, err)
	} else {
		log.Printf("offline snapshot: %d places, generated %s",
			len(snap.Places), snap.GeneratedAt.Format(time.RFC3339))
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the SSE endpoint holds a response open for the life of
		// a query, so a write deadline would sever the trace mid-stream.
		IdleTimeout: 120 * time.Second,
	}

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Printf("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(idle)
	}()

	log.Printf("listening on :%s (concurrent=%d rate=%d/min cache=%s)",
		cfg.Port, cfg.MaxConcurrent, cfg.RateLimitPerMin, cfg.CacheTTL)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	<-idle
}
