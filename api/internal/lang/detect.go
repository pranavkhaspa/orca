// Package lang implements script-based language detection and the per-language
// fallback templates.
//
// Detection is deterministic Unicode script-range analysis rather than a model
// call. That is a deliberate choice: language detection sits at the very front
// of the pipeline and gates the response format, so it must never be the step
// that fails or is rate-limited.
package lang

import "strings"

// Code is a supported language code.
type Code string

const (
	EN Code = "en"
	HI Code = "hi" // Hindi
	TE Code = "te" // Telugu
	TA Code = "ta" // Tamil
	KN Code = "kn" // Kannada
	OR Code = "or" // Odia
	BN Code = "bn" // Bengali
	ML Code = "ml" // Malayalam
	GU Code = "gu" // Gujarati
	MR Code = "mr" // Marathi
)

// Native is the endonym, used in the UI selector so a user who cannot read
// English script can still find their language.
var Native = map[Code]string{
	EN: "English", HI: "हिन्दी", TA: "தமிழ்", TE: "తెలుగు", KN: "ಕನ್ನಡ",
	ML: "മലയാളം", GU: "ગુજરાતી", OR: "ଓଡ଼ିଆ", BN: "বাংলা", MR: "मराठी",
}

// All lists supported languages in a stable order.
var All = []Code{EN, HI, TA, TE, KN, ML, GU, OR, BN, MR}

type rrange struct{ lo, hi rune }

var scriptRanges = []struct {
	code   Code
	ranges []rrange
}{
	{HI, []rrange{{0x0900, 0x097F}}},
	{BN, []rrange{{0x0980, 0x09FF}}},
	{GU, []rrange{{0x0A80, 0x0AFF}}},
	{OR, []rrange{{0x0B00, 0x0B7F}}},
	{TA, []rrange{{0x0B80, 0x0BFF}}},
	{TE, []rrange{{0x0C00, 0x0C7F}}},
	{KN, []rrange{{0x0C80, 0x0CFF}}},
	{ML, []rrange{{0x0D00, 0x0D7F}}},
}

// Devanagari is shared with Hindi, so the two must be told apart lexically.
// Weight 2 means the token is diagnostic of that language; weight 1 means it is
// common to both and only tips the balance in company.
//
// The earlier marker list was Marathi-only and first-match-wins, and that was
// wrong in a way that only shows up on real sentences: it included समुद्र,
// नाही, किंवा, उद्या and साठी, which are spelled identically in Hindi. So
// "क्या पुरी के पास समुद्र में मछली पकड़ना सुरक्षित है?" was answered in Marathi,
// because the word for "sea" appeared in it. A shared vocabulary item must
// never be allowed to select a language on its own.
var (
	hindiMarkers = map[string]int{
		"क्या": 2, "है": 2, "में": 2, "और": 2, "नहीं": 2, "जाना": 2, "जाए": 2,
		"सुरक्षित": 2, "मछली": 2, "नाव": 2, "पानी": 2, "पेड़": 2, "सकुश": 2,
		"कल": 1, "आज": 1, "समुद्र": 1, "साठी": 1, "उद्या": 1, "किंवा": 1,
	}
	marathiMarkers = map[string]int{
		"मराठी": 2, "आहे": 2, "मध्ये": 2, "आणि": 2, "काय": 2, "बोटी": 2,
		"पाणी": 2, "मच्छी": 2, "पेड": 2, "जाणे": 2, "मासे": 2, "नाही": 2,
		"काढायचे": 2, "पकडणे": 2, "सांगा": 2, "निकाली": 2,
		"साठी": 1, "उद्या": 1, "किंवा": 1, "समुद्र": 1, "कल": 1, "आज": 1,
	}
)

// devanagariBreaker scores one side of the Hindi/Marathi split. Map iteration
// order is irrelevant because the markers are summed, so detection stays
// deterministic no matter how the runtime walks the map.
func devanagariBreaker(s string, markers map[string]int) int {
	total := 0
	for marker, w := range markers {
		if strings.Contains(s, marker) {
			total += w
		}
	}
	return total
}

// Detect returns the language of the input, defaulting to English.
//
// A single Indic character is enough to decide, which matters for short
// queries like "Kochi" typed in Tamil script.
func Detect(s string) Code {
	var counts = map[Code]int{}
	for _, r := range s {
		for _, sr := range scriptRanges {
			for _, rg := range sr.ranges {
				if r >= rg.lo && r <= rg.hi {
					counts[sr.code]++
					break
				}
			}
		}
	}
	best, bestN := EN, 0
	for _, c := range All {
		if counts[c] > bestN {
			best, bestN = c, counts[c]
		}
	}
	if bestN == 0 {
		return EN
	}
	if best == HI {
		lower := strings.ToLower(s)
		// A tie goes to Hindi: it is the larger audience for this service, so
		// the ambiguous case should not be answered in the rarer language.
		if devanagariBreaker(lower, marathiMarkers) > devanagariBreaker(lower, hindiMarkers) {
			return MR
		}
	}
	return best
}

// Name returns the endonym for a code, falling back to the code itself so the
// UI can never render an empty label.
func Name(c Code) string {
	if n, ok := Native[c]; ok {
		return n
	}
	return string(c)
}

// Valid reports whether a code is supported.
func Valid(c Code) bool { _, ok := Native[c]; return ok }
