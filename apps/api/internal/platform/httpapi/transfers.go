package httpapi

import (
	"context"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"

	"github.com/gin-gonic/gin"
)

func registerTransfers(router *gin.RouterGroup, svc inventory.Service, authService *auth.Service) {
	router.GET("/transfers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Transfers(c.Request.Context(), actor, c.Query("store_id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.GET("/transfers/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Transfer(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.POST("/transfers", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.NewTransfer
		if readAdminJSON(c, &input) {
			result, err := svc.CreateTransfer(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondInventory(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/transfers/:id/dispatch", transferStoreAction(authService, svc.DispatchTransfer))
	router.POST("/transfers/:id/cancel", transferStoreAction(authService, svc.CancelTransfer))
	router.POST("/transfers/:id/receive", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.TransferReceipt
		if readAdminJSON(c, &input) {
			result, err := svc.ReceiveTransfer(c.Request.Context(), actor, c.Param("id"), input)
			respondInventory(c, http.StatusOK, result, err)
		}
	}))
}

func transferStoreAction(authService *auth.Service, action func(context.Context, auth.Identity, string, string) (inventory.Transfer, error)) gin.HandlerFunc {
	return authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			StoreID string `json:"storeId"`
		}
		if readAdminJSON(c, &input) {
			result, err := action(c.Request.Context(), actor, input.StoreID, c.Param("id"))
			respondInventory(c, http.StatusOK, result, err)
		}
	})
}
