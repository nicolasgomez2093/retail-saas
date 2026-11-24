package httpapi

import (
	"errors"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/stores"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerStores(router *gin.RouterGroup, db *pgxpool.Pool, service *auth.Service) {
	router.GET("/stores/admin", authenticated(service, func(c *gin.Context, actor auth.Identity) {
		items, err := stores.ListManaged(c.Request.Context(), db, actor)
		if err != nil {
			respondStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}))
	router.POST("/stores", authenticated(service, func(c *gin.Context, actor auth.Identity) {
		var input stores.StoreInput
		if !readAdminJSON(c, &input) {
			return
		}
		store, err := stores.Create(c.Request.Context(), db, actor, input)
		if err != nil {
			respondStoreError(c, err)
			return
		}
		c.JSON(http.StatusCreated, store)
	}))
	router.PATCH("/stores/:id", authenticated(service, func(c *gin.Context, actor auth.Identity) {
		var input stores.StoreChanges
		if !readAdminJSON(c, &input) {
			return
		}
		store, err := stores.Update(c.Request.Context(), db, actor, c.Param("id"), input)
		if err != nil {
			respondStoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, store)
	}))
}

func respondStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, stores.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, stores.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, stores.ErrCodeTaken):
		fail(c, 409, "store_code_taken", err.Error())
	default:
		respondError(c, err)
	}
}
