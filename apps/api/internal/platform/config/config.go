package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string
	Addr        string
	WebOrigin   string
	Secure      bool
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Addr:        env("HTTP_ADDR", ":8080"),
		WebOrigin:   env("WEB_ORIGIN", "http://localhost:5173"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL es obligatoria")
	}
	secure, err := strconv.ParseBool(env("COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("COOKIE_SECURE debe ser booleano")
	}
	origin, err := url.Parse(cfg.WebOrigin)
	if err != nil || origin.Hostname() == "" || origin.User != nil || cfg.WebOrigin != origin.Scheme+"://"+origin.Host ||
		!(origin.Scheme == "https" && secure ||
			origin.Scheme == "http" && !secure && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1")) {
		return Config{}, fmt.Errorf("WEB_ORIGIN debe ser un origen HTTPS o HTTP local con COOKIE_SECURE=false")
	}
	cfg.Secure = secure
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
