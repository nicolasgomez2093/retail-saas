package httpapi

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/stores"
	"retail-platform/api/internal/platform/config"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

const sessionCookie = "retail_session"

func New(cfg config.Config, db *pgxpool.Pool, service *auth.Service) *gin.Engine {
	router := gin.New()
	router.SetTrustedProxies(nil)
	router.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		slog.Error("pánico al procesar solicitud", "route", c.FullPath())
		fail(c, 500, "internal_error", "Error interno")
	}))
	router.NoRoute(func(c *gin.Context) { fail(c, 404, "not_found", "Ruta inexistente") })
	api := router.Group("/api")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPatch || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			origin := c.GetHeader("Origin")
			if origin != "" && origin != cfg.WebOrigin {
				fail(c, 403, "origin_forbidden", "Origen no permitido")
				return
			}
		}
		c.Next()
	})
	api.GET("/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	api.GET("/health/ready", func(c *gin.Context) {
		if err := db.Ping(c.Request.Context()); err != nil {
			fail(c, 503, "unavailable", "Base de datos no disponible")
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	version := api.Group("/v1")
	limiter := rate.NewLimiter(rate.Every(250*time.Millisecond), 8)
	version.POST("/auth/login", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
		var input struct {
			Commerce string `json:"commerce" binding:"max=80"`
			Email    string `json:"email" binding:"required,max=254"`
			Password string `json:"password" binding:"required"`
		}
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if contentType != "application/json" || c.ShouldBindJSON(&input) != nil || len(input.Password) > 1024 {
			fail(c, 400, "invalid_request", "Ingresá un correo y una contraseña válidos")
			return
		}
		email, valid := auth.NormalizeEmail(input.Email)
		if !valid {
			fail(c, 400, "invalid_request", "Ingresá un correo válido")
			return
		}
		if !limiter.Allow() {
			respondError(c, auth.ErrBusy)
			return
		}
		token, choice, err := service.Login(c.Request.Context(), strings.ToLower(strings.TrimSpace(input.Commerce)),
			email, input.Password)
		if err != nil {
			respondError(c, err)
			return
		}
		if choice != nil {
			c.JSON(http.StatusOK, choice)
			return
		}
		setCookie(c, cfg, token, int(auth.SessionTTL.Seconds()))
		c.Status(204)
	})
	version.POST("/auth/choose-commerce", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
		var input struct {
			Token      string `json:"token" binding:"required,len=43"`
			CommerceID string `json:"commerceId" binding:"required,uuid"`
		}
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if contentType != "application/json" || c.ShouldBindJSON(&input) != nil {
			fail(c, 400, "invalid_request", "Elegí un comercio válido")
			return
		}
		token, err := service.SelectCommerce(c.Request.Context(), input.Token, input.CommerceID)
		if err != nil {
			respondError(c, err)
			return
		}
		setCookie(c, cfg, token, int(auth.SessionTTL.Seconds()))
		c.Status(204)
	})
	version.POST("/auth/logout", func(c *gin.Context) {
		token, _ := c.Cookie(sessionCookie)
		if err := service.Logout(c.Request.Context(), token); err != nil {
			respondError(c, err)
			return
		}
		setCookie(c, cfg, "", -1)
		c.Status(204)
	})
	version.GET("/me", authenticated(service, func(c *gin.Context, identity auth.Identity) {
		c.JSON(200, identity)
	}))
	version.GET("/stores", authenticated(service, func(c *gin.Context, identity auth.Identity) {
		result, err := stores.List(c.Request.Context(), db, identity)
		if err != nil {
			respondError(c, err)
			return
		}
		c.JSON(200, gin.H{"items": result})
	}))
	registerUsers(version, db, service)
	registerStores(version, db, service)
	registerCatalog(version, db, service)
	registerPricing(version, db, service)
	registerInventory(version, db, service)
	registerCash(version, db, service)
	registerCustomers(version, db, service)
	registerSuppliers(version, db, service)
	registerPurchases(version, db, service)
	registerReservations(version, db, service)
	registerSales(version, db, service)
	return router
}

func authenticated(service *auth.Service, handler func(*gin.Context, auth.Identity)) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _ := c.Cookie(sessionCookie)
		identity, err := service.Identity(c.Request.Context(), token)
		if err != nil {
			respondError(c, err)
			return
		}
		handler(c, identity)
	}
}

func setCookie(c *gin.Context, cfg config.Config, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/api/v1", MaxAge: maxAge,
		HttpOnly: true, Secure: cfg.Secure, SameSite: http.SameSiteStrictMode,
	})
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		fail(c, 401, "unauthorized", err.Error())
	case errors.Is(err, auth.ErrForbidden):
		fail(c, 403, "forbidden", err.Error())
	case errors.Is(err, auth.ErrBusy):
		c.Header("Retry-After", "1")
		fail(c, 429, "rate_limited", err.Error())
	default:
		slog.Error("falló la solicitud", "route", c.FullPath(), "error_type", fmt.Sprintf("%T", err))
		fail(c, 500, "internal_error", "Error interno")
	}
}

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
