package agents

import (
	"context"
	"regexp"
	"strings"

	"orca/internal/config"
	"orca/internal/data"
	"orca/internal/domain"
	"orca/internal/lang"
	"orca/internal/llm"
)

// PlannerAgent turns a free-text question into a structured plan.
//
// The model is used when available, but a deterministic router is always run
// first and the model's result is only accepted if it is coherent. This
// inversion is deliberate: a plan is a control input, and a control input
// derived from a hallucinated place name would send every downstream agent
// somewhere wrong. The router also recognises the coastal reference table by
// name, which makes the common case both more reliable and offline-capable.
type PlannerAgent struct {
	cfg   config.Config
	llm   *llm.Client
	run   *runState
	coast string
}

const plannerSystem = `You convert a fisherman's question into a JSON plan.
Respond with ONLY a JSON object:
{"place":"<place name>","window_hours":<int 1..72>,"intent":"<safety|pfz|conditions|tide|general>","lang":"<language code>"}

Rules:
- place: the coastal location mentioned, in English if possible. Use the plain place name only.
- window_hours: how far ahead they want to know. Default 6. "now"/"today"=6, "tomorrow"=30, "next 3 days"=60.
- intent: safety=is it safe to go; pfz=where to fish; conditions=what is the sea like; tide=tide only; general=anything else.
- lang: the language code of the question: en, hi, ta, te, kn, ml, gu, or, bn, mr.
If no place is mentioned, use "".`

// Plan produces the query plan, preferring the deterministic router.
func (p *PlannerAgent) Plan(ctx context.Context, query string, detected lang.Code) domain.Plan {
	det := planFromRouter(query, detected, p.coast)
	if p.run != nil {
		p.run.llmNote = "router"
	}
	if !p.llm.Available() {
		return det
	}

	var out domain.Plan
	ctx, cancel := context.WithTimeout(ctx, p.cfg.LLMTimeout)
	defer cancel()
	system := plannerSystem
	if err := p.llm.CompleteJSON(ctx, p.cfg.PlanModel, system, query, &out); err != nil {
		return det
	}
	// Only accept the model plan when it names a place we can actually resolve.
	if strings.TrimSpace(out.Place) == "" {
		return det
	}
	if _, ok := data.MatchTown(out.Place, p.coast); !ok {
		// Trust the router's place if it found one; the model lost.
		if det.Place != "" {
			return det
		}
	}
	if out.WindowHours < 1 || out.WindowHours > 72 {
		out.WindowHours = det.WindowHours
	}
	if !lang.Valid(lang.Code(out.Lang)) {
		out.Lang = string(detected)
	}
	// Script-based detection is authoritative: it cannot be wrong about Tamil
	// text, whereas a model can mislabel short queries.
	out.Lang = string(detected)
	out.Planned = true
	out.Place = strings.TrimSpace(out.Place)
	if out.Place == "" {
		out.Place = det.Place
	}
	if p.run != nil {
		p.run.llmNote = "model"
	}
	return out
}

var (
	reHours = regexp.MustCompile(`(?i)(\d{1,2})\s*(hours?|hrs?)`)
	reDays  = regexp.MustCompile(`(?i)(\d{1,2})\s*(days?)`)
)

// The time and intent vocabulary is data rather than one regex per language, so
// that a language cannot be added to the interface without a line in the table
// beside it. The router decides *what to look at*, and if it reads a
// non-English question as a different question, the hazard analysis changes:
// the same Puri question came back as `intent=general, window=6h` in Odia
// against `intent=safety, window=30h` in English, and a 6-hour window did not
// reach the weather that made the call a caution. The answer was still written
// in the right language, so nothing looked wrong.
// The time and intent vocabulary is data rather than one regex per language, so
// a language cannot be added to the interface without a line in the table
// beside it.
//
// This table decides *what to look at*, which is why its coverage is a safety
// property rather than a typing convenience. The router picks the intent and
// the forecast window, so a question it misreads is a different question: the
// same Kochi question came back as `intent=general, window=6h` in Odia against
// `intent=safety, window=30h` in English, and a 6-hour window stopped short of
// the weather that made the call a caution. The answer still came back in the
// right language, so nothing looked wrong.
//
// Every term below is a stem taken from a sentence that is already in the
// repository: the evaluation corpus in `internal/eval` and the interface
// examples in `web/src/i18n.ts`. Transliterating a word by hand is how the
// table got Gujarati `સુરક્ષિત` and `સુરક્ષિત` in the same entry, one of which
// never matched anything. `TestTheTimeAndIntentTableCoversEveryLanguage` holds
// the source sentences to that standard.
var (
	timeTomorrow = []string{
		"tomorrow", "कल", "उद्या", "আগামীকাল", "કાલે", "ನಾಳೆ", "നാളെ",
		"நாளை", "రేపు", "ଆସନ୍ତାଦିନ",
	}
	timeToday = []string{
		"now", "today", "right now", "immediate", "present", "आज", "আজ",
		"આજે", "ಇಂದು", "ഇന്ന്", "இன்று", "ఈరోజు", "ଆଜି",
	}
	timeMorning = []string{
		"morning", "dawn", "sunrise", "सुबह", "सकाळी", "সকালে", "સવારે",
		"ಬೆಳಗ್ಗೆ", "രാവിലെ", "காலை", "ఉదయం", "ପୂର୍ବାହ୍ନ",
	}
	// Evening has no verified sentence to draw on, unlike the three above. The
	// risk of a missing term is a question that falls back to the 6-hour
	// default; the risk of a wrong one is a window that is not the one asked
	// for, so these are the conservative, common forms and a new sentence
	// should be added to the corpus to earn them a place.
	timeEvening = []string{
		"evening", "sunset", "night", "शाम", "संध्याकाळी", "সন্ধ্যায়", "સાંજે",
		"ಸಂಜೆ", "വൈകുന്നേരം", "மாலை", "సాయంత్రం", "ସନ୍ଧ୍ୟା",
	}
)

