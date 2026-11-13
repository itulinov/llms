package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) > 1 {
		if err := runCLI(os.Args[1:], os.Stdin, os.Stdout); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	// pgxpool читает PGHOST, PGPORT, PGUSER,
	// PGPASSWORD, PGDATABASE, PGSSLMODE.
	config, err := loadAuthConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, "")
	if err != nil {
		log.Fatal("Failed to configure PostgreSQL connection")
	}
	defer pool.Close()

	mux := http.NewServeMux()
	dummy, err := hashPassword("dummy-password-for-auth")
	if err != nil {
		log.Fatal(err)
	}
	auth := &authService{store: pgAuthStore{pool}, config: config, limiter: newLimiter(), dummy: dummy}
	auth.register(mux)
	registerEmployees(mux, auth, pool)
	registerStructure(mux, auth, pool)

	// Проверка API и PostgreSQL
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if err := pool.Ping(checkCtx); err != nil {
			log.Println("Database health check failed")

			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":   "error",
				"database": "unavailable",
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"database": "connected",
		})
	})

	// Информация о backend
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"service":  "corporate-lms-api",
			"language": "go",
		})
	})

	log.Println("Go API listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", auth.csrf(mux)))
}
