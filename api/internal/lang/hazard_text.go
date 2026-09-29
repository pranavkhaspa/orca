package lang

import (
	"fmt"

	"orca/internal/domain"
)

// Localised description of the deterministic verdict.
//
// The engine deliberately emits structured findings (kind, value, limit,
// severity) and leaves the English phrasing alongside them. That structure is
// the contract this package translates, so a user in Tamil receives a fully
// Tamil explanation of the same rule the Go code applied — not a translated
// sentence wrapped around English reasoning.
//
// Where a phrasing would require knowledge the system does not have (for
// instance the exact numeric rule), the value and threshold are still shown
// verbatim. Numbers and units are never translated, because a mistranslated
// threshold in a safety message is worse than an untranslated one.

type phrases struct {
	caution     string // verb phrase for a caution-level hazard
	critical    string
	ok          string
	verdictGo   string
	verdictCau  string
	verdictNoGo string
	highConf    string
	medConf     string
	lowConf     string
	zoneTitle   string
	rationaleHd string
	waveNoun    string
	windNoun    string
	convNoun    string
	tideNoun    string
	zoneNoun    string
}

var ph = map[Code]phrases{
	EN: {
		caution: "caution", critical: "critical", ok: "within limits",
		verdictGo: "GO", verdictCau: "CAUTION", verdictNoGo: "NO-GO",
		highConf: "high", medConf: "medium", lowConf: "low",
		zoneTitle: "Fishing zone", rationaleHd: "Why",
		waveNoun: "waves", windNoun: "wind", convNoun: "convection", tideNoun: "tide", zoneNoun: "zone",
	},
	HI: {
		caution: "सावधानी", critical: "गंभीर", ok: "सुरक्षित सीमा में",
		verdictGo: "जाना सुरक्षित", verdictCau: "सावधानी रखें", verdictNoGo: "समुद्र में न जाएं",
		highConf: "उच्च", medConf: "मध्यम", lowConf: "निम्न",
		zoneTitle: "मछली पकड़ने का क्षेत्र", rationaleHd: "कारण",
		waveNoun: "लहरें", windNoun: "हवा", convNoun: "गरज-चमक", tideNoun: "ज्वार", zoneNoun: "क्षेत्र",
	},
	TA: {
		caution: "எச்சரிக்கை", critical: "ஆபத்தானது", ok: "பாதுகாப்பானது",
		verdictGo: "செல்லலாம்", verdictCau: "எச்சரிக்கையுடன் செல்லவும்", verdictNoGo: "கடலில் செல்ல வேண்டாம்",
		highConf: "அதிக", medConf: "நடுத்தர", lowConf: "குறைந்த",
		zoneTitle: "மீன் பிடிக்கும் மண்டலம்", rationaleHd: "காரணம்",
		waveNoun: "அலைகள்", windNoun: "காற்று", convNoun: "மின்னல் மழை", tideNoun: "அலை", zoneNoun: "மண்டலம்",
	},
	TE: {
		caution: "జాగ్రత్త", critical: "ప్రమాదకరం", ok: "సురక్షిత పరిధిలో",
		verdictGo: "వెళ్లవచ్చు", verdictCau: "జాగ్రత్తగా వెళ్లండి", verdictNoGo: "సముద్రంలోకి వెళ్లకండి",
		highConf: "అధిక", medConf: "మధ్యస్థం", lowConf: "తక్కువ",
		zoneTitle: "చేపల వేటే మండలం", rationaleHd: "కారణం",
		waveNoun: " waves", windNoun: "గాలి", convNoun: "ఉరుములు", tideNoun: "ఎబ్బం", zoneNoun: "మండలం",
	},
	KN: {
		caution: "ಎಚ್ಚರಿಕೆ", critical: "ಅಪಾಯಕಾರಿ", ok: "ಸುರಕ್ಷಿತ ಮಿತಿಯಲ್ಲಿ",
		verdictGo: "ಹೋಗಬಹುದು", verdictCau: "ಎಚ್ಚರಿಕೆಯಿಂದ ಹೋಗಿ", verdictNoGo: "ಸಮುದ್ರಕ್ಕೆ ಹೋಗಬೇಡಿ",
		highConf: "ಹೆಚ್ಚು", medConf: "ಮಧ್ಯಮ", lowConf: "ಕಡಿಮೆ",
		zoneTitle: "ಮೀನು ಹಿಡಿಯುವ ವಲಯ", rationaleHd: "ಕಾರಣ",
		waveNoun: "ಅಲೆಗಳು", windNoun: "ಗಾಳಿ", convNoun: "ಮೇಘಾವರ್ಷೆ", tideNoun: "ಅಲೆ", zoneNoun: "ವಲಯ",
	},
	ML: {
		caution: "ജാഗ്രത്ത്", critical: "അപകടം", ok: "സുരക്ഷിത പരിധിയിൽ",
		verdictGo: "പോകാം", verdictCau: "ജാഗ്രത്തിലായി പോകുക", verdictNoGo: "കടലിലേക്ക് പോകരുത്",
		highConf: "ഉയർന്ന", medConf: "ഇടത്തരം", lowConf: "കുറവ്",
		zoneTitle: "മീൻ പിടിക്കുന്ന മേഖല", rationaleHd: "കാരണം",
		waveNoun: "തിരമാളങ്ങൾ", windNoun: "കാറ്റ്", convNoun: "ഇടിമാസ്", tideNoun: "വരിക", zoneNoun: "മേഖല",
	},
	GU: {
		caution: "સાવધ", critical: "જોખમકર", ok: "સલામત મર્યાદામાં",
		verdictGo: "જવું સલામત છે", verdictCau: "સાવધ રહો", verdictNoGo: "દરિયામાં ન જાઓ",
		highConf: "ઊંચું", medConf: "મધ્યમ", lowConf: "ઓછું",
		zoneTitle: "માછલી પકડવાનું વિસ્તાર", rationaleHd: "કારણ",
		waveNoun: "લહેરાં", windNoun: "પવન", convNoun: "વિદ્યુત", tideNoun: "ભંગ", zoneNoun: "વિસ્તાર",
	},
	OR: {
		caution: "ସତର୍କ", critical: "ବିପଜନକ", ok: "ସୁରକ୍ଷିତ ସୀମାରେ",
		verdictGo: "ଯାଓବା ସୁରକ୍ଷିତ", verdictCau: "ସତର୍କତା ରଖନ୍ତୁ", verdictNoGo: "ସମୁଦ୍ରକୁ ଯାଅନ୍ତୁ ନାହିଁ",
		highConf: "ଉଚ୍ଚ", medConf: "ମଧ୍ୟମ", lowConf: "କମ୍",
		zoneTitle: "ମାଛ ଧରିବା ଅଞ୍ଚଳ", rationaleHd: "କାରଣ",
		waveNoun: "ଢଉ", windNoun: "ପବନ", convNoun: "ବିଦ୍ୟୁତ", tideNoun: "ଜ୍ୱାର", zoneNoun: "ଅଞ୍ଚଳ",
	},
	BN: {
		caution: "সতর্ক", critical: "বিপজ্জনক", ok: "নিরাপদ সীমার মধ্যে",
		verdictGo: "যাওয়া নিরাপদ", verdictCau: "সতর্ক থাকুন", verdictNoGo: "সমুদ্রে যাবেন না",
		highConf: "উচ্চ", medConf: "মাঝারি", lowConf: "নিম্ন",
		zoneTitle: "মাছ ধরার এলাকা", rationaleHd: "কারণ",
		waveNoun: "ঢেউ", windNoun: "বাতাস", convNoun: "বজ্রপাত", tideNoun: "জোয়ার", zoneNoun: "এলাকা",
	},
	MR: {
		caution: "सावध", critical: "धोकादायक", ok: "सुरक्षित मर्यादेत",
		verdictGo: "जाणे सुरक्षित आहे", verdictCau: "सावध रहा", verdictNoGo: "समुद्रात जाऊ नका",
		highConf: "उच्च", medConf: "मध्यम", lowConf: "कमी",
		zoneTitle: "मासे पकडण्याचा विभाग", rationaleHd: "कारण",
		waveNoun: "लाटा", windNoun: "वारा", convNoun: "विजा", tideNoun: "भरती", zoneNoun: "विभाग",
	},
}

