// Package httpapi exposes the ORCA service over HTTP.
//
// The standard library only: the dependency surface of a safety-adjacent
// service is worth keeping at zero, and net/http covers streaming, CORS and
// routing for the four endpoints we need.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"orca/internal/agents"
	"orca/internal/config"
	"orca/internal/data"

	"orca/internal/engine"
	"orca/internal/lang"
)

// Server holds the shared dependencies for all handlers.
type Server struct {
	cfg   config.Config
	orch  *agents.Orchestrator
	lim   *limiter
	sem   chan struct{} // caps concurrent upstream work
	start time.Time
}

// New builds a Server.
//
// The limits are clamped rather than trusted. A zero-value or half-populated
// Config is reachable — a test, a quick local run, a future caller — and a
// concurrency cap of zero would make every request block forever on the
// semaphore with no error and no log line. Failing open with a small default is
// the only safe reading of an unset limit.
func New(cfg config.Config, orch *agents.Orchestrator) *Server {
	cfg = cfg.WithDefaults()
	return &Server{
		cfg:   cfg,
		orch:  orch,
		lim:   newLimiter(cfg.RateLimitPerMin, time.Minute),
		sem:   make(chan struct{}, cfg.MaxConcurrent),
		start: time.Now(),
	}
}

// Handler returns the routed, CORS-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/ask", s.handleAsk)
	mux.HandleFunc("/api/ask/stream", s.handleStream)
	mux.HandleFunc("/api/eval", s.handleEval)
	return s.withCORS(s.withRecover(s.withTimeout(mux)))
}

// ---- middleware ------------------------------------------------------------

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic on %s %s: %v", r.Method, r.URL.Path, rec)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "internal error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withTimeout caps total request time. The pipeline is bounded by its own
// timeouts; this is the backstop that guarantees a client always gets a
// response rather than a hanging socket.
func (s *Server) withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- handlers --------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	snapAge := "unknown"
	if snap, err := data.LoadSnapshot(s.cfg.SnapshotPath); err == nil && !snap.GeneratedAt.IsZero() {
		snapAge = time.Since(snap.GeneratedAt).Round(time.Hour).String()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"uptime":         time.Since(s.start).Round(time.Second).String(),
		"llm":            s.cfg.LLMAvailable(),
		"llm_degraded":   !s.cfg.LLMAvailable(),
		"offline":        data.Offline(),
		"plan_model":     s.cfg.PlanModel,
		"narrate_model":  s.cfg.NarrateModel,
		"languages":      len(lang.All),
		"places":         len(data.GeoNames(s.cfg.CoastalPath)),
		"cache_entries":  s.cacheEntries(),
		"snapshot_age":   snapAge,
		"verdict_source": "deterministic Go (no model involvement)",
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	type langOpt struct {
		Code   string `json:"code"`
		Native string `json:"native"`
	}
	langs := make([]langOpt, 0, len(lang.All))
	for _, c := range lang.All {
		langs = append(langs, langOpt{string(c), lang.Name(c)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"languages": langs,
		"places":    data.GeoNames(s.cfg.CoastalPath),
		"llm":       s.cfg.LLMAvailable(),
		// Published so the UI can state the rules rather than presenting
		// unexplained numbers.
		"rules": map[string]any{
			"wave_caution_m":    engine.WaveCautionM,
			"wave_critical_m":   engine.WaveCriticalM,
			"gust_caution_kmh":  engine.GustCautionKMH,
			"gust_critical_kmh": engine.GustCriticalKMH,
			"tide_caution_m":    engine.TideCautionM,
			"pfz_band_c":        []float64{engine.PFZBandLoC, engine.PFZBandHiC},
			"anomaly_caution_c": engine.AnomalyCautionC,
		},
		"sources": []map[string]string{
			{"name": "Open-Meteo Marine", "use": "wave height, period, swell, sea-surface temperature, tide"},
			{"name": "Open-Meteo Forecast", "use": "wind, gusts, precipitation, cloud cover, weather code"},
			{"name": "Open-Meteo Archive", "use": "seasonal sea-surface temperature baseline"},
			{"name": "INCOIS", "use": "corroboration only — never a gate on the verdict"},
		},
	})
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if !s.lim.allow(r.RemoteAddr) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many requests; please wait a moment",
		})
		return
	}
	s.acquire()
	defer s.release()
	f := s.orch.Ask(r.Context(), q, nil)
	writeJSON(w, http.StatusOK, f)
}

// handleStream is the live trace endpoint used by the UI. Events are sent as
// server-sent events so the agent trace appears as it happens.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q parameter is required"})
		return
	}
	if !s.lim.allow(r.RemoteAddr) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many requests; please wait a moment",
		})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // defeat proxy buffering on Render
	w.WriteHeader(http.StatusOK)

	s.acquire()
	defer s.release()

	// Buffered so a slow client cannot block the agent goroutines.
	ch := make(chan agents.Event, 64)
	emit := func(e agents.Event) {
		select {
		case ch <- e:
		default: // drop rather than stall the pipeline
		}
	}
	go func() {
		defer close(ch)
		s.orch.Ask(r.Context(), q, emit)
	}()

	// A heartbeat keeps intermediaries from closing an idle connection while an
	// agent is still working.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				// The orchestrator emits its own "done" carrying the complete
				// findings before closing the channel, so this only terminates
				// the response. Re-sending "done" here would duplicate the whole
				// payload for the client.
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			sendEvent(w, flusher, e)
		}
	}
}

func sendEvent(w http.ResponseWriter, f http.Flusher, e agents.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
	f.Flush()
}

func (s *Server) acquire() {
	select {
	case s.sem <- struct{}{}:
	case <-time.After(3 * time.Second):
		// Never block the request path; the upstream calls have their own
		// timeouts and the snapshot fallback covers the rest.
	}
}

func (s *Server) release() {
	select {
	case <-s.sem:
	default:
	}
}

func (s *Server) cacheEntries() int { return s.orch.CacheEntries() }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// ---- rate limiting ---------------------------------------------------------

// limiter is a fixed-window per-client counter. A public demo on free upstreams
// needs one, otherwise a single enthusiastic user exhausts the provider quota
// before the judges arrive.
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(addr string) bool {
	key := addr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		key = addr[:i] // strip port
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
