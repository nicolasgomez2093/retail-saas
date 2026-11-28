package httpapi

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/storefront"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

func registerStorefront(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := storefront.New(db)
	router.GET("/storefront", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Admin(c.Request.Context(), actor)
		respondStorefront(c, http.StatusOK, result, err)
	}))
	router.PUT("/storefront", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input storefront.Settings
		if readAdminJSON(c, &input) {
			result, err := svc.Save(c.Request.Context(), actor, input)
			respondStorefront(c, http.StatusOK, result, err)
		}
	}))
	router.PUT("/storefront/products/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			Hidden *bool `json:"hidden"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		if input.Hidden == nil {
			respondStorefront(c, 0, nil, storefront.ErrInvalid)
			return
		}
		err := svc.SetHidden(c.Request.Context(), actor, c.Param("id"), *input.Hidden)
		respondStorefront(c, http.StatusNoContent, nil, err)
	}))

	// Public storefront: no session, only what a shopper may see, limited per client address.
	limits := newClientLimiter(rate.Every(100*time.Millisecond), 60)
	public := router.Group("/public/storefronts/:slug", func(c *gin.Context) {
		if !limits.Allow(c.ClientIP()) {
			c.Header("Retry-After", "2")
			fail(c, 429, "rate_limited", "Demasiadas consultas. Esperá unos segundos.")
			return
		}
		c.Next()
	})
	public.GET("", func(c *gin.Context) {
		result, err := svc.Public(c.Request.Context(), c.Param("slug"))
		respondStorefront(c, http.StatusOK, result, err)
	})
	public.GET("/products/:id", func(c *gin.Context) {
		result, err := svc.PublicProduct(c.Request.Context(), c.Param("slug"), c.Param("id"))
		respondStorefront(c, http.StatusOK, result, err)
	})
	public.GET("/images/:imageId", func(c *gin.Context) {
		side := storefront.SizeCard
		switch c.Query("size") {
		case "", "card":
		case "detail":
			side = storefront.SizeDetail
		default:
			respondStorefront(c, 0, nil, storefront.ErrNotFound)
			return
		}
		body, err := svc.PublicImage(c.Request.Context(), c.Param("slug"), c.Param("imageId"), side)
		if err != nil {
			respondStorefront(c, 0, nil, err)
			return
		}
		// A photo never changes under the same id; hiding the product stops new requests.
		c.Header("Cache-Control", "public, max-age=3600")
		c.Data(http.StatusOK, "image/jpeg", body)
	})
}

func respondStorefront(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil && status == http.StatusNoContent:
		c.Status(status)
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, storefront.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, storefront.ErrIncomplete):
		fail(c, 400, "storefront_incomplete", err.Error())
	case errors.Is(err, storefront.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	default:
		respondError(c, err)
	}
}

// clientLimiter gives each client address its own token bucket and forgets idle addresses.
type clientLimiter struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	clients map[string]*clientBucket
	swept   time.Time
}

type clientBucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newClientLimiter(every rate.Limit, burst int) *clientLimiter {
	return &clientLimiter{every: every, burst: burst, clients: map[string]*clientBucket{}, swept: time.Now()}
}

func (l *clientLimiter) Allow(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.swept) > time.Minute || len(l.clients) > 50000 {
		for key, bucket := range l.clients {
			if now.Sub(bucket.seen) > 10*time.Minute {
				delete(l.clients, key)
			}
		}
		l.swept = now
	}
	bucket := l.clients[address]
	if bucket == nil {
		bucket = &clientBucket{limiter: rate.NewLimiter(l.every, l.burst)}
		l.clients[address] = bucket
	}
	bucket.seen = now
	return bucket.limiter.Allow()
}
