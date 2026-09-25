package realtime

const (
	EventRoundOpened      = "ROUND_OPENED"
	EventRoundStarted     = "ROUND_STARTED"
	EventMultiplierUpdate = "MULTIPLIER_UPDATE"
	EventRoundCrashed     = "ROUND_CRASHED"
	EventRoundSettled     = "ROUND_SETTLED"
)

// Event contains public game state only. Decimal values are formatted by the
// publisher with exactly two decimal places; crash points are revealed at crash.
type Event struct {
	Type        string `json:"type"`
	RoundID     int64  `json:"round_id"`
	RoundNumber int64  `json:"round_number"`
	Multiplier  string `json:"multiplier,omitempty"`
	CrashPoint  string `json:"crash_point,omitempty"`
}
