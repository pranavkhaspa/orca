// Package config holds runtime configuration, all sourced from environment
// variables with sane defaults so the service runs with zero configuration.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port           string
	OpenRouterKey  string
	PlanModel      string
	NarrateModel   string
	LLMTimeout     time.Duration
	CoastalPath    string
	SnapshotPath   string
	CacheTTL       time.Duration
	MarineDays     int
	WaypointKm     float64
	AllowedOrigins string

	// PlannerLLMTimeout is the model budget for the router's *refinement* pass,
	// and it is deliberately far shorter than LLMTimeout.
	//
	// The deterministic router has already produced a complete, correct plan
	// before the model is consulted, and every failure path in the planner
	// returns that plan. The model call is therefore an optional improvement to
	// place-name extraction, not an input to the analysis — so blocking the
	// whole pipeline on it for the narration budget charged the user ten
	// seconds of latency for a cosmetic edit, on the one request where the
	// result was already certain. Five seconds is generous for extracting a
	// place name from a sentence; past that the router's answer is the better
	// one anyway, because it is the answer the language-parity test guarantees.
	PlannerLLMTimeout time.Duration

	// Operational limits. These were hardcoded when there was one deployment to
	// think about; on a free-tier host the concurrency cap and the per-client
	// rate limit are the two numbers that decide whether the service survives
	// its own popularity, so they are configuration rather than constants.
	UpstreamTimeout time.Duration // one HTTP call to a data provider
	RequestTimeout  time.Duration // the whole request, including the model calls
	MaxConcurrent   int           // simultaneous upstream fan-outs
	RateLimitPerMin int           // per client IP
	PFZSearchKm     float64       // how far offshore the fishing-zone fan reaches; 0 pins it to the waypoint
	// pfzSearchSet records that PFZSearchKm was configured as zero on purpose,
	// so WithDefaults can tell "the operator chose 0" from "nobody set it". It
	// stays false for a struct literal, which is why an unconfigured Config
	// still picks up the default like every other field.
	pfzSearchSet   bool
	OpenRouterBase string // overridable so a proxy or a second provider can be used
	Offline        bool   // ORCA_OFFLINE=1: answer from the snapshot, make no calls
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// envFloatOr is envFloat with the extra ability to report whether the variable
// was present and parsed. The distinction matters for PFZ_SEARCH_KM, where zero
// is a meaningful setting rather than a missing one: it pins the fishing zone
// to the waypoint and cuts the marine fan from six calls to none. WithDefaults
// has to replace a zero that means "unset" while leaving a zero that the
// operator chose, and a plain float cannot tell them apart.
func envFloatOr(k string, def float64, present *bool) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*present = true
			return f
		}
	}
	return def
}

// Documented defaults. They live here rather than being inlined at each use
// site so that a zero-valued Config, a test, and a real deployment all agree on
// what "unset" means.
const (
	DefaultPort            = "8080"
	DefaultPlanModel       = "deepseek/deepseek-v3.2"
	DefaultNarrateModel    = "deepseek/deepseek-v3.2"
	DefaultOpenRouterBase  = "https://openrouter.ai/api/v1"
	DefaultUpstreamTimeout = 20 * time.Second
	DefaultRequestTimeout  = 60 * time.Second
	DefaultLLMTimeout      = 25 * time.Second
	// The router's refinement pass is optional and already has a correct answer
	// waiting, so it gets a budget sized for "did the model get back in time" and
	// not for "did the model write a good paragraph".
	DefaultPlannerLLMTimeout = 5 * time.Second
	DefaultCacheTTL          = 15 * time.Minute
	DefaultMarineDays        = 5
	DefaultWaypointKm        = 45.0
	DefaultPFZSearchKm       = 120.0
	DefaultMaxConcurrent     = 6
	DefaultRateLimitPerMin   = 12
	DefaultAllowedOrigins    = "*"
	DefaultCoastalPath       = "embed:coastal_towns.json"
	DefaultSnapshotPath      = "embed:snapshot.json"
)

