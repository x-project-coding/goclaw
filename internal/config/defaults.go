package config

// Default agent configuration values.
// These are the single source of truth — all fallback/default logic should reference these
// instead of hardcoding numeric literals.
const (
	DefaultContextWindow   = 200000
	DefaultMaxTokens       = 8192
	DefaultMaxMessageChars = 32000
	DefaultMaxIterations   = 30

	// DefaultToolRateLimitPerHour caps tool executions per chat session in a
	// sliding hour. It is a runaway-loop guard, not a cost control: real sessions
	// sit at a p99 of ~43 calls/hour, so this leaves deep work untouched while
	// still stopping a loop inside the hour. 0 disables limiting.
	DefaultToolRateLimitPerHour = 1000
	DefaultTemperature          = 0.7
	DefaultHistoryShare         = 0.85
)
