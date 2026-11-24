package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/modules/purchases"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerPurchases(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	service := purchases.New(db)
	router.GET("/purchase-orders", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			respondPurchase(c, 0, nil, purchases.ErrInvalid)
			return
		}
		filter := purchaseOrderFilter(c)
		filter.Limit, filter.Offset = limit, offset
		result, err := service.List(c.Request.Context(), actor, c.Query("store_id"), filter)
		respondPurchase(c, http.StatusOK, result, err)
	}))
	router.GET("/reports/purchases", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			respondPurchase(c, 0, nil, purchases.ErrInvalid)
			return
		}
		result, err := service.Report(c.Request.Context(), actor, c.Query("store_id"), purchases.ReportFilter{
			FromDate: c.Query("from_date"), ToDate: c.Query("to_date"), Limit: limit, Offset: offset,
		})
		respondPurchase(c, http.StatusOK, result, err)
	}))
	router.POST("/purchase-orders", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input purchases.OrderInput
		if readAdminJSON(c, &input) {
			result, err := service.Create(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondPurchase(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/purchase-orders/csv", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		downloadCSV(c, "ordenes-de-compra.csv", func(output io.Writer) error {
			return service.ExportOrdersCSV(c.Request.Context(), actor, c.Query("store_id"), purchaseOrderFilter(c), output)
		}, func(err error) { respondPurchase(c, 0, nil, err) })
	}))
	router.GET("/purchase-orders/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := service.Get(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondPurchase(c, http.StatusOK, result, err)
	}))
	router.POST("/purchase-orders/:id/cancel", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input purchases.CancelInput
		if readAdminJSON(c, &input) {
			result, err := service.Cancel(c.Request.Context(), actor, c.Param("id"), input)
			respondPurchase(c, http.StatusOK, result, err)
		}
	}))
	router.POST("/purchase-orders/:id/receipts", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input purchases.ReceiptInput
		if readAdminJSON(c, &input) {
			result, err := service.Receive(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondPurchase(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/purchase-orders/:id/payments", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input purchases.PaymentInput
		if readAdminJSON(c, &input) {
			result, err := service.RegisterPayment(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondPurchase(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/purchase-orders/:id/payments/:paymentId/void", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input purchases.VoidPaymentInput
		if readAdminJSON(c, &input) {
			result, err := service.VoidPayment(c.Request.Context(), actor, c.Param("id"), c.Param("paymentId"), input)
			respondPurchase(c, http.StatusOK, result, err)
		}
	}))
	router.GET("/purchase-orders/:id/receipts", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			respondPurchase(c, 0, nil, purchases.ErrReceiptInvalid)
			return
		}
		result, err := service.Receipts(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"), limit, offset)
		respondPurchase(c, http.StatusOK, result, err)
	}))
	router.GET("/purchase-orders/:id/receipts/csv", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		downloadCSV(c, "recepciones-de-compra.csv", func(output io.Writer) error {
			return service.ExportReceiptsCSV(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"), output)
		}, func(err error) { respondPurchase(c, 0, nil, err) })
	}))
}

func purchaseOrderFilter(c *gin.Context) purchases.ListFilter {
	return purchases.ListFilter{SupplierID: c.Query("supplier_id"), Status: c.Query("status"), Payment: c.Query("payment"), Query: c.Query("q"),
		FromDate: c.Query("from_date"), ToDate: c.Query("to_date")}
}

func respondPurchase(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, purchases.ErrInvalid), errors.Is(err, purchases.ErrReceiptInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, purchases.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, purchases.ErrSupplier):
		fail(c, 409, "purchase_supplier_unavailable", err.Error())
	case errors.Is(err, purchases.ErrVariant):
		fail(c, 409, "purchase_variant_unavailable", err.Error())
	case errors.Is(err, purchases.ErrReceiptLocation):
		fail(c, 409, "purchase_receipt_location", err.Error())
	case errors.Is(err, purchases.ErrReceiptQuantity):
		fail(c, 409, "purchase_receipt_quantity", err.Error())
	case errors.Is(err, inventory.ErrBalance):
		fail(c, 409, "inventory_balance_conflict", err.Error())
	case errors.Is(err, purchases.ErrClosed):
		fail(c, 409, "purchase_order_closed", err.Error())
	case errors.Is(err, purchases.ErrPaymentExceeds):
		fail(c, 409, "purchase_payment_exceeds", err.Error())
	case errors.Is(err, purchases.ErrKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	default:
		respondError(c, err)
	}
}
