package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time" 
)
      
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
 
	gameURL := os.Getenv("GAME_URL") // Connection env var → points to game service

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

		// Call the game service back to verify bidirectional connectivity
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
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	fmt.Printf("api service starting on :%s (GAME_URL=%s)\n", port, gameURL)
	http.ListenAndServe(":"+port, nil)
}

