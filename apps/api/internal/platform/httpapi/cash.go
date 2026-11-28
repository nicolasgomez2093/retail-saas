package httpapi

import (
	"errors"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerCash(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := cash.New(db)
	router.GET("/cash-registers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Registers(c.Request.Context(), actor, c.Query("store_id"))
		respondCash(c, http.StatusOK, result, err)
	}))
	router.POST("/cash-registers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input cash.RegisterInput
		if readAdminJSON(c, &input) {
			result, err := svc.CreateRegister(c.Request.Context(), actor, input)
			respondCash(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/cash-sessions", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Sessions(c.Request.Context(), actor, c.Query("store_id"), c.Query("register_id"))
		respondCash(c, http.StatusOK, gin.H{"items": result}, err)
	}))
	router.GET("/cash-sessions/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Session(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondCash(c, http.StatusOK, result, err)
	}))
	router.POST("/cash-sessions", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input cash.OpenInput
		if readAdminJSON(c, &input) {
			result, err := svc.Open(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondCash(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/cash-sessions/:id/summary", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Summary(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondCash(c, http.StatusOK, result, err)
	}))
	router.GET("/cash-sessions/:id/movements", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Movements(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondCash(c, http.StatusOK, result, err)
	}))
	router.POST("/cash-sessions/:id/movements", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input cash.MovementInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddMovement(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondCash(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/cash-sessions/:id/close", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input cash.CloseInput
		if readAdminJSON(c, &input) {
			result, err := svc.Close(c.Request.Context(), actor, c.Param("id"), input)
			respondCash(c, http.StatusOK, result, err)
		}
	}))
}

func respondCash(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, cash.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, cash.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, cash.ErrRegisterCode):
		fail(c, 409, "cash_register_code_taken", err.Error())
	case errors.Is(err, cash.ErrOccupied):
		fail(c, 409, "cash_register_occupied", err.Error())
	case errors.Is(err, cash.ErrSessionState):
		fail(c, 409, "cash_session_state_conflict", err.Error())
	case errors.Is(err, cash.ErrIdempotency):
		fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, cash.ErrCashBalance):
		fail(c, 409, "cash_balance_conflict", err.Error())
	default:
		respondError(c, err)
	}
}
