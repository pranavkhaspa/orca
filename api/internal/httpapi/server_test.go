package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orca/internal/agents"
	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/llm"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	data.SetOffline(true)
	t.Cleanup(func() { data.SetOffline(false) })

	cfg := config.Config{
		CoastalPath:    "embed:coastal_towns.json",
		SnapshotPath:   "embed:snapshot.json",
		WaypointKm:     45,
		CacheTTL:       time.Minute,
		AllowedOrigins: "*",
	}
	srv := New(cfg, agents.New(cfg, llm.New("", 0, ""), data.NewCache(time.Minute)))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var h map[string]any
	json.NewDecoder(resp.Body).Decode(&h)
	if h["status"] != "ok" {
		t.Errorf("status = %v", h["status"])
	}
	// The health payload must state where the verdict comes from, so an operator
	// can confirm the safety-critical property at a glance.
	if s, _ := h["verdict_source"].(string); !strings.Contains(s, "deterministic") {
		t.Errorf("verdict_source = %q, want a statement that it is deterministic", h["verdict_source"])
	}
	if h["llm"] != false {
		t.Error("health should report llm=false when no key is configured")
	}
	if n, _ := h["places"].(float64); n < 20 {
		t.Errorf("places = %v, want the coastal table", h["places"])
	}
}

func TestMetaPublishesRulesAndLanguages(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var m struct {
		Languages []struct{ Code, Native string } `json:"languages"`
		Places    []string                        `json:"places"`
		LLM       bool                            `json:"llm"`
		Rules     map[string]any                  `json:"rules"`
		Sources   []struct{ Name, Use string }    `json:"sources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if len(m.Languages) != 10 {
		t.Errorf("languages = %d, want 10", len(m.Languages))
	}
	for _, l := range m.Languages {
		if l.Native == "" {
			t.Errorf("language %s has no endonym for the selector", l.Code)
		}
	}
	if len(m.Places) < 20 {
		t.Errorf("places = %d, want the full coastal list", len(m.Places))
	}
	// The UI needs the thresholds to explain the verdict, and needs to know
	// INCOIS is not a gate.
	for _, k := range []string{"wave_caution_m", "wave_critical_m", "gust_critical_kmh", "pfz_band_c"} {
		if _, ok := m.Rules[k]; !ok {
			t.Errorf("rules are missing %q", k)
		}
	}
	foundINCOIS := false
	for _, s := range m.Sources {
		if s.Name == "INCOIS" {
			foundINCOIS = true
			if !strings.Contains(s.Use, "never a gate") {
				t.Errorf("INCOIS source note should state it is not a gate, got %q", s.Use)
			}
		}
	}
	if !foundINCOIS {
		t.Error("INCOIS should be listed as a source, even though it is only corroboration")
	}
}

func TestAskRequiresPostAndQuery(t *testing.T) {
	ts := newTestServer(t)

	// GET is not allowed.
	if resp, _ := http.Get(ts.URL + "/api/ask"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/ask status = %d, want 405", resp.StatusCode)
	}
	// Empty query.
	resp, err := http.Post(ts.URL+"/api/ask", "application/json", strings.NewReader(`{"query":"  "}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty query status = %d, want 400", resp.StatusCode)
	}
	// Malformed body.
	resp2, _ := http.Post(ts.URL+"/api/ask", "application/json", strings.NewReader(`not json`))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", resp2.StatusCode)
	}
}

