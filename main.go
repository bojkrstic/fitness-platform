package main

import (
	"context"
	"log"
	"net/http"
	"os"
)

func main() {
	ctx := context.Background()

	if err := loadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := openDatabase(ctx, dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	store := NewPostgresStore(db)
	app := NewApp(
		store,
		envOrDefault("SEED_ADMIN_EMAIL", "admin@fitness.local"),
		envOrDefault("SEED_ADMIN_PASSWORD", "Admin123!"),
	)

	if err := app.Bootstrap(ctx); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	port := envOrDefault("PORT", "8080")
	server := &http.Server{
		Addr:    ":" + port,
		Handler: app.Routes(),
	}

	log.Printf("Server running on http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
