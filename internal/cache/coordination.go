package cache

import (
	"aviator/backend/internal/httpapi"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"time"
)

// SuppressDuplicates is an optimization only. On Redis failure PostgreSQL
// still enforces ownership, locked state transitions and unique ledger entries.
func (s *Store) SuppressDuplicates(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			httpapi.Error(w, "request unavailable", 500)
			return
		}
		token := hex.EncodeToString(bytes)
		key := "aviator:bet-action:" + r.PathValue("id")
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
		ok, err := s.client.SetNX(ctx, key, token, 5*time.Second).Result()
		cancel()
		if err != nil {
			log.Printf("redis action coordination unavailable; using PostgreSQL: %v", err)
		}
		if err == nil && !ok {
			httpapi.Error(w, "bet action is already processing", 409)
			return
		}
		if ok {
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				_ = s.client.Eval(ctx, `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`, []string{key}, token).Err()
			}()
		}
		next.ServeHTTP(w, r)
	})
}
