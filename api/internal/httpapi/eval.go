package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"orca/internal/data"
	"orca/internal/eval"
)

// handleEval runs the evaluation corpus and returns the report as JSON.
//
// It is exposed rather than kept in a test file because the number it produces
// is the answer to the two questions a reviewer is most likely to ask: is the
// narrative actually grounded in the observations, and is the answer really in
// the language the user asked in. A claim like that is worth more when it can be
// reproduced on demand than when it is asserted in a slide.
//
// Two deliberate restrictions. The corpus runs in offline mode by default, so
// the endpoint is deterministic and works during a demo with no network, and
// the run is given its own generous deadline because a full corpus is
// hundreds of pipeline executions rather than one query.
func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, `{"answer":"use GET or POST"}`, http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	live := q.Get("live") == "1" || q.Get("live") == "true"
	repeat := 1
	if v := q.Get("repeat"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 3 {
			repeat = n
		}
	}
	only := q.Get("group")

	cases := eval.Corpus(s.snapshotPlaces())
	if only != "" {
		var kept []eval.Case
		for _, c := range cases {
			if c.ID == only || c.Group == only {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	if len(cases) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"answer": "no cases matched that filter"})
		return
	}

	// A full corpus is slow by nature; the shared request deadline would cut it
	// off mid-run and report a failure that is really a timeout.
	timeout := time.Duration(len(cases)) * 900 * time.Millisecond
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	if timeout > 15*time.Minute {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	rep := eval.Run(ctx, s.orch, cases, eval.Config{Live: live, Repeat: repeat})

	// A failing corpus is still a successful evaluation, so the report is
	// returned as 200. A non-2xx here would read as "the endpoint is broken"
	// rather than "the system under test regressed", which is the opposite of
	// what a reviewer needs to be told.
	writeJSON(w, http.StatusOK, rep)
}

// snapshotPlaces lists the places the offline snapshot can answer for, so the
// evaluation corpus and the snapshot cannot drift apart silently.
func (s *Server) snapshotPlaces() []string {
	places, err := data.SnapshotPlaces(s.cfg.SnapshotPath)
	if err != nil {
		return nil
	}
	return places
}
