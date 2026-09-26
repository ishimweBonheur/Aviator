package admin

import (
	"context"
	"net/url"
)

// Settled rounds define realized gaming revenue. Cancelled bets and open-round
// stakes are excluded; deposits/withdrawals use their completion timestamps.
func (repo *Repository) analytics(ctx context.Context, q url.Values) (map[string]any, error) {
	from, to, err := dateRange(q)
	if err != nil {
		return nil, err
	}
	rows, err := repo.objects(ctx, `WITH gaming AS (
 SELECT (r.ended_at AT TIME ZONE 'UTC')::date AS day,sum(b.amount) wagered,sum(b.payout) payouts,count(*) bet_count,count(DISTINCT b.user_id) active_players
 FROM bets b JOIN game_rounds r ON r.id=b.round_id
 WHERE r.status='SETTLED' AND b.status IN ('LOST','CASHED_OUT') AND ($1::timestamptz IS NULL OR r.ended_at >= $1) AND ($2::timestamptz IS NULL OR r.ended_at < $2) GROUP BY 1
 ), payments AS (
 SELECT (completed_at AT TIME ZONE 'UTC')::date AS day,amount,kind FROM (
 SELECT completed_at,amount,'deposit' kind FROM deposits WHERE status='COMPLETED'
 UNION ALL SELECT completed_at,amount,'withdrawal' kind FROM withdrawals WHERE status='COMPLETED') p
 WHERE ($1::timestamptz IS NULL OR completed_at >= $1) AND ($2::timestamptz IS NULL OR completed_at < $2)
 ), days AS (SELECT day FROM gaming UNION SELECT day FROM payments), daily AS (
 SELECT d.day,COALESCE(g.wagered,0) wagered,COALESCE(g.payouts,0) payouts,COALESCE(g.bet_count,0) bet_count,COALESCE(g.active_players,0) active_players,
 COALESCE((SELECT sum(amount) FROM payments p WHERE p.day=d.day AND kind='deposit'),0) deposits,
 COALESCE((SELECT sum(amount) FROM payments p WHERE p.day=d.day AND kind='withdrawal'),0) withdrawals FROM days d LEFT JOIN gaming g USING(day)
 ), totals AS (SELECT COALESCE(sum(wagered),0) wagered,COALESCE(sum(payouts),0) payouts,COALESCE(sum(deposits),0) deposits,COALESCE(sum(withdrawals),0) withdrawals,COALESCE(sum(bet_count),0)::bigint bet_count FROM daily)
 SELECT json_build_object(
 'total_wagered',wagered::text,'total_payouts',payouts::text,'ggr',(wagered-payouts)::text,
 'rtp_percent',COALESCE(round(payouts/NULLIF(wagered,0)*100,2),0)::text,
 'house_margin_percent',COALESCE(round((wagered-payouts)/NULLIF(wagered,0)*100,2),0)::text,
 'deposits',deposits::text,'withdrawals',withdrawals::text,'bet_count',bet_count,
 'active_players',(SELECT count(DISTINCT b.user_id) FROM bets b JOIN game_rounds r ON r.id=b.round_id WHERE r.status='SETTLED' AND b.status IN ('LOST','CASHED_OUT') AND ($1::timestamptz IS NULL OR r.ended_at >= $1) AND ($2::timestamptz IS NULL OR r.ended_at < $2)),
 'daily',COALESCE((SELECT json_agg(x ORDER BY x.day) FROM (SELECT day,wagered::text,payouts::text,(wagered-payouts)::text ggr,COALESCE(round(payouts/NULLIF(wagered,0)*100,2),0)::text rtp_percent,deposits::text,withdrawals::text,bet_count,active_players FROM daily) x),'[]'::json)) FROM totals`, from, to)
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

func (repo *Repository) overview(ctx context.Context) (map[string]any, error) {
	result, err := repo.analytics(ctx, url.Values{})
	if err != nil {
		return nil, err
	}
	delete(result, "daily")
	rows, err := repo.objects(ctx, `SELECT json_build_object('total_users',(SELECT count(*) FROM users),'active_users',(SELECT count(*) FROM users WHERE status='ACTIVE'),'total_player_balances',(SELECT COALESCE(sum(balance),0)::text FROM users),'active_bets',(SELECT count(*) FROM bets WHERE status='ACTIVE'),'pending_deposits',(SELECT count(*) FROM deposits WHERE status IN ('PENDING','PROCESSING')),'pending_withdrawals',(SELECT count(*) FROM withdrawals WHERE status IN ('PENDING','PROCESSING')))`)
	if err != nil {
		return nil, err
	}
	for k, v := range rows[0] {
		result[k] = v
	}
	return result, nil
}
