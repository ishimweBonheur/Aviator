package admin

// AnalyticsResponse documents decimal strings without losing money precision.
type AnalyticsResponse struct {
	TotalWagered       string         `json:"total_wagered" example:"100000.00"`
	TotalPayouts       string         `json:"total_payouts" example:"94000.00"`
	GGR                string         `json:"ggr" example:"6000.00"`
	RTPPercent         string         `json:"rtp_percent" example:"94.00"`
	HouseMarginPercent string         `json:"house_margin_percent" example:"6.00"`
	Deposits           string         `json:"deposits" example:"120000.00"`
	Withdrawals        string         `json:"withdrawals" example:"30000.00"`
	BetCount           int64          `json:"bet_count"`
	ActivePlayers      int64          `json:"active_players"`
	Daily              []DailyMetrics `json:"daily"`
}
type DailyMetrics struct {
	Day           string `json:"day" example:"2026-09-26"`
	Wagered       string `json:"wagered" example:"100000.00"`
	Payouts       string `json:"payouts" example:"94000.00"`
	GGR           string `json:"ggr" example:"6000.00"`
	RTPPercent    string `json:"rtp_percent" example:"94.00"`
	Deposits      string `json:"deposits"`
	Withdrawals   string `json:"withdrawals"`
	BetCount      int64  `json:"bet_count"`
	ActivePlayers int64  `json:"active_players"`
}
type OverviewResponse struct {
	TotalUsers          int64  `json:"total_users"`
	ActiveUsers         int64  `json:"active_users"`
	TotalPlayerBalances string `json:"total_player_balances" example:"450000.00"`
	TotalWagered        string `json:"total_wagered"`
	TotalPayouts        string `json:"total_payouts"`
	GGR                 string `json:"ggr"`
	RTPPercent          string `json:"rtp_percent"`
	HouseMarginPercent  string `json:"house_margin_percent"`
	Deposits            string `json:"deposits"`
	Withdrawals         string `json:"withdrawals"`
	BetCount            int64  `json:"bet_count"`
	ActivePlayers       int64  `json:"active_players"`
	ActiveBets          int64  `json:"active_bets"`
	PendingDeposits     int64  `json:"pending_deposits"`
	PendingWithdrawals  int64  `json:"pending_withdrawals"`
}
type ErrorResponse struct {
	Error string `json:"error"`
}
