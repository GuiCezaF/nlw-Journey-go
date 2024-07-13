package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GuiCezaF/nlw-Journey-go/internal/api"
	"github.com/GuiCezaF/nlw-Journey-go/internal/api/spec"
	"github.com/GuiCezaF/nlw-Journey-go/internal/mailer/mailpit"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/phenpessoa/gutils/netutils/httputils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Println("\ngoodbye :)")
}

func run(ctx context.Context) error {
	cnf := zap.NewDevelopmentConfig()
	cnf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	logger, err := cnf.Build()

	if err != nil {
		return err
	}

	logger = logger.Named("Journey_app")
	defer func() { _ = logger.Sync() }()

	envErr := godotenv.Load(".env")
	if envErr != nil {
		log.Fatalf("Error loading .env file: %s", envErr)
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf("user=%s password=%s host=%s port=%s dbname=%s",
		os.Getenv("JOURNEY_DATABASE_USER"),
		os.Getenv("JOURNEY_DATABASE_PASSWORD"),
		os.Getenv("JOURNEY_DATABASE_HOST"),
		os.Getenv("JOURNEY_DATABASE_PORT"),
		os.Getenv("JOURNEY_DATABASE_NAME"),
	))
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return err
	}

	si := api.NewApi(pool, logger, mailpit.NewMailPit(pool))
	r := chi.NewMux()
	r.Use(middleware.RequestID, middleware.Recoverer, httputils.ChiLogger(logger))
	r.Mount("/", spec.Handler(&si))

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	defer func() {
		const timeout = 30 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown server: ", zap.Error(err))
		}
	}()

	errChan := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}
