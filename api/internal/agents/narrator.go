package agents

import (
	"context"
	"fmt"
	"strings"

	"orca/internal/config"
	"orca/internal/domain"
	"orca/internal/lang"
	"orca/internal/llm"
)

const narratorSystem = `You are a marine-safety assistant for Indian artisanal fishers.
You are given verified, computed facts. Write a short answer in the requested language.

Hard rules:
1. NEVER contradict the given verdict. The verdict is computed by code and is final.
2. Use ONLY the numbers in the facts. Do not add, estimate, or recall any other figure.
3. Lead with the verdict, then the fishing zone, then the main reasons.
4. Be concise: 120 words maximum. Plain text, short paragraphs, no tables, no headings markup beyond bold.
5. If the verdict is no-go, say plainly that they should not go out.
6. Never give medical, legal, or financial advice. Never predict beyond the forecast window.`

// factSheet renders the deterministic findings as the sole input the narrator
// is permitted to draw on.
//
// Everything the narrator can possibly state is present in this text. That is
// the mechanism that keeps the model grounded: it is not asked to be careful,
// it is simply not given anything else to be careless with.
func factSheet(f domain.Findings) string {
	var b strings.Builder
	fmt.Fprintf(&b, "VERDICT: %s (severity %s)\n", f.Verdict.Level, f.Verdict.Severity)
	fmt.Fprintf(&b, "LOCATION: %s, coast at %.3f, %.3f\n", f.Geo.Name, f.Geo.Lat, f.Geo.Lon)
	fmt.Fprintf(&b, "OFFSHORE WAYPOINT: %.3f, %.3f (%.0f km at bearing %03.0f degrees)\n",
		f.Geo.WayLat, f.Geo.WayLon, f.Geo.DistanceKm, f.Geo.BearingDeg)
	fmt.Fprintf(&b, "WINDOW: %d hours from now\n", f.Plan.WindowHours)
	fmt.Fprintf(&b, "SEA STATE: significant wave height %.2f m, wave period %.1f s, swell height %.2f m, swell period %.1f s, tide %.2f m\n",
		f.Marine.WaveHeightM, f.Marine.WavePeriodS, f.Marine.SwellHeightM, f.Marine.SwellPeriodS, f.Marine.TideM)
	fmt.Fprintf(&b, "SEA SURFACE TEMPERATURE: %.1f C\n", f.Marine.SSTC)
	fmt.Fprintf(&b, "WEATHER: wind %.0f km/h, gusts %.0f km/h, precipitation %.1f mm, cloud cover %.0f percent, WMO code %d, lightning risk %s\n",
		f.Weather.WindKmh, f.Weather.GustKmh, f.Weather.PrecipMm, f.Weather.CloudPct, f.Weather.WxCode, f.Weather.LightningRisk)
	fmt.Fprintf(&b, "FISHING ZONE: score %.2f, at %.4f, %.4f, %.0f km offshore, temperature band %s, anomaly %+.1f C, confidence %s\n",
		f.PFZ.Score, f.PFZ.Lat, f.PFZ.Lon, f.PFZ.DistanceKm, f.PFZ.SSTBand, f.PFZ.AnomalyC, f.PFZ.Confidence)
	if f.Advisory.Available {
		fmt.Fprintf(&b, "OFFICIAL CORROBORATION: %s (%s)\n", f.Advisory.Status, f.Advisory.Sector)
	} else {
		b.WriteString("OFFICIAL CORROBORATION: unavailable; the verdict above was computed independently\n")
	}
	b.WriteString("\nHAZARD FINDINGS:\n")
	for _, h := range f.Verdict.Hazards {
		fmt.Fprintf(&b, "- %s: %s, severity %s, threshold %s. %s\n", h.Kind, h.Value, h.Severity, h.Limit, h.Note)
	}
	b.WriteString("\nDETERMINISTIC RATIONALE:\n")
	for _, r := range f.Verdict.Rationale {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	return b.String()
}

// narrate produces the user-facing answer.
//
// The second and final model call. If anything at all goes wrong — no key, a
// timeout, a rate limit, malformed output, or an implausibly long response — the
// deterministic template is returned instead. The caller can therefore ignore
// the second return value when it only needs text.
func (o *Orchestrator) narrate(ctx context.Context, f domain.Findings, code lang.Code) (string, bool) {
	fallback := lang.Render(f, code)
	if !o.llm.Available() {
		return fallback, false
	}
	ctx, cancel := context.WithTimeout(ctx, o.cfg.LLMTimeout)
	defer cancel()

	user := fmt.Sprintf("Answer in this language: %s (%s).\n\nFACTS:\n%s", lang.Name(code), code, factSheet(f))
	txt, err := o.llm.Complete(ctx, o.cfg.NarrateModel, narratorSystem, user, false)
	if err != nil {
		return fallback, false
	}
	txt = strings.TrimSpace(txt)
	// Guard against a runaway or degenerate response. A narration much longer
	// than the fact sheet is not a useful summary of it.
	if txt == "" || len(txt) > 4000 || len(txt) < 40 {
		return fallback, false
	}
	return txt, true
}

// unknownPlace says the named place could not be located, and asks for one that
// can. It is deliberately a different message from clarify(): the user *did*
// name a place, so repeating "which location?" would read as though the app had
// ignored them. The honest thing is to say the name was not found and name the
// places that are supported, so the next question is answerable first time.
func unknownPlace(place string, code lang.Code) string {
	var head string
	switch code {
	case lang.HI:
		head = fmt.Sprintf("**%s** नाम की जगह नहीं मिल सकी।", place)
	case lang.MR:
		head = fmt.Sprintf("**%s** येथे शोधता आली नाही.", place)
	case lang.TA:
		head = fmt.Sprintf("**%s** என்ற இடம் கிடைக்கவில்லை.", place)
	case lang.TE:
		head = fmt.Sprintf("**%s** అనే స్థానం కనబడలేదు.", place)
	case lang.KN:
		head = fmt.Sprintf("**%s** ಎಂಬ ಸ್ಥಳವು ಸಿಗಲಿಲ್ಲ.", place)
	case lang.ML:
		head = fmt.Sprintf("**%s** എന്ന സ്ഥലം കണ്ടെത്തിയില്ല.", place)
	case lang.GU:
		head = fmt.Sprintf("**%s** નામનું સ્થળ મળ્યું નથી.", place)
	case lang.OR:
		head = fmt.Sprintf("**%s** ନାମର ସ୍ଥାନ ମିଳିଲା ନାହିଁ।", place)
	case lang.BN:
		head = fmt.Sprintf("**%s** নামের জায়গা পাওয়া যায়নি।", place)
	default:
		head = fmt.Sprintf("I could not find a coastal location called **%s**.", place)
	}
	return head + " " + clarify(code)
}

// clarify asks for a location when the question did not contain one. Asking is
// the correct behaviour here: guessing a coast would produce a confident,
// wrong answer about a real person's safety.
func clarify(code lang.Code) string {
	switch code {
	case lang.HI:
		return "किस तटीय स्थान के बारे में पूछ रहे हैं? जैसे **कोच्चि**, **मुंबई** या **चेन्नई** — या नाम के साथ अपना स्थान लिखें।"
	case lang.MR:
		return "तुम्ही कोणत्या किनाऱ्यावरील जागेबद्दल विचारत आहात? उदा. **कोची**, **मुंबई** किंवा **चेन्नई**."
	case lang.TA:
		return "எந்த கடற்கரைப் பகுதியைப் பற்றிக் கேட்கிறீர்கள்? எடுத்துக்காட்டாக **கொச்சி**, **மும்பை** அல்லது **சென்னை**."
	case lang.TE:
		return "మీరు ఏ ప్రాంతీర కడల గురించి అడుగుతున్నారు? ఉదాహరణకు **కోచి**, **ముంబై** లేదా **చెన్నై**."
	case lang.KN:
		return "ನೀವು ಯಾವ ಕರದ ಪ್ರದೇಶದ ಬಗ್ಗೆ ಕೇಳುತ್ತಿದ್ದೀರಿ? ಉದಾಹರಣೆಗೆ **ಕೊಚಿ**, **ಮುಂಬೈ** ಅಥವಾ **ಚೆನ್ನೈ**."
	case lang.ML:
		return "ഏത് കടലോര സ്ഥലത്തെക്കുറിച്ചാണ് ചോദിക്കുന്നത്? ഉദാഹരണത്തിന് **കൊച്ചി**, **മുംബൈ** അല്ലെങ്കിൽ **ചെന്നൈ**."
	case lang.GU:
		return "તમે કઈ દરિયાકિનારની વિશે પૂછો છો? દા.ત. **કોચિ**, **મુંબઈ** અથવા **ચેન્નઈ**."
	case lang.OR:
		return "ଆପଣ କେଉଁଥି ସମୁଦ୍ରତଟ ବିଷୟରେ ପଚାରୁଛନ୍ତି? ଉଦାହରଣ ପାଇଁ **କୋଚି**, **ମୁମ୍ବାଇ** କିମ୍ବା **ଚେନ୍ନାଇ**."
	case lang.BN:
		return "আপনি কোন উপকূলীয় এলাকা সম্পর্কে জিজ্ঞাসা করছেন? যেমন **কোচি**, **মুম্বাই** বা **চেন্নাই**।"
	default:
		return "Which coastal location are you asking about? Try **Kochi**, **Mumbai**, **Chennai** — or type your own location alongside the question."
	}
}

var _ = llm.Client{}
var _ = config.Config{}
