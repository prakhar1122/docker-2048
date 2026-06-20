package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	gameURL := os.Getenv("GAME_URL")

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			fmt.Printf("redis: invalid REDIS_URL: %v\n", err)
		} else {
			rdb = redis.NewClient(opts)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				fmt.Printf("redis: ping failed: %v\n", err)
			} else {
				fmt.Println("redis: connected")
			}
		}
	} else {
		fmt.Println("redis: REDIS_URL not set, skipping connection")
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

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		result := map[string]any{"status": "healthy"}

		if rdb == nil {
			result["redis"] = "not_configured"
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				result["redis"] = "unreachable"
				result["redis_error"] = err.Error()
				result["status"] = "degraded"
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				result["redis"] = "ok"
			}
		}

		json.NewEncoder(w).Encode(result)
	})

	fmt.Printf("api service starting on :%s (GAME_URL=%s)\n", port, gameURL)
	http.ListenAndServe(":"+port, nil)
}
