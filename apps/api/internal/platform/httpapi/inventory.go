package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerInventory(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := inventory.New(db)
	registerTransfers(router, svc, authService)
	registerCounts(router, svc, authService)
	registerExistences(router, svc, authService)
	registerMovementHistory(router, svc, authService)
	router.GET("/locations", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Locations(c.Request.Context(), actor, c.Query("store_id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.POST("/locations", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.LocationInput
		if readAdminJSON(c, &input) {
			result, err := svc.CreateLocation(c.Request.Context(), actor, input)
			respondInventory(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/inventory", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		flag := c.Query("low_stock")
		if flag != "" && flag != "true" && flag != "false" {
			fail(c, 400, "invalid_request", "low_stock debe ser true o false")
			return
		}
		result, err := svc.Balances(c.Request.Context(), actor, c.Query("store_id"), c.Query("location_id"), flag == "true")
		respondInventory(c, http.StatusOK, gin.H{"items": result}, err)
	}))
	router.GET("/reports/inventory", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Valuation(c.Request.Context(), actor, c.Query("store_id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.GET("/reports/inventory/csv", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var output bytes.Buffer
		if err := svc.ExportValuationCSV(c.Request.Context(), actor, c.Query("store_id"), &output); err != nil {
			respondInventory(c, 0, nil, err)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="valuacion-inventario.csv"`)
		c.Data(http.StatusOK, csvContentType, output.Bytes())
	}))
	router.PUT("/inventory/minimums", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			StoreID         string          `json:"storeId"`
			LocationID      string          `json:"locationId"`
			VariantID       string          `json:"variantId"`
			Minimum         json.RawMessage `json:"minimum"`
			ExpectedVersion *string         `json:"expectedVersion,omitempty"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		var minimum *int32
		if err := json.Unmarshal(input.Minimum, &minimum); err != nil {
			respondInventory(c, 0, nil, inventory.ErrMinimumInvalid)
			return
		}
		err := svc.SetMinimum(c.Request.Context(), actor, inventory.MinimumInput{
			StoreID: input.StoreID, LocationID: input.LocationID, VariantID: input.VariantID, Minimum: minimum, ExpectedVersion: input.ExpectedVersion,
		})
		if err != nil {
			respondInventory(c, 0, nil, err)
			return
		}
		c.Status(http.StatusNoContent)
	}))
	router.GET("/inventory/movements", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Movements(c.Request.Context(), actor, c.Query("store_id"), c.Query("location_id"))
		respondInventory(c, http.StatusOK, gin.H{"items": result}, err)
	}))
	router.GET("/inventory/adjustments/result", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		params, ok := existenceParams(c, "store_id", "key")
		if !ok {
			return
		}
		result, err := svc.AdjustmentResult(c.Request.Context(), actor, params.Get("store_id"), params.Get("key"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.POST("/inventory/adjustments", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.Adjustment
		if readAdminJSON(c, &input) {
			result, err := svc.Adjust(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondInventory(c, http.StatusCreated, result, err)
		}
	}))
}

func respondInventory(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, inventory.ErrInvalid), errors.Is(err, inventory.ErrMinimumInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, inventory.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, inventory.ErrLocationCode):
		fail(c, 409, "location_code_taken", err.Error())
	case errors.Is(err, inventory.ErrAdjustmentStale):
		fail(c, 409, "adjustment_stale", err.Error())
	case errors.Is(err, inventory.ErrAdjustmentInactive):
		fail(c, 409, "adjustment_inactive", err.Error())
	case errors.Is(err, inventory.ErrMinimumStale):
		fail(c, 409, "minimum_stale", err.Error())
	case errors.Is(err, inventory.ErrBalance):
		fail(c, 409, "inventory_balance_conflict", err.Error())
	case errors.Is(err, inventory.ErrCountState):
		fail(c, 409, "count_state_conflict", err.Error())
	case errors.Is(err, inventory.ErrCountStale):
		fail(c, 409, "count_stale", err.Error())
	case errors.Is(err, inventory.ErrTransferState):
		fail(c, 409, "transfer_state_conflict", err.Error())
	case errors.Is(err, inventory.ErrReceiptReason):
		fail(c, 400, "transfer_receipt_reason_required", err.Error())
	case errors.Is(err, inventory.ErrCostWithoutStock):
		fail(c, 409, "inventory_cost_without_stock", err.Error())
	case errors.Is(err, inventory.ErrExportTooLarge):
		fail(c, 409, "inventory_export_too_large", err.Error())
	case errors.Is(err, inventory.ErrKeyConflict), errors.Is(err, inventory.ErrTransferKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	default:
		respondError(c, err)
	}
}
