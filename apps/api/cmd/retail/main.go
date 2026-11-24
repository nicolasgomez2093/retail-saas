package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/config"
	"retail-platform/api/internal/platform/httpapi"
	"retail-platform/api/migrations"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("falló el comando", "detalle", err.Error())
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || (len(os.Args) != 2 && os.Args[1] != "provision-tenant") {
		return errors.New("uso: retail serve|migrate|seed-demo|provision-tenant|healthcheck")
	}
	if os.Args[1] == "provision-tenant" {
		return runProvision(os.Args[2:])
	}
	if os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://127.0.0.1:8080/api/health/ready")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("readiness: %s", response.Status)
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("DATABASE_URL inválida")
	}
	defer db.Close()
	switch os.Args[1] {
	case "migrate":
		return migrations.Apply(ctx, db)
	case "seed-demo":
		return seedDemo(ctx, cfg, db)
	case "serve":
		gin.SetMode(gin.ReleaseMode)
		service, err := auth.New(db)
		if err != nil {
			return err
		}
		server := &http.Server{Addr: cfg.Addr, Handler: httpapi.New(cfg, db, service),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		slog.Info("API iniciada", "addr", cfg.Addr)
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		}
	default:
		return errors.New("comando desconocido")
	}
}

func seedDemo(ctx context.Context, cfg config.Config, db *pgxpool.Pool) error {
	if cfg.Secure {
		return errors.New("seed-demo está disponible en desarrollo con COOKIE_SECURE=false")
	}
	_, err := tenancy.Provision(ctx, db, tenancy.Input{
		Slug: "demo", Name: "Comercio demo", Timezone: "America/Argentina/Tucuman", Currency: "ARS",
		StoreCode: "CENTRAL", StoreName: "Sucursal central",
		OwnerEmail: "demo@example.test", OwnerName: "Usuario demo", Password: os.Getenv("RETAIL_SEED_PASSWORD"),
	})
	return err
}
