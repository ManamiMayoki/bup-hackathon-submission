package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gridwise/internal/app"
)

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i >= 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])

			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') ||
					(val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, val)
			}
		}
	}
}

func main() {

	loadDotEnv(".env")
	if _, exists := os.LookupEnv("OPENROUTER_API_KEY"); !exists {
		if exe, err := os.Executable(); err == nil {
			loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
		}
	}

	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		fmt.Fprintln(os.Stderr, "warning: OPENROUTER_API_KEY not configured. Using deterministic fallback.")
	} else {
		fmt.Printf("OpenRouter API key loaded (%d chars).\n", len(key))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	handler := app.NewHandler()

	fmt.Printf("Running securely on port %s\n", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, handler); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