func phr(c Code) phrases {
	if p, ok := ph[c]; ok {
		return p
	}
	return ph[EN]
}

// VerdictWord returns the localised headline verdict.
func VerdictWord(c Code, level string) string {
	p := phr(c)
	switch level {
	case "no-go":
		return p.verdictNoGo
	case "caution":
		return p.verdictCau
	case "go":
		return p.verdictGo
	default:
		return level
	}
}

// ConfidenceWord localises the zone confidence grade.
func ConfidenceWord(c Code, conf string) string {
	p := phr(c)
	switch conf {
	case "high":
		return p.highConf
	case "medium":
		return p.medConf
	case "low":
		return p.lowConf
	default:
		return conf
	}
}

func severityWord(c Code, sev string) string {
	p := phr(c)
	switch sev {
	case "critical":
		return p.critical
	case "caution":
		return p.caution
	default:
		return p.ok
	}
}

func kindNoun(c Code, kind string) string {
	p := phr(c)
	switch kind {
	case "waves":
		return p.waveNoun
	case "wind":
		return p.windNoun
	case "convection":
		return p.convNoun
	case "tide":
		return p.tideNoun
	default:
		return p.zoneNoun
	}
}

// Reason returns the localisable core of one hazard finding: what was measured,
// the grade, and the threshold it was judged against.
//
// The value and limit are passed through verbatim from the engine. Translating a
// figure is how a safety message becomes wrong, so the numbers are never
// reworded — only the frame around them is.
func Reason(f domain.Findings, h domain.Hazard, c Code) string {
	noun := kindNoun(c, h.Kind)
	sev := severityWord(c, h.Severity)
	switch h.Severity {
	case "critical":
		return fmt.Sprintf("%s %s — %s (%s)", sev, noun, h.Value, h.Limit)
	case "caution":
		return fmt.Sprintf("%s %s — %s (%s)", sev, noun, h.Value, h.Limit)
	default:
		return fmt.Sprintf("%s — %s %s (%s)", sev, h.Value, noun, h.Limit)
	}
}

