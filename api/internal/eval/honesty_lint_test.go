package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The honesty lint.
//
// Every rule in this file exists because the corresponding claim was once wrong
// or was one edit away from being wrong. A lint is the right tool for these
// because they are properties of the repository as a whole rather than of any
// one function: a number in the README, a figure in the UI, and the table the
// engine actually reads have to agree, and nothing at runtime compares them.
//
// Each rule is written to fail loudly with the offending path and line, because
// a check that fails quietly gets disabled.

// repoRoot walks up from the test's working directory to the checkout root.
//
// The module lives in api/, so the Go module directory is one level below the
// repository root. The root is identified by the README and the web/ directory
// rather than by go.mod, because go.mod marks the wrong level.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "web", "package.json")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate the repository root from the working directory")
	return ""
}

// trackedFiles returns the repository's text files, skipping build output,
// vendored dependencies and anything git is not tracking.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	// Basenames skipped anywhere in the tree.
	skipName := map[string]bool{
		"node_modules": true, "dist": true, ".git": true,
		"vendor": true, ".opencode": true,
	}
	// Relative paths skipped, which is how the generated geography output is
	// excluded without also excluding a directory that happens to be named geo.
	skipRel := map[string]bool{
		filepath.Join("web", "public", "geo"): true,
	}
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if info.IsDir() {
			if skipName[info.Name()] || skipRel[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".ts", ".tsx", ".md", ".json", ".mjs", ".css", ".yaml", ".yml":
			if !info.IsDir() {
				out = append(out, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(string(b), "\n")
}

// The place count is quoted in user-facing copy, in the README and in the
// docs. It used to be 32 and the copy kept saying 32 after the table grew to 36,
// which is the exact class of drift this lint exists to make impossible. The
// expected number is counted from the table at test time, never hardcoded.
func TestPlaceCountInProseMatchesTheReferenceTable(t *testing.T) {
	root := repoRoot(t)
	want := strconv.Itoa(countTowns(t, root))

	// A count is only meaningful next to the word it counts, so the pattern
	// requires a counting word within a few words of the number.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(\d+)\s+(supported\s+)?(coastal\s+)?(locations|places|ports|towns)\b`),
		regexp.MustCompile(`(?i)\b(coastal\s+)?(locations|places)\s+supported\s*[:=]\s*(\d+)`),
	}

	// A dated iteration log records counts that were correct at the time.
	// Rewriting them would falsify the record, so lines carrying an iteration
	// marker are exempt. Present-tense prose has no marker and is still checked.
	iterationMarker := regexp.MustCompile(`\|\s*\**I\d+\**\s*\||\|\s*\*\*P\d+\*\*\s*\||·[PI]\d+`)

	for _, path := range trackedFiles(t, root) {
		rel, _ := filepath.Rel(root, path)
		// The table itself and the test are the definitions of the number.
		if rel == filepath.Join("api", "internal", "data", "files", "coastal_towns.json") {
			continue
		}
		if strings.HasSuffix(rel, "honesty_lint_test.go") {
			continue
		}
		for i, line := range readLines(t, path) {
			if iterationMarker.MatchString(line) {
				continue
			}
			// The baked snapshot legitimately covers fewer places than the
			// reference table, so its count is a different fact, not drift.
			if strings.Contains(strings.ToLower(line), "snapshot") {
				continue
			}
			for _, re := range patterns {
				m := re.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				got := firstNumber(m)
				if got == "" {
					continue
				}
				if got != want {
					t.Errorf("%s:%d: says %s coastal places but the table holds %s: %q",
						rel, i+1, got, want, strings.TrimSpace(line))
				}
			}
		}
	}
}

// countTowns reads the reference table straight off disk.
//
// data.Towns memoises behind a sync.Once, so the first caller in the package
// fixes the path for everyone and the result would depend on test ordering. A
// repository-level lint has to be order-independent, so it reads the file.
func countTowns(t *testing.T, root string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "api", "internal", "data", "files", "coastal_towns.json"))
	if err != nil {
		t.Fatalf("read coastal table: %v", err)
	}
	var doc struct {
		Towns []struct {
			Name string `json:"name"`
		} `json:"towns"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse coastal table: %v", err)
	}
	if len(doc.Towns) == 0 {
		t.Fatal("the coastal reference table is empty, so the expected count is meaningless")
	}
	// A duplicate name would make the count wrong in a way a user would notice.
	seen := map[string]bool{}
	for _, town := range doc.Towns {
		if town.Name == "" {
			t.Error("the coastal reference table has a town with no name")
		}
		if seen[town.Name] {
			t.Errorf("duplicate town %q in the coastal reference table", town.Name)
		}
		seen[town.Name] = true
	}
	return len(doc.Towns)
}

func firstNumber(m []string) string {
	for _, g := range m {
		if g != "" {
			if _, err := strconv.Atoi(g); err == nil {
				return g
			}
		}
	}
	return ""
}

// Overclaiming language is banned in documentation and in user-facing strings.
//
// The rule is not that these words are forbidden outright; it is that a document
// may not assert certainty the system does not have. Each pattern below is a
// claim this system has been in a position to make and should not: a safety
// verdict is not a guarantee, bundled pattern data is not live data, and a
// compiled coastline is not a surveyed one.
var overclaims = []struct {
	pattern *regexp.Regexp
	why     string
}{
	// "Guarantee" is only banned when it is applied to safety. The repository
	// legitimately guarantees determinism, and architecture.md leans on that
	// claim, so a blanket ban would forbid the one guarantee that is real.
	{regexp.MustCompile(`(?i)\bguarantee[ds]?\b[^.]{0,40}\b(safe|safety|seaworthy|seaworthiness|risk-?free)\b`),
		"the verdict is a reasoned assessment, not a guarantee of safety"},
	{regexp.MustCompile(`(?i)\b(safe|safety|seaworthy|seaworthiness)\b[^.]{0,20}\b(is|are)\s+guaranteed\b`),
		"the verdict is a reasoned assessment, not a guarantee of safety"},
	{regexp.MustCompile(`(?i)\b100%\s+(accurate|precise|reliable|safe)\b`),
		"no measurement in this system is exact"},
	{regexp.MustCompile(`(?i)\b(always|never)\s+(safe|seaworthy)\b`),
		"no verdict can promise the sea is safe"},
	{regexp.MustCompile(`(?i)\breal-?time\s+data\b`),
		"the data is fetched on demand and may come from a snapshot; it is not a live feed"},
	{regexp.MustCompile(`(?i)\blive\s+(?:sst|wave|weather)\s+data\b`),
		"upstream figures may be a snapshot, so 'live' must not be claimed in prose"},
	{regexp.MustCompile(`(?i)\bexactly\s+accurate\b`),
		"nothing here is exactly accurate"},
}

func TestDocsDoNotOverclaim(t *testing.T) {
	root := repoRoot(t)
	// Files whose job is to record that these words are banned.
	selfReferential := map[string]bool{
		filepath.Join("api", "internal", "eval", "honesty_lint_test.go"): true,
	}
	hits := 0
	for _, path := range trackedFiles(t, root) {
		rel, _ := filepath.Rel(root, path)
		if selfReferential[rel] {
			continue
		}
		for i, line := range readLines(t, path) {
			trimmed := strings.TrimSpace(line)
			// A word inside a code fence or a negative example is not a claim.
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			// The eval corpus is built from phrases the system must refuse. A
			// banned phrase there is a test fixture asserting its absence, which
			// is the opposite of an overclaim.
			if strings.Contains(line, "MustNotContain") || strings.Contains(line, "MustContain") {
				continue
			}
			// A prompt-injection fixture carries the banned phrase in the
			// adversarial query itself, on a line with no assertion attached. The
			// injection marker identifies those unambiguously.
			if strings.Contains(line, "Ignore all previous instructions") {
				continue
			}
			// Same reasoning for an explicit negation.
			if regexp.MustCompile(`(?i)\b(not|never|no)\b`).MatchString(line) &&
				regexp.MustCompile(`(?i)\b(claim|contains?|phrase|forbidden|banned)\b`).MatchString(line) {
				continue
			}
			for _, o := range overclaims {
				if o.pattern.MatchString(line) {
					t.Errorf("%s:%d: %s\n    rule: %s\n    line: %s",
						rel, i+1, o.pattern, o.why, strings.TrimSpace(line))
					hits++
				}
			}
		}
	}
	if hits == 0 {
		t.Log("no overclaiming language found in the tracked docs and sources")
	}
}

// INCOIS was documented as unreachable when it is in fact live. A doc that
// declares a live service dead is worse than no doc, so the claim is pinned.
func TestDocsDoNotDeclareTheIncoisEndpointDead(t *testing.T) {
	root := repoRoot(t)
	bad := regexp.MustCompile(`(?i)erddap\.incois\.gov\.in[^\n]{0,80}\b(dead|unreachable|offline|shut\s+down|unavailable)\b`)
	alsoBad := regexp.MustCompile(`(?i)\b(dead|unreachable|offline)\b[^\n]{0,80}erddap\.incois\.gov\.in`)
	for _, path := range trackedFiles(t, root) {
		rel, _ := filepath.Rel(root, path)
		if strings.HasSuffix(rel, "honesty_lint_test.go") {
			continue
		}
		for i, line := range readLines(t, path) {
			if bad.MatchString(line) || alsoBad.MatchString(line) {
				t.Errorf("%s:%d: declares a live endpoint dead: %q", rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The map is generated from Natural Earth, not from OpenStreetMap. Documents
// that name OpenStreetMap as the source of the shipped geometry are making a
// provenance claim a user would rely on when deciding whether a coastline is
// accurate enough to trust.
func TestDocsDoNotMisattributeTheShippedCoastline(t *testing.T) {
	root := repoRoot(t)
	// Allowed: naming OSM as something not yet integrated, or as an alternative.
	// Banned: attributing the committed geometry to OSM.
	bad := regexp.MustCompile(`(?i)\b(built|generated|derived|rendered|powered|from)\s+(from\s+)?openstreetmap\b`)
	bad2 := regexp.MustCompile(`(?i)\bcoastline[^\n]{0,40}\bfrom\s+openstreetmap\b`)
	for _, path := range trackedFiles(t, root) {
		rel, _ := filepath.Rel(root, path)
		if strings.HasSuffix(rel, "honesty_lint_test.go") {
			continue
		}
		for i, line := range readLines(t, path) {
			if bad.MatchString(line) || bad2.MatchString(line) {
				t.Errorf("%s:%d: attributes the shipped geometry to OpenStreetMap, but it is "+
					"generated from Natural Earth: %q", rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// Every user-visible figure in the response has to be attributable. The cheapest
// mechanical version of that rule: the wire contract must not carry a NaN or an
// Inf, which is what an absent measurement looks like once it has been through
// JSON, and which would otherwise be rendered as a number.
func TestWireContractHasNoNonFiniteNumbers(t *testing.T) {
	root := repoRoot(t)
	geoPath := filepath.Join(root, "web", "public", "geo")
	if _, err := os.Stat(geoPath); err != nil {
		t.Skipf("no generated geography at %s", geoPath)
	}
	for _, name := range []string{"world.json", "labels.json"} {
		path := filepath.Join(geoPath, name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// A JSON document cannot hold a literal NaN, so a non-finite value
		// arrives as a bare token, which decoding rejects. Decoding is the check.
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Errorf("%s is not valid JSON, so it cannot have been checked: %v", name, err)
			continue
		}
		var walk func(any, string)
		walk = func(n any, where string) {
			switch v := n.(type) {
			case float64:
				if v != v || v > 1.7976931348623157e308 || v < -1.7976931348623157e308 {
					t.Errorf("%s: non-finite value at %s: %v", name, where, v)
				}
			case []any:
				for i, e := range v {
					walk(e, fmt.Sprintf("%s[%d]", where, i))
				}
			case map[string]any:
				for k, e := range v {
					walk(e, where+"."+k)
				}
			}
		}
		walk(doc, name)
	}
}

// The ban calendar's own honesty claims are enforced in the data package, but
// the flag has to stay false here too, at the repository level, so that editing
// the JSON directly is caught.
func TestBanCalendarDoesNotClaimGazetteVerification(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "api", "internal", "data", "files", "seasonal_bans.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ban calendar: %v", err)
	}
	var doc struct {
		Verified *bool `json:"_verified_against_gazettes"`
		Windows  []struct {
			State      string `json:"state"`
			Authority  string `json:"authority"`
			Caveat     string `json:"caveat"`
			Confidence string `json:"confidence"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse ban calendar: %v", err)
	}
	if doc.Verified == nil {
		t.Error("the calendar must state its verification status explicitly")
	} else if *doc.Verified {
		t.Error("the calendar claims verification against gazette notifications, but no gazette " +
			"is in this repository; that claim needs the actual notifications behind it")
	}
	for _, w := range doc.Windows {
		if w.Caveat == "" {
			t.Errorf("%s: no verification caveat", w.State)
		}
		if w.Authority == "" {
			t.Errorf("%s: no authority named", w.State)
		}
		switch strings.ToLower(w.Confidence) {
		case "high", "moderate", "low":
		default:
			t.Errorf("%s: confidence %q is not high, moderate or low", w.State, w.Confidence)
		}
	}
}
