package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"gridwise/internal/routes"
)

func NewHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	routes.Register(mux)

	return recoverMiddleware(mux)
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var bodyCopy []byte
		if r.Body != nil {
			bodyCopy, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(bodyCopy))
		}
		defer func() {
			if rec := recover(); rec != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"scenario_id": scenarioIDFromBody(bodyCopy),
					"error":       "Internal server processing failure.",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func scenarioIDFromBody(body []byte) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "UNKNOWN"
	}
	if v, ok := m["scenario_id"].(string); ok && v != "" {
		return v
	}
	return "UNKNOWN"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
