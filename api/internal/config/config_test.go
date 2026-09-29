package config

import (
	"testing"
	"time"
)

// TestWithDefaultsFillsEverything is the guard for the failure mode that a
// struct-literal Config can introduce: a zero MaxConcurrent would make the
// server block forever on its semaphore, and a zero RateLimitPerMin would reject
// every request after the first. Both are silent — no error, no log line.
func TestWithDefaultsFillsEverything(t *testing.T) {
	got := Config{}.WithDefaults()

	if got.Port != DefaultPort {
		t.Errorf("Port = %q, want %q", got.Port, DefaultPort)
	}
	if got.MaxConcurrent != DefaultMaxConcurrent {
		t.Errorf("MaxConcurrent = %d, want %d", got.MaxConcurrent, DefaultMaxConcurrent)
	}
	if got.RateLimitPerMin != DefaultRateLimitPerMin {
		t.Errorf("RateLimitPerMin = %d, want %d", got.RateLimitPerMin, DefaultRateLimitPerMin)
	}
	if got.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("RequestTimeout = %s, want %s", got.RequestTimeout, DefaultRequestTimeout)
	}
	if got.UpstreamTimeout != DefaultUpstreamTimeout {
		t.Errorf("UpstreamTimeout = %s, want %s", got.UpstreamTimeout, DefaultUpstreamTimeout)
	}
	if got.LLMTimeout != DefaultLLMTimeout {
		t.Errorf("LLMTimeout = %s, want %s", got.LLMTimeout, DefaultLLMTimeout)
	}
	if got.CacheTTL != DefaultCacheTTL {
		t.Errorf("CacheTTL = %s, want %s", got.CacheTTL, DefaultCacheTTL)
	}
	if got.PFZSearchKm != DefaultPFZSearchKm {
		t.Errorf("PFZSearchKm = %v, want %v", got.PFZSearchKm, DefaultPFZSearchKm)
	}
	if got.WaypointKm != DefaultWaypointKm {
		t.Errorf("WaypointKm = %v, want %v", got.WaypointKm, DefaultWaypointKm)
	}
	if got.AllowedOrigins != DefaultAllowedOrigins {
		t.Errorf("AllowedOrigins = %q, want %q", got.AllowedOrigins, DefaultAllowedOrigins)
	}
	if got.CoastalPath != DefaultCoastalPath || got.SnapshotPath != DefaultSnapshotPath {
		t.Errorf("embedded asset paths = %q / %q, want the embed defaults", got.CoastalPath, got.SnapshotPath)
	}
}

// TestWithDefaultsDoesNotOverrideExplicitValues ensures the normalisation is a
// floor and not a blunt "reset everything" — an operator who deliberately sets a
// tight limit must keep it.
func TestWithDefaultsDoesNotOverrideExplicitValues(t *testing.T) {
	in := Config{
		Port:            "9999",
		MaxConcurrent:   1,
		RateLimitPerMin: 2,
		RequestTimeout:  5 * time.Second,
		PFZSearchKm:     10,
		AllowedOrigins:  "https://orca.example",
		WaypointKm:      10,
	}
	got := in.WithDefaults()

	if got.Port != "9999" || got.MaxConcurrent != 1 || got.RateLimitPerMin != 2 {
		t.Errorf("explicit values were overwritten: %+v", got)
	}
	if got.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %s, want 5s", got.RequestTimeout)
	}
	if got.PFZSearchKm != 10 || got.WaypointKm != 10 {
		t.Errorf("explicit distances overwritten: %+v", got)
	}
	if got.AllowedOrigins != "https://orca.example" {
		t.Errorf("AllowedOrigins = %q", got.AllowedOrigins)
	}
	// Untouched fields still get their defaults.
	if got.MarineDays != DefaultMarineDays {
		t.Errorf("MarineDays = %d, want %d", got.MarineDays, DefaultMarineDays)
	}
}

// TestLLMAvailableIsWhitespaceSensitive covers the routine failure of a key
// copied with a trailing newline: the system must treat it as absent rather than
// spend a timeout discovering that on the first request.
func TestLLMAvailableIsWhitespaceSensitive(t *testing.T) {
	if (Config{}).LLMAvailable() {
		t.Error("empty config reports the LLM available")
	}
	if (Config{OpenRouterKey: "  \n\t"}).LLMAvailable() {
		t.Error("whitespace-only key reports the LLM available")
	}
	if !(Config{OpenRouterKey: "sk-or-v1-x"}).LLMAvailable() {
		t.Error("real key reports the LLM unavailable")
	}
}

// TestLoadWithNoEnvironment pins the promise that the service starts with zero
// configuration. Every variable in the deployment blueprint is optional.
func TestLoadWithNoEnvironment(t *testing.T) {
	for _, k := range []string{
		"PORT", "OPENROUTER_API_KEY", "PLAN_MODEL", "NARRATE_MODEL",
		"ALLOWED_ORIGINS", "LLM_TIMEOUT_SEC", "CACHE_TTL_SEC", "MARINE_DAYS",
		"WAYPOINT_KM", "UPSTREAM_TIMEOUT_SEC", "REQUEST_TIMEOUT_SEC",
		"MAX_CONCURRENT", "RATE_LIMIT_PER_MIN", "PFZ_SEARCH_KM",
		"OPENROUTER_BASE_URL", "COASTAL_PATH", "SNAPSHOT_PATH", "ORCA_OFFLINE",
	} {
		t.Setenv(k, "")
	}

	c := Load().WithDefaults()
	if c.LLMAvailable() {
		t.Error("LLM available with no environment at all")
	}
	if c.Offline {
		t.Error("offline enabled with no environment at all")
	}
	if c.Port != DefaultPort {
		t.Errorf("Port = %q, want %q", c.Port, DefaultPort)
	}
	if c.MaxConcurrent != DefaultMaxConcurrent {
		t.Errorf("MaxConcurrent = %d, want %d", c.MaxConcurrent, DefaultMaxConcurrent)
	}
}

// TestPFZSearchKmZeroIsHonoured pins a documented behaviour that did not work.
// The deployment guide says PFZ_SEARCH_KM=0 pins the zone to the waypoint, but
// an operator who set it got the 120 km default and the full six-call marine fan
// instead — the setting that exists to cut upstream load was the one setting
// that silently did nothing.
func TestPFZSearchKmZeroIsHonoured(t *testing.T) {
	t.Setenv("PFZ_SEARCH_KM", "0")
	if got := Load().WithDefaults().PFZSearchKm; got != 0 {
		t.Errorf("PFZ_SEARCH_KM=0 gave %v, want 0 (pinned to the waypoint)", got)
	}

	t.Setenv("PFZ_SEARCH_KM", "45")
	if got := Load().WithDefaults().PFZSearchKm; got != 45 {
		t.Errorf("PFZ_SEARCH_KM=45 gave %v, want 45", got)
	}

	t.Setenv("PFZ_SEARCH_KM", "")
	if got := Load().WithDefaults().PFZSearchKm; got != DefaultPFZSearchKm {
		t.Errorf("unset PFZ_SEARCH_KM gave %v, want the default %v", got, DefaultPFZSearchKm)
	}

	// A negative value is nonsense rather than a deliberate choice, and silently
	// becoming 0 would pin the fan off by accident.
	t.Setenv("PFZ_SEARCH_KM", "-5")
	if got := Load().WithDefaults().PFZSearchKm; got != DefaultPFZSearchKm {
		t.Errorf("negative PFZ_SEARCH_KM gave %v, want the default %v", got, DefaultPFZSearchKm)
	}
}
