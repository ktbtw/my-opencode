package main

import (
	"log"
	"net/http"
	"os"

	"relay-server/internal/app"
)

func main() {
	addr := env("ADDR", ":8080")
	srv := &http.Server{
		Addr:    addr,
		Handler: app.New().Router(),
	}

	log.Printf("relay server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
