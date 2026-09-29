package lang

import (
	"fmt"
	"strings"

	"orca/internal/domain"
)

// Templates render the answer without any model involvement.
//
// This is the system's hard guarantee of continued operation: if the LLM is
// rate-limited, malformed, or simply absent, a user in Tamil still receives a
// complete, correctly formatted answer in Tamil. The templates therefore
// assemble the same figures the narrator would, from the same structured
// result — they are a degraded presentation path, not a reduced one.
//
// The layout is generated from a per-language frame rather than hand-written
// ten times. Duplicated templates drift: a fix applied to one language silently
// fails in the other nine. Here the structure lives in one place and only the
// words are translated.
type frame struct {
	headline   string // "%s — %s" place, verdict
	zoneLine   string // zone sentence, with 1=place 2=lat 3=lon 4=distance 5=confidence
	zoneCoords string // coordinates line
	confidence string // "confidence: %s"
	kmOffshore string
	// zoneWhy explains why this spot was chosen, with 1=SST 2=band-low
	// 3=band-high. Only numbers and units are inserted: a translated figure in a
	// safety answer is worse than an English one.
	zoneWhy string
	// zoneWhyOut is used when the observed temperature falls outside the band.
	// A single unconditional sentence would assert that the water is "inside
	// the band" when it plainly is not, which is the exact class of error this
	// system exists to avoid.
	zoneWhyOut string
	// snapshotNote is appended when any figure came from the baked dataset
	// instead of a live fetch. A safety answer that does not disclose that it is
	// stale is misleading even when the verdict is correct.
	snapshotNote string
}

