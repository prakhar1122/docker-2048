// Build marker — Sprint 0 credential remediation, 2026-07-31.
//
// Bumped to force a real CI build. Earlier smoke runs redeployed an unchanged
// commit, so CI resolved the existing image and skipped clone/build/push
// entirely, leaving the builder VM's env-sourced credentials unexercised.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client
var pdb *sql.DB

// pingWithRetry probes a dependency until it answers, instead of once at startup.
//
// The container is running before its Istio sidecar has finished programming
// outbound routing, and a connection opened in that window is reset. Observed on
// a cold start: Postgres failed with "tls error: read: connection reset by peer"
// while the same pod's later /health calls reported it healthy, no restart in
// between. A single probe therefore logs a failure for a dependency that is
// reachable seconds later — which makes the startup log actively misleading.
func pingWithRetry(name string, ping func(context.Context) error) error {
	const attempts = 6

	var err error
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = ping(ctx)
		cancel()

		if err == nil {
			return nil
		}
		if i < attempts {
			fmt.Printf("%s: not ready (attempt %d/%d), retrying: %v\n", name, i, attempts, err)
			time.Sleep(2 * time.Second)
		}
	}
	return err
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
  
	gameURL := os.Getenv("GAME_URL")

	fmt.Printf("TEST_HAHA=%q\n", os.Getenv("TEST_HAHA"))

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			fmt.Printf("redis: invalid REDIS_URL: %v\n", err)
		} else {
			rdb = redis.NewClient(opts)
			if err := pingWithRetry("redis", func(ctx context.Context) error {
				return rdb.Ping(ctx).Err()
			}); err != nil {
				fmt.Printf("redis: ping failed: %v\n", err)
			} else {
				fmt.Println("redis: connected")
			}
		}
	} else {
		fmt.Println("redis: REDIS_URL not set, skipping connection")
	}

	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		db, err := sql.Open("pgx", dbURL)
		if err != nil {
			fmt.Printf("postgres: invalid DATABASE_URL: %v\n", err)
		} else {
			pdb = db
			if err := pingWithRetry("postgres", pdb.PingContext); err != nil {
				fmt.Printf("postgres: ping failed: %v\n", err)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var version string
				if err := pdb.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
					fmt.Printf("postgres: connected, but version query failed: %v\n", err)
				} else {
					fmt.Printf("postgres: connected — %s\n", version)
				}
			}
		}
	} else {
		fmt.Println("postgres: DATABASE_URL not set, skipping connection")
	}

	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"service":  "api",
			"status":   "pong",
			"time":     time.Now().Format(time.RFC3339),
			"game_url": gameURL,
		})
	})

	http.HandleFunc("/roundtrip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		result := map[string]any{
			"service":  "api",
			"step":     "api received roundtrip request",
			"game_url": gameURL,
		}

		if gameURL == "" {
			result["error"] = "GAME_URL not set — no connection env var configured"
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(result)
			return
		}

		resp, err := http.Get(gameURL)
		if err != nil {
			result["error"] = fmt.Sprintf("failed to reach game service: %v", err)
			result["game_reachable"] = false
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(result)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		result["game_reachable"] = true
		result["game_status"] = resp.StatusCode
		result["game_response_size"] = len(body)
		result["roundtrip"] = "success"

		json.NewEncoder(w).Encode(result)
	})

	http.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var ev struct {
			Type  string `json:"type"`
			Score int    `json:"score"`
			Ts    int64  `json:"ts"`
		}
		if err := json.Unmarshal(body, &ev); err != nil || ev.Type == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Printf("event received: type=%s score=%d ts=%d\n", ev.Type, ev.Score, ev.Ts)
		if rdb != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			rdb.Incr(ctx, "events:count:"+ev.Type)
			rdb.Set(ctx, "events:last:"+ev.Type, string(body), 0)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if rdb == nil {
			json.NewEncoder(w).Encode(map[string]any{"redis": "not_configured"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		stats := map[string]any{}
		for _, t := range []string{"score_update", "game_start", "game_over"} {
			count, _ := rdb.Get(ctx, "events:count:"+t).Int64()
			last, _ := rdb.Get(ctx, "events:last:"+t).Result()
			stats[t] = map[string]any{"count": count, "last": last}
		}
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		result := map[string]any{"status": "healthy"}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		degraded := false

		if rdb == nil {
			result["redis"] = "not_configured"
		} else {
			if err := rdb.Ping(ctx).Err(); err != nil {
				result["redis"] = "unreachable"
				result["redis_error"] = err.Error()
				degraded = true
			} else {
				result["redis"] = "ok"
			}
		}

		if pdb == nil {
			result["postgres"] = "not_configured"
		} else {
			if err := pdb.PingContext(ctx); err != nil {
				result["postgres"] = "unreachable"
				result["postgres_error"] = err.Error()
				degraded = true
			} else {
				result["postgres"] = "ok"
			}
		}

		// Written after both probes so a Postgres failure cannot be masked by an
		// earlier WriteHeader for Redis — the header may only be sent once.
		if degraded {
			result["status"] = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(result)
	})

	fmt.Printf("api service starting on :%s (GAME_URL=%s)\n", port, gameURL)
	http.ListenAndServe(":"+port, nil)
}
