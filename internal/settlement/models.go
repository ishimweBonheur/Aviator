package settlement

type Result struct {
	RoundID        int64
	LostBets       int
	AlreadySettled bool
}
