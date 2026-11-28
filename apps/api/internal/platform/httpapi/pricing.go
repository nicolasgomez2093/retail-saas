package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/pricing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerPricing(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := pricing.New(db)
	router.GET("/price-lists", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Configuration(c.Request.Context(), actor)
		respondPricing(c, http.StatusOK, result, err)
	}))
	router.POST("/price-lists", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			Name string `json:"name"`
		}
		if readAdminJSON(c, &input) {
			result, err := svc.Create(c.Request.Context(), actor, input.Name)
			respondPricing(c, http.StatusCreated, result, err)
		}
	}))
	router.PUT("/stores/:id/price-list", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			ListID json.RawMessage `json:"listId"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		var listID *string
		if json.Unmarshal(input.ListID, &listID) != nil {
			respondPricing(c, 0, nil, pricing.ErrInvalid)
			return
		}
		err := svc.Assignment(c.Request.Context(), actor, c.Param("id"), listID)
		respondPricing(c, http.StatusNoContent, nil, err)
	}))
	router.GET("/price-lists/:id/items/:variantId", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.GetItem(c.Request.Context(), actor, c.Param("id"), c.Param("variantId"))
		respondPricing(c, http.StatusOK, result, err)
	}))
	router.PUT("/price-lists/:id/items/:variantId", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			Amount json.RawMessage `json:"amount"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		var amount *string
		if json.Unmarshal(input.Amount, &amount) != nil {
			respondPricing(c, 0, nil, pricing.ErrInvalid)
			return
		}
		result, err := svc.SetItem(c.Request.Context(), actor, c.Param("id"), c.Param("variantId"), amount)
		respondPricing(c, http.StatusOK, result, err)
	}))
	// Bulk change of a list: apply=false only previews; apply=true writes every line in one transaction.
	router.POST("/price-lists/:id/adjustments", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input pricing.AdjustInput
		if readAdminJSON(c, &input) {
			result, err := svc.Adjust(c.Request.Context(), actor, c.Param("id"), input)
			respondPricing(c, http.StatusOK, result, err)
		}
	}))
	router.GET("/stores/:id/variants/:variantId/price", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Resolve(c.Request.Context(), actor, c.Param("id"), c.Param("variantId"))
		respondPricing(c, http.StatusOK, result, err)
	}))
}

func respondPricing(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil && status == http.StatusNoContent:
		c.Status(status)
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, pricing.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, pricing.ErrNegative):
		fail(c, 409, "price_negative", err.Error())
	case errors.Is(err, pricing.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, pricing.ErrNameTaken):
		fail(c, 409, "price_list_name_taken", err.Error())
	default:
		respondError(c, err)
	}
}
