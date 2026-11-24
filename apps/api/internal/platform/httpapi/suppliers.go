package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/suppliers"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerSuppliers(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	service := suppliers.New(db)
	router.GET("/suppliers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			respondSupplier(c, 0, nil, suppliers.ErrInvalid)
			return
		}
		result, err := service.List(c.Request.Context(), actor, c.Query("q"), limit, offset)
		respondSupplier(c, http.StatusOK, result, err)
	}))
	router.POST("/suppliers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input suppliers.Input
		if readAdminJSON(c, &input) {
			result, err := service.Create(c.Request.Context(), actor, input)
			respondSupplier(c, http.StatusCreated, result, err)
		}
	}))
	router.PATCH("/suppliers/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input suppliers.Changes
		if readAdminJSON(c, &input) {
			result, err := service.Update(c.Request.Context(), actor, c.Param("id"), input)
			respondSupplier(c, http.StatusOK, result, err)
		}
	}))
}

func respondSupplier(c *gin.Context, status int, result any, err error) {
	var duplicate suppliers.ErrDuplicate
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.As(err, &duplicate):
		c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "supplier_duplicate", "message": duplicate.Error(),
			"field": duplicate.Field, "supplier": gin.H{"id": duplicate.ID, "name": duplicate.Name}}})
	case errors.Is(err, suppliers.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, suppliers.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	default:
		respondError(c, err)
	}
}