var frames = map[Code]frame{
	EN: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — %.0f km offshore. %s",
		zoneCoords:   "",
		confidence:   "Zone confidence: %s.",
		kmOffshore:   "%.0f km offshore",
		zoneWhy:      "Sea surface temperature is %.1f°C, inside the %.0f–%.0f°C band used for fishing-zone identification.",
		zoneWhyOut:   "Sea surface temperature is %.1f°C, outside the %.0f–%.0f°C band used for fishing-zone identification.",
		snapshotNote: "These figures come from a stored snapshot, not a live feed. Check conditions before you leave.",
	},
	HI: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — तट से %.0f किमी दूर। %s",
		confidence:   "क्षेत्र विश्वास: %s।",
		kmOffshore:   "तट से %.0f किमी",
		zoneWhy:      "समुद्री तापमान %.1f°C है, जो मत्स्य क्षेत्र की पहचान के लिए %.0f–%.0f°C बैंड के भीतर है।",
		zoneWhyOut:   "समुद्री तापमान %.1f°C है, जो मत्स्य क्षेत्र की %.0f–%.0f°C बैंड के बाहर है।",
		snapshotNote: "ये आंकड़े लाइव फ़ीड नहीं, सुरक्षित स्नैपशॉट से हैं। निकलने से पहले स्थिति दोबारा जाँचें।",
	},
	TA: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — கடற்கரையிலிருந்து %.0f கி.மீ. %s",
		confidence:   "மண்டல நம்பிக்கை: %s.",
		kmOffshore:   "தீட்டிலிருந்து %.0f கி.மீ.",
		zoneWhy:      "கடல் மேற்பரப்பு வெப்பநிலை %.1f°C; இது மீன் பிடிக்கும் பகுதி கண்டறிதலுக்கான %.0f–%.0f°C வரட்டறைக்குள் உள்ளது.",
		zoneWhyOut:   "கடல் மேற்பரப்பு வெப்பநிலை %.1f°C; இது மீன் பிடிக்கும் பகுதிக்கான %.0f–%.0f°C வரட்டறைக்கு வெளியே உள்ளது.",
		snapshotNote: "இவ்வருத்தாகங்கள் நேரடி வழங்கலிலிருந்து அல்ல, சேமித்த ஸ்னாப்ஷாட்டிலிருந்து கிடைத்தவை. புறப்படுவதற்கு முன் நிலையை மீண்டும் சரிபாருங்கள்.",
	},
	TE: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — తీరం నుండి %.0f కి.మీ. %s",
		confidence:   "మండల నమ్మకత: %s.",
		kmOffshore:   "తీరం నుండి %.0f కి.మీ.",
		zoneWhy:      "సముద్ర ఉష్ణోగ్రత %.1f°C; ఇది చేపల ప్రాంత గుర్తింపు బ్యాండ్ %.0f–%.0f°C లోపల ఉంది.",
		zoneWhyOut:   "సముద్ర ఉష్ణోగ్రత %.1f°C; ఇది చేపల ప్రాంత గుర్తింపు బ్యాండ్ %.0f–%.0f°C బయట ఉంది.",
		snapshotNote: "ఈ సంఖ్యలు ప్రత్యక్ష ఫీడ్ నుండి కాదు, సేవ్ చేసిన స్నాప్‌షాట్ నుండి వచ్చాయి. బయలుదేరే ముందు పరిస్థితిని మళ్లీ తనిఖీ చేయండి.",
	},
	KN: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — ಕರದಿಂದ %.0f ಕಿ.ಮೀ. %s",
		confidence:   "ವಲಯ ವಿಶ್ವಾಸ: %s.",
		kmOffshore:   "ಕರದಿಂದ %.0f ಕಿ.ಮೀ.",
		zoneWhy:      "ಸಮುದ್ರ ತಾಪಮಾನ %.1f°C; ಇದು ಮೀನು ಪ್ರದೇಶ ಗುರುತಿಸುವ ಬ್ಯಾಂಡ್ %.0f–%.0f°C ಒಳಗಿದೆ.",
		zoneWhyOut:   "ಸಮುದ್ರ ತಾಪಮಾನ %.1f°C; ಇದು ಮೀನು ಪ್ರದೇಶದ %.0f–%.0f°C ಬ್ಯಾಂಡ್‌ಗಿಂತ ಹೊರಗಿದೆ.",
		snapshotNote: "ಈ ಸಂಖ್ಯೆಗಳು ನೇರ ಫೀಡ್‌ನಲ್ಲ, ಉಳಿಸಿದ ಸ್ನಾಪ್‌ಷಾಟ್‌ನಿಂದ ಬಂದವು. ಹೊರಡುವ ಮೊದಲು ಪರಿಸ್ಥಿತಿ ಮತ್ತೆ ಪರಿಶೀಲಿಸಿ.",
	},
	ML: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — തീരത്തിൽ നിന്ന് %.0f കി.മീ. %s",
		confidence:   "മേഖല വിശ്വാസം: %s.",
		kmOffshore:   "തീരത്തിൽ നിന്ന് %.0f കി.മീ.",
		zoneWhy:      "കടൽ പ്രതിപുഠനം %.1f°C; ഇത് മീൻ പ്രദേശം കണ്ടെത്തുന്ന %.0f–%.0f°C പരിധിയിലുള്ളതാണ്.",
		zoneWhyOut:   "കടൽ പ്രതിപുഠനം %.1f°C; ഇത് മീൻ പ്രദേശത്തിനുള്ള %.0f–%.0f°C പരിധിക്ക് പുറത്താണ്.",
		snapshotNote: "ഈ അക്കങ്ങൾ തതസമയ ഫീഡിൽ നിന്നല്ല, സംഭവിച്ച സ്നാപ്പ്‌ഷോട്ടിൽ നിന്നാണ്. പുറക്കുന്നതിന് മുമ്പ് അവസ്ഥ വീണ്ടും പരിശോധിക്കുക.",
	},
	GU: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — દરિયાકિનારથી %.0f કિમી. %s",
		confidence:   "વિસ્તાર વિશ્વાસ: %s.",
		kmOffshore:   "દરિયાકિનારથી %.0f કિમી",
		zoneWhy:      "દરિયાનું તાપમાન %.1f°C; આ માછ પ્રદેશ ઓળખવાના %.0f–%.0f°C બેન્ડમાં છે.",
		zoneWhyOut:   "દરિયાનું તાપમાન %.1f°C; આ માછ પ્રદેશ ઓળખવાના %.0f–%.0f°C બેન્ડથી બહાર છે.",
		snapshotNote: "આ આંકડા લાઇવ ફીડમાંથી નહીં, સાચવેલા સ્નેપશોટમાંથી છે. નીકળતાં પહેલાં હાલની સ્થિતિ ફરી તપાસો.",
	},
	OR: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — ସହରରୁ %.0f କିମି ଦୂରେ। %s",
		confidence:   "ଅଞ୍ଚଳ ଆତ୍ମବିଶ୍ୱାସ: %s।",
		kmOffshore:   "ସହରରୁ %.0f କିମି",
		zoneWhy:      "ସମୁଦ୍ର ତାପମାନ %.1f°C; ଏହା ମାଛ ଅଞ୍ଚଳ ଚିହ୍ନଟ ପରିସର %.0f–%.0f°C ଭିତରେ ଅଛି।",
		zoneWhyOut:   "ସମୁଦ୍ର ତାପମାନ %.1f°C; ଏହା ମାଛ ଅଞ୍ଚଳ ଚିହ୍ନଟ %.0f–%.0f°C ପରିସରରୁ ବାହାରେ ଅଛି।",
		snapshotNote: "ଏହି ସଂଖ୍ୟାଗୁଡ଼ିକ ସର୍ବେଚ୍ଚ ଫିଡରୁ ନୁହେଁ, ସାଚିଥିବା ସ୍ନାପସଟରୁ ଅଛନ୍ତି। ବାହାରିବା ପୂର୍ବରୁ ପରିସ୍ଥିତି ପୁନଃ ଯାଞ୍ଚ କରନ୍ତୁ।",
	},
	BN: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — উপকূল থেকে %.0f কিমি দূরে। %s",
		confidence:   "এলাকার নির্ভরযোগ্যতা: %s।",
		kmOffshore:   "উপকূল থেকে %.0f কিমি",
		zoneWhy:      "সমুদ্রপৃষ্ঠের তাপমাত্রা %.1f°C; এটি মাছ ধরার এলাকা শনাক্তের %.0f–%.0f°C ব্যান্ডের ভিতরে।",
		zoneWhyOut:   "সমুদ্রপৃষ্ঠের তাপমাত্রা %.1f°C; এটি মাছ ধরার এলাকার %.0f–%.0f°C ব্যান্ডের বাইরে।",
		snapshotNote: "এই সংখ্যাগুলো সরাসরি ফিড থেকে নয়, সংরক্ষিত স্ন্যাপশট থেকে এসেছে। বের হওয়ার আগে অবস্থা আবার দেখে নিন।",
	},
	MR: {
		headline:     "**%s — %s**",
		zoneLine:     "%s %.4f, %.4f — किनार्यापासून %.0f किमी. %s",
		confidence:   "विभाग विश्वास: %s.",
		kmOffshore:   "किनार्यापासून %.0f किमी",
		zoneWhy:      "समुद्रपृष्ठ तापमान %.1f°C; हे मासे पकडण्याच्या विभाग ओळखण्याच्या %.0f–%.0f°C बँडमध्ये आहे.",
		zoneWhyOut:   "समुद्रपृष्ठ तापमान %.1f°C; हे मासे पकडण्याच्या विभाग %.0f–%.0f°C बँडच्या बाहेर आहे.",
		snapshotNote: "हे आकडे थेट फीडमधून नाहीत, जतन केलेल्या स्नॅपशॉटमधून आहेत. बाहेर जाण्यापूर्वी स्थिती पुन्हा तपासा.",
	},
}

