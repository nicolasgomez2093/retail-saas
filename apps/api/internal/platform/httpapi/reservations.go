package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/modules/reservations"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerReservations(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := reservations.New(db)
	router.GET("/reservations", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", reservations.ErrInvalid.Error())
			return
		}
		result, err := svc.List(c.Request.Context(), actor, c.Query("store_id"), c.DefaultQuery("status", "ACTIVE"), limit, offset)
		respondReservation(c, http.StatusOK, result, err)
	}))
	router.POST("/reservations", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input reservations.Input
		if readAdminJSON(c, &input) {
			result, err := svc.Create(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondReservation(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/reservations/:id/deposit", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input reservations.DepositInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddDeposit(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondReservation(c, http.StatusCreated, result, err)
		}
	}))
	release := func(status string) gin.HandlerFunc {
		return authenticated(authService, func(c *gin.Context, actor auth.Identity) {
			var input struct {
				StoreID string `json:"storeId"`
			}
			if readAdminJSON(c, &input) {
				result, err := svc.Release(c.Request.Context(), actor, input.StoreID, c.Param("id"), status)
				respondReservation(c, http.StatusOK, result, err)
			}
		})
	}
	router.POST("/reservations/:id/cancel", release("CANCELLED"))
	router.POST("/reservations/:id/expire", release("EXPIRED"))
}

func respondReservation(c *gin.Context, status int, result any, err error) {
	switch {
	case errors.Is(err, reservations.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, reservations.ErrDepositInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, reservations.ErrDepositExists):
		fail(c, 409, "reservation_deposit_exists", err.Error())
	case errors.Is(err, reservations.ErrDepositChanged):
		fail(c, 409, "reservation_deposit_changed", err.Error())
	case errors.Is(err, reservations.ErrDepositExceedsTotal):
		fail(c, 409, "reservation_deposit_exceeds_total", err.Error())
	case errors.Is(err, reservations.ErrNotFound):
		fail(c, 404, "reservation_not_found", err.Error())
	case errors.Is(err, reservations.ErrState):
		fail(c, 409, "reservation_state", err.Error())
	case errors.Is(err, reservations.ErrNotExpired):
		fail(c, 409, "reservation_not_expired", err.Error())
	case errors.Is(err, reservations.ErrMismatch):
		fail(c, 409, "reservation_mismatch", err.Error())
	case errors.Is(err, reservations.ErrKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, inventory.ErrBalance):
		fail(c, 409, "reservation_stock_insufficient", "stock disponible insuficiente para reservar")
	default:
		respondCash(c, status, result, err)
	}
}
