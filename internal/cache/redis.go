// Package cache owns Redis connections, ephemeral state, event distribution and
// engine leases. It never stores authoritative balances or financial decisions.
package cache

import (
	"aviator/backend/internal/realtime"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"log"
	"time"
)

const EventsChannel = "aviator:events"
const leaderKey = "aviator:engine:leader"
const leaseTTL = 10 * time.Second

type Store struct{ client *redis.Client }

func New(addr, password string, db int) *Store {
	return &Store{redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: true})}
}
func (s *Store) Close() error { return s.client.Close() }
func (s *Store) Publish(ctx context.Context, event realtime.Event) {
	event.Timestamp = time.Now().UTC()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.client.Publish(ctx, EventsChannel, data).Err(); err != nil {
		log.Printf("redis event publish: %v", err)
	}
}

// Subscribe is the only Redis-to-hub path. The hub never republishes events.
func (s *Store) Subscribe(ctx context.Context, hub *realtime.Hub) {
	sub := s.client.Subscribe(ctx, EventsChannel)
	defer sub.Close()
	go func() { <-ctx.Done(); _ = sub.Close() }()
	for ctx.Err() == nil {
		msg, err := sub.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("redis subscription reconnect: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		var event realtime.Event
		if json.Unmarshal([]byte(msg.Payload), &event) == nil {
			hub.Broadcast(event)
		}
	}
}

// State accepts only public metadata. In particular crash_at would disclose the
// future crash point and must never be included in a player-facing snapshot.
func (s *Store) State(ctx context.Context, key string, state any) {
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.client.Set(ctx, "aviator:round:"+key, data, 30*time.Second).Err(); err != nil {
		log.Printf("redis live state: %v", err)
	}
}

// Lead cancels the game loop on any renewal uncertainty, and waits for it to
// stop before releasing the token. A PostgreSQL guard additionally fences DB
// lifecycle writes across process pauses/Redis restarts (see game leadership).
func (s *Store) Lead(ctx context.Context, run func(context.Context)) {
	for ctx.Err() == nil {
		tokenBytes := make([]byte, 24)
		if _, err := rand.Read(tokenBytes); err != nil {
			log.Printf("leader token: %v", err)
			return
		}
		token := hex.EncodeToString(tokenBytes)
		ok, err := s.client.SetNX(ctx, leaderKey, token, leaseTTL).Result()
		if err != nil {
			log.Printf("redis leadership unavailable: %v", err)
		}
		if ok && err == nil {
			child, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() { defer close(done); run(child) }()
			ticker := time.NewTicker(2 * time.Second)
		lease:
			for {
				select {
				case <-ctx.Done():
					break lease
				case <-done:
					break lease
				case <-ticker.C:
					n, err := s.client.Eval(ctx, `if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('pexpire',KEYS[1],ARGV[2]) else return 0 end`, []string{leaderKey}, token, leaseTTL.Milliseconds()).Int()
					if err != nil || n != 1 {
						log.Printf("engine leadership lost: %v", err)
						break lease
					}
				}
			}
			ticker.Stop()
			cancel()
			<-done
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), time.Second)
			if err := s.client.Eval(releaseCtx, `if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`, []string{leaderKey}, token).Err(); err != nil {
				log.Printf("release engine lease: %v", err)
			}
			releaseCancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