// TTSName is the place name as written in the language's own script, for the
// speech synthesis path. The map is keyed by the English reference name.
var TTSName = map[string]map[Code]string{
	"Kochi":         {TA: "கொச்சி", TE: "కోచి", KN: "ಕೊಚಿ", ML: "കൊച്ചി", HI: "कोची"},
	"Chennai":       {TA: "சென்னை", TE: "చెన్నై", KN: "ಚೆನ್ನೈ", ML: "ചെന്നൈ", HI: "चेन्नई"},
	"Mumbai":        {TA: "மும்பை", TE: "ముంబై", KN: "ಮುಂಬೈ", ML: "മുംബൈ", HI: "मुंबई"},
	"Kozhikode":     {TA: "கோഴിക്കോട്", TE: "కోఴికోడ్", KN: "ಕೊಝಿಕ್ಕೋಡು", ML: "കോഴിക്കോട്", HI: "कोझिकोड"},
	"Mangaluru":     {TA: "மங்களூர்", TE: "మంగళూరు", KN: "ಮಂಗಳೂರು", ML: "മംഗളാപുരം", HI: "मंगलूर"},
	"Visakhapatnam": {TA: "விசாகபட்டணம்", TE: "విశాఖపట్టణం", KN: "ವಿಶಾಖಪಟ್ಟಣ", ML: "വിശാഖപട്ടണം", HI: "विशाखापट्टनम"},
	"Kollam":        {TA: "கொல்லம்", TE: "కోల్లం", KN: "ಕೊಲ್ಲಂ", ML: "കൊല്ലം", HI: "कोल्लम"},
	"Kolkata":       {TA: "கல்கத்தா", TE: "కలకత్తా", KN: "ಕೊಲ್ಕತಾ", ML: "കൊൽകത്ത", HI: "कोलकाता"},
	"Rameswaram":    {TA: "ராமேஸ்வரம்", TE: "రామేశ్వరం", KN: "ರಾಮೇಶ್ವರ", ML: "രാമേശ്വരം", HI: "रामेश्वर"},
	"Tuticorin":     {TA: "தூத்துக்குடி", TE: "తుత్తుకోడి", KN: "ತುತ್ತುಕೋಡ", ML: "തുത്തുകുട്ടി", HI: "ठुट्टुकुड"},
	"Nagapattinam":  {TA: "நாகப்பட்டினம்", TE: "నాగపట్టినం", KN: "ನಾಗಪಟ್ಟಿನಂ", ML: "നാഗപ്പട്ടണം", HI: "नागपट्टनम"},
	"Pondicherry":   {TA: "பாண்டிச்சேரி", TE: "పాండిచ్చెరి", KN: "ಪಾಂಡಿಚೆರಿ", ML: "പൊണ്ടിച്ചേരി", HI: "पुदुचेरी"},
	"Port Blair":    {TA: "போர்ட் பிளேர்", TE: "పోర్ట్ బ్లైర్", KN: "ಪೋರ್ಟ್ ಬ್ಲೇರ್", ML: "പോർട്ട് ബ്ലെയർ", HI: "पोर्ट ब्लेयर"},
	"Alibag":        {TA: "அலிபாக்", TE: "అలిబాగ్", KN: "ಅಲಿಬಾಗ್", ML: "അലിബാഗ്", HI: "अलिबाग"},
	"Kavaratti":     {TA: "கவரட்டி", TE: "కవరత్తి", KN: "ಕವರತ್ತಿ", ML: "കവരത്തി", HI: "कवरत्ती"},
}

