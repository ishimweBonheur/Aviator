package betting

import (
	"aviator/backend/internal/realtime"
	"context"
)

func (s *Service) SetPublisher(p realtime.Publisher) { s.publisher = p }
func (s *Service) publish(ctx context.Context, kind string, roundID, betID int64, amount string) {
	if s.publisher == nil {
		return
	}
	var number int64
	if err := s.db.QueryRow(ctx, "SELECT round_number FROM game_rounds WHERE id=$1", roundID).Scan(&number); err != nil {
		return
	}
	s.publisher.Publish(ctx, realtime.Event{Type: kind, RoundID: roundID, RoundNumber: number, BetID: betID, Amount: amount})
}
