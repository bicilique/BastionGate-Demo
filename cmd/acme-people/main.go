package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bicilique/BastionGate-Demo/internal/app"
)

func main() {
	config := app.Config{
		BastionGateInternalURL: envOrDefault("BASTIONGATE_INTERNAL_URL", "http://host.docker.internal:8080"),
		BastionGatePublicURL:   envOrDefault("BASTIONGATE_PUBLIC_URL", "http://localhost:8080"),
		BastionGateAPIKey:      os.Getenv("BASTIONGATE_API_KEY"),
		PolicyCode:             envOrDefault("BASTIONGATE_POLICY_CODE", "DEFAULT"),
		MaxUploadBytes:         2 << 20,
	}
	handler, err := app.NewHandler(config)
	if err != nil {
		log.Fatal(err)
	}

	listenAddress := envOrDefault("LISTEN_ADDR", ":3000")
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()

	log.Printf("Acme People listening on %s", listenAddress)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-stopped
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