// LocalisePlace returns the place name in the requested script where a
// translation is known, and the English reference name otherwise.
//
// The English name is always correct even when untranslated, so a missing
// translation degrades to a usable answer rather than an empty one.
func LocalisePlace(name string, c Code) string {
	if byLang, ok := TTSName[name]; ok {
		if n, ok := byLang[c]; ok {
			return n
		}
	}
	return name
}

// Render produces a deterministic answer in the requested language.
func zoneSST(f domain.Findings) float64 {
	if f.PFZ.SampleSSTC != 0 {
		return f.PFZ.SampleSSTC
	}
	return f.Marine.SSTC
}

func Render(f domain.Findings, c Code) string {
	fr, ok := frames[c]
	if !ok {
		fr = frames[EN]
		c = EN
	}
	conf := fmt.Sprintf(fr.confidence, ConfidenceWord(c, f.PFZ.Confidence))
	place := LocalisePlace(f.Geo.Name, c)

	head := fmt.Sprintf(fr.headline, place, VerdictWord(c, f.Verdict.Level))
	zone := fmt.Sprintf(fr.zoneLine, place, f.PFZ.Lat, f.PFZ.Lon, f.PFZ.DistanceKm, conf)
	why := Rationale(f, c)

	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n")
	b.WriteString(strings.Join(why, "\n\n"))
	if len(f.PFZ.Reasoning) > 0 {
		b.WriteString("\n\n")
		if c != EN {
			b.WriteString(fmt.Sprintf("**%s**\n", ZoneTitle(c)))
		} else {
			b.WriteString("**Where to fish**\n")
		}
		b.WriteString(zone)
		// The localized band sentence always goes first: it carries the published
		// rule, which is the part a reader needs in order to trust the zone.
		// Branch on the actual measurement: the sentence must never claim the
		// water is inside the band when it is not.
		//
		// The temperature is the one at the point this section is describing,
		// which is the zone and not the coast. Reading the coastal value here
		// put two sea-surface temperatures in one paragraph next to a wave
		// height taken at the zone, so the two sentences described different
		// places and a reader checking the numbers could not reconcile them.
		sst := zoneSST(f)
		tpl := fr.zoneWhyOut
		if sst >= domain.PFZBandLoC && sst <= domain.PFZBandHiC {
			tpl = fr.zoneWhy
		}
		if tpl != "" {
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf(tpl, sst, domain.PFZBandLoC, domain.PFZBandHiC))
		}
		// The engine additionally reasons about sea state and swell in prose.
		// That prose is English-only, so it is shown for English alone; the
		// sentence above already covers the zone claim in every other language.
		if c == EN {
			var extra []string
			for _, r := range f.PFZ.Reasoning {
				// Drop the line that restates the band, now covered above.
				if strings.Contains(r, "productive band") {
					continue
				}
				extra = append(extra, r)
			}
			if len(extra) > 0 {
				b.WriteString("\n")
				b.WriteString(strings.Join(extra, "\n"))
			}
		}
	}
	if notice := snapshotNotice(f, fr); notice != "" {
		b.WriteString("\n\n")
		b.WriteString(notice)
	}
	return b.String()
}

// snapshotNotice discloses, in the reader's own language, that the figures came
// from the baked dataset rather than a live fetch.
//
// This is deliberately part of the answer text and not only a badge in the UI.
// The answer is what gets read aloud, forwarded, and acted on; a safety message
// that hides its own staleness is wrong even when the verdict happens to be
// right.
func snapshotNotice(f domain.Findings, fr frame) string {
	if fr.snapshotNote == "" {
		return ""
	}
	for _, c := range f.Prov {
		if c.Live {
			return ""
		}
	}
	if len(f.Prov) == 0 {
		return ""
	}
	return fr.snapshotNote
}