func TestAskReturnsCompleteFindings(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Post(ts.URL+"/api/ask", "application/json",
		strings.NewReader(`{"query":"is it safe to go fishing off Kochi tomorrow"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var f domain.Findings
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if f.Verdict.Level == "" || f.Verdict.Level == "  " {
		t.Error("no verdict in response")
	}
	if len(f.Verdict.Hazards) == 0 {
		t.Error("no hazard breakdown in response")
	}
	if len(f.Trace) != 7 {
		t.Errorf("trace has %d entries, want 7", len(f.Trace))
	}
	if f.Answer == "" {
		t.Error("no answer in response")
	}
	if f.AnswerLang != "en" {
		t.Errorf("answer_lang = %q, want en", f.AnswerLang)
	}
	if len(f.Prov) == 0 {
		t.Error("no provenance in response")
	}
	// Every source URL must be a real https endpoint the user can open.
	for _, c := range f.Prov {
		if !strings.HasPrefix(c.URL, "https://") {
			t.Errorf("provenance URL is not https: %q", c.URL)
		}
	}
}

// TestAskWorksInALanguageTheQuestionWasAskedIn confirms the response language
// follows the question, end to end through the HTTP layer.
func TestAskRespondsInQuestionLanguage(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Post(ts.URL+"/api/ask", "application/json",
		strings.NewReader(`{"query":"கொச்சி நாளை மீன் பிடிக்க போகலாமா"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var f domain.Findings
	json.NewDecoder(resp.Body).Decode(&f)
	if f.AnswerLang != "ta" {
		t.Errorf("answer_lang = %q, want ta", f.AnswerLang)
	}
	if f.Plan.Place != "Kochi" {
		t.Errorf("place = %q, want the English reference name Kochi", f.Plan.Place)
	}
	if !strings.Contains(f.Answer, "கொச்சி") {
		t.Errorf("answer does not contain the Tamil place name: %q", f.Answer)
	}
}

func TestCORS(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/ask", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("preflight did not return an allowed origin")
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Error("preflight does not allow POST")
	}
}

// TestStreamEmitsTheFullTrace is the contract the live UI depends on: agent
// events as they happen, then the answer, then a terminating event.
func TestStreamEmitsTheFullTrace(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/ask/stream?q=" + "is+it+safe+off+Kochi")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("missing X-Accel-Buffering: no; proxies will buffer the trace")
	}

	var agentNames []string
	sawAnswer, sawDone, sawEnd := false, false, false
	var answerText string

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			raw := strings.TrimPrefix(line, "data: ")
			switch event {
			case "agent":
				var e agents.Event
				if err := json.Unmarshal([]byte(raw), &e); err != nil {
					t.Fatalf("agent event is not valid JSON: %v (%s)", err, raw)
				}
				if e.Run == nil {
					t.Fatal("agent event has no run")
				}
				agentNames = append(agentNames, e.Run.Name)
			case "answer":
				var e agents.Event
				json.Unmarshal([]byte(raw), &e)
				answerText = e.Answer
				sawAnswer = true
			case "done":
				var e agents.Event
				if err := json.Unmarshal([]byte(raw), &e); err != nil {
					t.Fatalf("done event is not valid JSON: %v", err)
				}
				if e.Final == nil {
					t.Fatal("done event carried no final findings")
				}
				if e.Final.Verdict.Level == "" {
					t.Error("final findings carry no verdict")
				}
				sawDone = true
			case "end":
				sawEnd = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading stream: %v", err)
	}

	want := []string{"planner", "geo", "ocean", "weather", "incois", "domain", "narrator"}
	if len(agentNames) != len(want) {
		t.Fatalf("streamed %d agent events %v, want %d", len(agentNames), agentNames, len(want))
	}
	for i := range want {
		if agentNames[i] != want[i] {
			t.Errorf("agent event %d = %q, want %q", i, agentNames[i], want[i])
		}
	}
	if !sawAnswer || answerText == "" {
		t.Error("stream did not deliver an answer")
	}
	if !sawDone {
		t.Error("stream did not deliver the final findings")
	}
	if !sawEnd {
		t.Error("stream did not terminate with an end event")
	}
}

func TestStreamRequiresQuery(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/ask/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestRateLimitProtectsFreeUpstreams confirms a burst from one client is
// throttled rather than passed through to the free providers.
func TestRateLimitProtectsFreeUpstreams(t *testing.T) {
	lim := newLimiter(3, time.Minute)
	addr := "10.0.0.1:5555"
	for i := 0; i < 3; i++ {
		if !lim.allow(addr) {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}
	if lim.allow(addr) {
		t.Error("the fourth request should have been limited")
	}
	// A different client is unaffected, so one user cannot lock out everyone.
	if !lim.allow("10.0.0.2:5555") {
		t.Error("a second client was throttled by the first")
	}
	// The window expires.
	l2 := newLimiter(1, 20*time.Millisecond)
	a := "10.0.0.3:1"
	if !l2.allow(a) || l2.allow(a) {
		t.Error("limiter did not enforce its limit")
	}
	time.Sleep(40 * time.Millisecond)
	if !l2.allow(a) {
		t.Error("limiter did not release after its window elapsed")
	}
}

func TestRequestSizeIsBounded(t *testing.T) {
	ts := newTestServer(t)
	huge := strings.Repeat("a", 1<<20)
	resp, err := http.Post(ts.URL+"/api/ask", "application/json",
		strings.NewReader(`{"query":"`+huge+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// A megabyte of query text is not a real question, and forwarding it to a
	// paid model would be an easy way to burn a quota.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an oversized body", resp.StatusCode)
	}
}

// TestStreamSendsExactlyOneDone guards against the duplication that a naive
// drain-on-close produces: the payload is large and the client would render the
// answer twice.
func TestStreamSendsExactlyOneDone(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/ask/stream?q=is+it+safe+off+Puri")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	doneCount, answerCount, agentCount, endCount := 0, 0, 0, 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			switch event {
			case "done":
				doneCount++
			case "answer":
				answerCount++
			case "agent":
				agentCount++
			case "end":
				endCount++
			}
		}
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want exactly 1", doneCount)
	}
	if answerCount != 1 {
		t.Errorf("answer events = %d, want exactly 1", answerCount)
	}
	if agentCount != 7 {
		t.Errorf("agent events = %d, want 7", agentCount)
	}
	if endCount != 1 {
		t.Errorf("end events = %d, want exactly 1", endCount)
	}
}