// WithDefaults returns a copy with every unset field replaced by its documented
// default.
//
// Config is constructed as a struct literal in several places, including tests,
// and a caller that forgets MaxConcurrent would otherwise get a service that
// blocks forever on a zero-capacity semaphore and one that rejects every request
// after the first. Normalising in one place makes "zero means the default" true
// everywhere rather than true only for values that came out of the environment.
func (c Config) WithDefaults() Config {
	if c.Port == "" {
		c.Port = DefaultPort
	}
	if c.PlanModel == "" {
		c.PlanModel = DefaultPlanModel
	}
	if c.NarrateModel == "" {
		c.NarrateModel = DefaultNarrateModel
	}
	if c.OpenRouterBase == "" {
		c.OpenRouterBase = DefaultOpenRouterBase
	}
	if c.UpstreamTimeout <= 0 {
		c.UpstreamTimeout = DefaultUpstreamTimeout
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}
	if c.LLMTimeout <= 0 {
		c.LLMTimeout = DefaultLLMTimeout
	}
	if c.PlannerLLMTimeout <= 0 {
		c.PlannerLLMTimeout = DefaultPlannerLLMTimeout
	}
	if c.CacheTTL <= 0 {
		c.CacheTTL = DefaultCacheTTL
	}
	if c.MarineDays <= 0 {
		c.MarineDays = DefaultMarineDays
	}
	if c.WaypointKm <= 0 {
		c.WaypointKm = DefaultWaypointKm
	}
	// Zero is a deliberate setting here, not a missing one: it pins the zone to
	// the waypoint. It is only replaced when the field was never populated, so
	// that an operator who typed 0 gets what they asked for instead of a silent
	// 120 km fan and six upstream calls they thought they had turned off.
	if c.PFZSearchKm < 0 || (c.PFZSearchKm == 0 && !c.pfzSearchSet) {
		c.PFZSearchKm = DefaultPFZSearchKm
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	if c.RateLimitPerMin <= 0 {
		c.RateLimitPerMin = DefaultRateLimitPerMin
	}
	if c.AllowedOrigins == "" {
		c.AllowedOrigins = DefaultAllowedOrigins
	}
	if c.CoastalPath == "" {
		c.CoastalPath = DefaultCoastalPath
	}
	if c.SnapshotPath == "" {
		c.SnapshotPath = DefaultSnapshotPath
	}
	return c
}

func Load() Config {
	origin := env("ALLOWED_ORIGINS", DefaultAllowedOrigins)
	var pfzUnset bool
	c := Config{
		Port:          env("PORT", DefaultPort),
		OpenRouterKey: os.Getenv("OPENROUTER_API_KEY"),
		PlanModel:     env("PLAN_MODEL", DefaultPlanModel),
		NarrateModel:  env("NARRATE_MODEL", DefaultNarrateModel),
		LLMTimeout:    time.Duration(envFloat("LLM_TIMEOUT_SEC", DefaultLLMTimeout.Seconds())) * time.Second,

		PlannerLLMTimeout: time.Duration(envFloat("PLANNER_LLM_TIMEOUT_SEC", DefaultPlannerLLMTimeout.Seconds())) * time.Second,
		CoastalPath:       env("COASTAL_PATH", DefaultCoastalPath),
		SnapshotPath:      env("SNAPSHOT_PATH", DefaultSnapshotPath),
		CacheTTL:          time.Duration(envFloat("CACHE_TTL_SEC", DefaultCacheTTL.Seconds())) * time.Second,
		MarineDays:        int(envFloat("MARINE_DAYS", DefaultMarineDays)),
		WaypointKm:        envFloat("WAYPOINT_KM", DefaultWaypointKm),
		AllowedOrigins:    origin,

		UpstreamTimeout: time.Duration(envFloat("UPSTREAM_TIMEOUT_SEC", DefaultUpstreamTimeout.Seconds())) * time.Second,
		RequestTimeout:  time.Duration(envFloat("REQUEST_TIMEOUT_SEC", DefaultRequestTimeout.Seconds())) * time.Second,
		MaxConcurrent:   int(envFloat("MAX_CONCURRENT", DefaultMaxConcurrent)),
		RateLimitPerMin: int(envFloat("RATE_LIMIT_PER_MIN", DefaultRateLimitPerMin)),
		PFZSearchKm:     envFloatOr("PFZ_SEARCH_KM", DefaultPFZSearchKm, &pfzUnset),
		OpenRouterBase:  env("OPENROUTER_BASE_URL", DefaultOpenRouterBase),
		Offline:         os.Getenv("ORCA_OFFLINE") == "1",
	}
	// A zero the operator typed in the environment is left alone; anything else
	// that arrived as the zero value of the struct is filled in.
	c.pfzSearchSet = pfzUnset
	return c
}

// LLMAvailable reports whether a model call is possible. The entire system is
// required to work without it (INSTR.md standing rule 1), so callers must
// branch on this rather than assume failure.
func (c Config) LLMAvailable() bool { return strings.TrimSpace(c.OpenRouterKey) != "" }
