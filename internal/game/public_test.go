package game

import (
	"encoding/json"
	"github.com/shopspring/decimal"
	"strings"
	"testing"
)

func TestRoundSecrets(t *testing.T) {
	seed := "secret"
	point := decimal.NewFromFloat(2.3456)
	for _, status := range []RoundStatus{RoundCreated, RoundBettingOpen, RoundBettingClosed, RoundRunning, RoundCrashed, RoundSettled} {
		data, err := json.Marshal(GameRound{Status: status, ServerSeed: &seed, CrashPoint: &point})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret") != (status == RoundSettled) {
			t.Fatalf("seed leak or missing reveal at %s", status)
		}
		if strings.Contains(string(data), "crash_point") != (status == RoundCrashed || status == RoundSettled) {
			t.Fatalf("crash point leak at %s", status)
		}
	}
}
