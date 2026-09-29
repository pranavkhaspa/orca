package lang

import "testing"

// TestDetect covers every supported script. Detection gates the response
// language, so a miss here means a user receives an answer they cannot read.
func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Code
	}{
		{"plain english", "is it safe to go fishing near Kochi tomorrow?", EN},
		{"hindi", "कल कोच्चि जाना सुरक्षित है क्या?", HI},
		{"marathi", "मुंबळा मासे पकडण्यासाठी नाव काढायचे का", MR},
		{"tamil", "கொச்சி நாளை மீன் பிடிக்க போகலாமா", TA},
		{"telugu", "కోచీ నేపు చేపల వెటేయడం సురక్షితమేమో", TE},
		{"kannada", "ಕೊಚಿ ನಾಳೆ ಮೀನು ಹಿಡಿಯಬಹುದೇ", KN},
		{"malayalam", "കൊച്ചിയിൽ നാളെ മീൻ പിടിക്കണോ", ML},
		{"gujarati", "કોચી કાલે માછલી પકડવાનું છે કે", GU},
		{"odia", "କୋଚିରେ ଆସନ୍ତାଦିନ ମାଛ ଧରିବା ଉଚିତ କି", OR},
		{"bengali", "কোচিতে আগামীকাল মাছ ধরা কি নিরাপদ", BN},
		{"empty defaults to english", "", EN},
		{"numbers and latin default to english", "Kochi 24/10 06:00", EN},
		{"a single indic character is enough", "மீன்", TA},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.in); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDetectIsStable repeats detection to confirm no ordering dependency in the
// argmax over script counts.
func TestDetectIsStable(t *testing.T) {
	in := "कल कोच्चि நாளை மீன்"
	first := Detect(in)
	for i := 0; i < 100; i++ {
		if got := Detect(in); got != first {
			t.Fatalf("Detect unstable: %q then %q", first, got)
		}
	}
}

func TestNameAndValid(t *testing.T) {
	for _, c := range All {
		if !Valid(c) {
			t.Errorf("%q reported invalid but is in All", c)
		}
		if Name(c) == "" {
			t.Errorf("%q has no endonym", c)
		}
	}
	if Valid(Code("xx")) {
		t.Error("unsupported code should not validate")
	}
	if Name(Code("xx")) != "xx" {
		t.Error("unsupported code should fall back to itself, not an empty string")
	}
	if len(All) != 10 {
		t.Errorf("expected 10 supported languages, got %d", len(All))
	}
}

// TestHindiMarathiDisambiguation is the regression test for the misdetection
// found during final validation. Both languages share a script and much of a
// fisherman's vocabulary, so the earlier Marathi-only first-match list answered
// a plainly Hindi sentence in Marathi. Detection gates the response language:
// getting this wrong means the user reads an answer in a language they did not
// ask in, with no indication that it happened.
func TestHindiMarathiDisambiguation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Code
	}{
		// Every one of these is a real query a user would type. Each shares
		// vocabulary with the other language; that is the hard part.
		{"hindi using the shared word for sea", "क्या पुरी के पास समुद्र में मछली पकड़ना सुरक्षित है?", HI},
		{"hindi using the shared word for tomorrow", "कल सुबह कोच्चि जाना सुरक्षित है क्या", HI},
		{"hindi using the shared word for not", "आज समुद्र में नहीं जाना चाहिए", HI},
		{"hindi with the shared question word", "किंवा समुद्र कैसा है आज", HI},
		{"hindi fishing", "मुंबई में कल मछली पकड़ने के लिए नाव ले जाना ठीक है?", HI},
		{"marathi full sentence", "मुंबळा मासे पकडण्यासाठी नाव काढायचे का", MR},
		{"marathi marine question", "आज समुद्रात मासे पकडणे सुरक्षित आहे का", MR},
		{"marathi with the shared word for tomorrow", "उद्या सकाळी बोटी निघावी का", MR},
		{"marathi conjunctive", "मुंबळा आणि कोल्हापूर मध्ये मासे पकडणे", MR},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.in); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSharedDevanagariWordsDoNotSelectALanguage states the rule directly: a
// token that both languages spell the same way carries a weight of 1 on each
// side, so a sentence built from shared words must not resolve to either.
func TestSharedDevanagariWordsDoNotSelectALanguage(t *testing.T) {
	for _, in := range []string{"समुद्र", "उद्या किंवा आज", "समुद्र कल"} {
		if got := Detect(in); got != HI {
			t.Errorf("Detect(%q) = %q, want the Hindi default on a tie", in, got)
		}
	}
}
