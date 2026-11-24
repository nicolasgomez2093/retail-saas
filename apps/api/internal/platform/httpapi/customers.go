package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/customers"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerCustomers(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := customers.New(db)
	router.GET("/customers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", customers.ErrInvalid.Error())
			return
		}
		result, err := svc.List(c.Request.Context(), actor, c.Query("store_id"), c.Query("q"), limit, offset, c.Query("summary") == "true")
		respondCustomer(c, http.StatusOK, result, err)
	}))
	router.POST("/customers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input customers.Input
		if readAdminJSON(c, &input) {
			result, err := svc.Create(c.Request.Context(), actor, input)
			respondCustomer(c, http.StatusCreated, result, err)
		}
	}))
	router.PATCH("/customers/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input customers.Input
		if readAdminJSON(c, &input) {
			result, err := svc.Update(c.Request.Context(), actor, c.Param("id"), input)
			respondCustomer(c, http.StatusOK, result, err)
		}
	}))
}

func respondCustomer(c *gin.Context, status int, result any, err error) {
	if errors.Is(err, customers.ErrInvalid) {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if errors.Is(err, customers.ErrNotFound) {
		fail(c, 404, "not_found", err.Error())
		return
	}
	respondCash(c, status, result, err)
}