// Rationale rebuilds the reasoning from the structured hazard list in the
// requested language, falling back to the engine's English prose only for
// English, where that prose is already correct and more detailed.
func Rationale(f domain.Findings, c Code) []string {
	if c == EN {
		if len(f.Verdict.Rationale) > 0 {
			return f.Verdict.Rationale
		}
		return []string{noHazard(EN)}
	}
	var out []string
	for _, h := range f.Verdict.Hazards {
		switch h.Severity {
		case "critical", "caution":
			out = append(out, Reason(f, h, c))
		}
	}
	if len(out) == 0 {
		return []string{noHazard(c)}
	}
	return out
}

// ZoneTitle is the localised heading for the fishing-zone section.
func ZoneTitle(c Code) string { return phr(c).zoneTitle }

// RationaleHeading is the localised heading above the reasoning list.
func RationaleHeading(c Code) string { return phr(c).rationaleHd }

func noHazard(c Code) string {
	switch c {
	case HI, MR:
		return "अनुरोध की अवधि में कोई बढ़ा हुआ जोखिम नहीं मिला।"
	case TA:
		return "கேட்ட நேரத்தில் அதிகரித்த ஆபத்து எதுவும் கிடைக்கவில்லை."
	case TE:
		return "అడిగిన సమయంలో ఎక్కువ ప్రమాదం కనిపించలేదు."
	case KN:
		return "ಕೋರಿದ ಸಮಯದಲ್ಲಿ ಹೆಚ್ಚಿನ ಅಪಾಯ ಕಂಡುಬಂದಿಲ್ಲ."
	case ML:
		return "ചോദിച്ച സമയത്ത് കൂടുതൽ അപകടം കണ്ടെത്തിയില്ല."
	case GU:
		return "વિનંતી કાળમાં વધુ જોખમ મળ્યું નથી."
	case OR:
		return "ଅନୁରୋଧିତ ସମୟରେ ବର୍ଦ୍ଧିତ ବିପଦ ମିଳିଲା ନାହିଁ।"
	case BN:
		return "চাওয়া সময়ে কোনো বর্ধিত বিপদ পাওয়া যায়নি।"
	default:
		return "No elevated hazard was detected in the requested window."
	}
}