// intentWords maps an intent to the words that mean it, in the order the
// planner tries them. A question that is both about safety and about fishing is
// a safety question, which is the safer of the two readings, so `safety` comes
// first rather than alphabetically.
var intentWords = []struct {
	intent string
	words  []string
}{
	{"safety", []string{
		"safe", "safety", "risk", "danger", "ok to go", "venture",
		"worth it", "safe to", "go out", "leave port", "set sail",
		"can i go", "should i go",
		"सुरक्षित",   // hi
		"सुरक्षित",   // mr
		"নিরাপদ",     // bn
		"સુરક્ષિત",   // gu
		"ಸುರಕ್ಷಿತ",   // kn
		"ಹಿಡಿಯಬಹುದೇ", // kn: "may I fish", the phrasing the corpus uses
		"പോകാമോ",     // ml
		"போகலாமா",    // ta
		"సురక్షిత",   // te
		"ସୁରକ୍ଷିତ",   // or
		"ଉଚିତ",       // or: "should I", the phrasing the corpus uses
	}},
	{"pfz", []string{
		"fish", "fishing", "fishing zone", "pfz", "catch", "where to",
		"मछली", // hi
		"मास",  // mr: मासे, मासेमारी
		"মাছ",  // bn
		"માછ",  // gu: માછલી
		"ಮೀನು", // kn
		"മീൻ",  // ml
		"மீன்", // ta
		"చేపల", // te
		"ମାଛ",  // or
	}},
	{"tide", []string{
		"tide", "tides", "ज्वार", "ज्वार", "জোয়ার",
		"જ્વાર", "அலை", "ఎదురు", "ଜୋର",
	}},
	{"conditions", []string{
		"condition", "conditions", "weather", "sea state", "wave", "waves",
		"status", "how is the sea",
		"मौसम", "লহর", "आবহাওয়া", "ঢেউ", "હવામાન", "લહેર",
		"ಹವಾಮಾನ", "കാലാസ്ഥിതി", "வானிலை", "వాతావరణం",
		"ପାଣିପାଗ",
	}},
}

// planFromRouter is the offline planner. It must always return something usable.
func planFromRouter(query string, detected lang.Code, coastPath string) domain.Plan {
	q := strings.ToLower(query)

	// Place resolution, widest first. A whole-query scan catches native-script
	// spellings that token-based latin matching cannot see.
	place := ""
	if t, ok := data.FindTownIn(query, coastPath); ok {
		place = t.Name
	} else {
		for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(tok) < 4 {
				continue
			}
			if t, ok := data.MatchTown(tok, coastPath); ok {
				place = t.Name
				break
			}
		}
	}

	// Intent is read from question form, not keywords alone. The table is
	// ordered: the first intent with a matching word wins, so a question that
	// is both about safety and about fishing is a safety question, which is
	// the safer of the two readings.
	intent := "general"
	for _, g := range intentWords {
		if containsAny(q, g.words...) {
			intent = g.intent
			break
		}
	}

	window := 6
	switch {
	case reDays.MatchString(q):
		if m := reDays.FindStringSubmatch(q); len(m) > 1 {
			if n := atoiSafe(m[1]); n > 0 {
				window = n * 24
			}
		}
	case reHours.MatchString(q):
		if m := reHours.FindStringSubmatch(q); len(m) > 1 {
			if n := atoiSafe(m[1]); n > 0 {
				window = n
			}
		}
	case containsAny(q, timeTomorrow...):
		// Tomorrow is 30h rather than 24 so the window reaches past midnight
		// into the following morning, which is the part of "tomorrow" a
		// fisherman actually cares about.
		window = 30
	case containsAny(q, timeMorning...), containsAny(q, timeEvening...):
		window = 12
	case containsAny(q, timeToday...):
		window = 6
	}
	if window < 1 {
		window = 1
	}
	if window > 72 {
		window = 72
	}

	return domain.Plan{Place: place, WindowHours: window, Intent: intent, Lang: string(detected)}
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
