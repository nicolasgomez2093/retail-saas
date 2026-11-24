package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/modules/sales"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerSales(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := sales.New(db)
	router.GET("/reports/sales", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", sales.ErrReportInvalid.Error())
			return
		}
		filter := salesMarginFilter(c)
		filter.Limit, filter.Offset = limit, offset
		result, err := svc.SalesMarginReport(c.Request.Context(), actor, c.Query("store_id"), filter)
		respondSale(c, http.StatusOK, result, err)
	}))
	router.GET("/reports/collections", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", sales.ErrReportInvalid.Error())
			return
		}
		result, err := svc.Collections(c.Request.Context(), actor, c.Query("store_id"), sales.CollectionsFilter{
			FromDate: c.Query("from_date"), ToDate: c.Query("to_date"), Method: c.Query("method"), Limit: limit, Offset: offset,
		})
		respondSale(c, http.StatusOK, result, err)
	}))
	router.GET("/reports/sales/csv", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		salesCSV(c, "margen-ventas.csv", func(output io.Writer) error {
			return svc.ExportSalesMarginCSV(c.Request.Context(), actor, c.Query("store_id"), salesMarginFilter(c), output)
		})
	}))
	router.POST("/sales", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.Input
		if readAdminJSON(c, &input) {
			result, err := svc.Confirm(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondSale(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/receivables", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", "Verificá la paginación de las cuentas por cobrar")
			return
		}
		result, err := svc.Receivables(c.Request.Context(), actor, c.Query("store_id"), receivableFilter(c), limit, offset)
		respondSale(c, http.StatusOK, result, err)
	}))
	router.GET("/reports/receivables/csv", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		salesCSV(c, "fiados.csv", func(output io.Writer) error {
			return svc.ExportReceivablesCSV(c.Request.Context(), actor, c.Query("store_id"), receivableFilter(c), output)
		})
	}))
	router.GET("/receivables/:id/payments", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", "Verificá la paginación de los abonos")
			return
		}
		result, err := svc.ReceivablePayments(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"), limit, offset)
		respondSale(c, http.StatusOK, result, err)
	}))
	router.POST("/receivables/:id/payments", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReceivablePaymentInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReceivablePayment(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondSale(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/sales", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", "Verificá la paginación del historial de ventas")
			return
		}
		result, err := svc.History(c.Request.Context(), actor, c.Query("store_id"), sales.HistoryFilter{
			Number: c.Query("number"), Query: c.Query("q"), FromDate: c.Query("from_date"), ToDate: c.Query("to_date"), Method: c.Query("method"), Limit: limit, Offset: offset,
		})
		respondSale(c, http.StatusOK, result, err)
	}))
	router.GET("/sales/:id/returns", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			fail(c, 400, "invalid_request", sales.ErrReturnInvalid.Error())
			return
		}
		result, err := svc.Returns(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"), limit, offset)
		respondReturn(c, http.StatusOK, result, err)
	}))
	router.POST("/sales/:id/returns", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReturn(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondReturn(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/sales/:id/return-operations", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnOperationInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReturnOperation(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondReturn(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/sales/:id/return-operations/preview", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnOperationInput
		if readAdminJSON(c, &input) {
			result, err := svc.PreviewReturnOperation(c.Request.Context(), actor, c.Param("id"), input)
			respondReturn(c, http.StatusOK, result, err)
		}
	}))
	router.POST("/sales/:id/returns/:return_id/refund", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnRefundInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReturnRefund(c.Request.Context(), actor, c.Param("id"), c.Param("return_id"), c.GetHeader("Idempotency-Key"), input)
			respondReturn(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/sales/:id/returns/:return_id/debt-credits", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnDebtCreditInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReturnDebtCredit(c.Request.Context(), actor, c.Param("id"), c.Param("return_id"), c.GetHeader("Idempotency-Key"), input)
			respondReturn(c, http.StatusCreated, result, err)
		}
	}))
	router.POST("/sales/:id/returns/:return_id/exchange", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input sales.ReturnExchangeInput
		if readAdminJSON(c, &input) {
			result, err := svc.AddReturnExchange(c.Request.Context(), actor, c.Param("id"), c.Param("return_id"), c.GetHeader("Idempotency-Key"), input)
			respondReturn(c, http.StatusCreated, result, err)
		}
	}))
	router.GET("/sales/:id/ticket", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Ticket(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondSale(c, http.StatusOK, result, err)
	}))
	router.GET("/sales/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Sale(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondSale(c, http.StatusOK, result, err)
	}))
}

func receivableFilter(c *gin.Context) sales.ReceivableFilter {
	return sales.ReceivableFilter{Status: c.DefaultQuery("status", "pending"), Query: c.Query("q"), CustomerID: c.Query("customer_id")}
}

func salesCSV(c *gin.Context, filename string, export func(io.Writer) error) {
	downloadCSV(c, filename, export, func(err error) { respondSale(c, 0, nil, err) })
}

func salesMarginFilter(c *gin.Context) sales.SalesMarginFilter {
	return sales.SalesMarginFilter{
		From: c.Query("from"), To: c.Query("to"),
		FromDate: c.Query("from_date"), ToDate: c.Query("to_date"), Query: c.Query("q"), Method: c.Query("method"),
	}
}

func respondSale(c *gin.Context, status int, result any, err error) {
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.Is(err, sales.ErrInvalid), errors.Is(err, sales.ErrReportInvalid), errors.Is(err, sales.ErrReceivableSearch):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, sales.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, sales.ErrReceivableNotFound):
		fail(c, 404, "receivable_not_found", err.Error())
	case errors.Is(err, sales.ErrReceivableAmount):
		fail(c, 409, "receivable_amount_exceeds_balance", err.Error())
	case errors.Is(err, sales.ErrCustomerRequired):
		fail(c, 400, "sale_customer_required", err.Error())
	case errors.Is(err, sales.ErrCustomerNotFound):
		fail(c, 404, "sale_customer_not_found", err.Error())
	case errors.Is(err, sales.ErrCreditBalance):
		fail(c, 409, "sale_credit_balance", err.Error())
	case errors.Is(err, sales.ErrLocation):
		fail(c, 409, "sale_location_unavailable", err.Error())
	case errors.Is(err, sales.ErrNoPrice):
		fail(c, 409, "sale_price_missing", err.Error())
	case errors.Is(err, sales.ErrPriceChanged):
		fail(c, 409, "sale_price_changed", err.Error())
	case errors.Is(err, sales.ErrPaymentTotal):
		fail(c, 409, "sale_payment_total", err.Error())
	case errors.Is(err, sales.ErrTender):
		fail(c, 409, "sale_insufficient_cash", err.Error())
	case errors.Is(err, sales.ErrTotal):
		fail(c, 409, "sale_amount_limit", err.Error())
	case errors.Is(err, sales.ErrKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, inventory.ErrBalance):
		fail(c, 409, "sale_stock_insufficient", "stock disponible insuficiente para una variante del carrito")
	default:
		respondReservation(c, status, nil, err)
	}
}

func respondReturn(c *gin.Context, status int, result any, err error) {
	switch {
	case errors.Is(err, sales.ErrOperationUnsettled):
		fail(c, 409, "return_operation_unsettled", err.Error())
	case errors.Is(err, sales.ErrExchangeInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, sales.ErrExchangeUnavailable):
		fail(c, 409, "return_exchange_unavailable", err.Error())
	case errors.Is(err, sales.ErrExchangeStock):
		fail(c, 409, "return_exchange_stock", err.Error())
	case errors.Is(err, sales.ErrExchangeChargeAmount):
		fail(c, 409, "return_exchange_charge_amount", err.Error())
	case errors.Is(err, sales.ErrExchangeDifference):
		fail(c, 409, "return_exchange_difference", err.Error())
	case errors.Is(err, sales.ErrExchangeKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, sales.ErrDebtCreditInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, sales.ErrDebtCreditAmount):
		fail(c, 409, "return_debt_credit_amount", err.Error())
	case errors.Is(err, sales.ErrRefundInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, sales.ErrRefundMethod):
		fail(c, 409, "return_refund_method_mismatch", err.Error())
	case errors.Is(err, sales.ErrRefundUnavailable):
		fail(c, 409, "return_refund_unavailable", err.Error())
	case errors.Is(err, sales.ErrRefundAlreadyRecorded):
		fail(c, 409, "return_already_refunded", err.Error())
	case errors.Is(err, sales.ErrRefundAllocation):
		fail(c, 409, "return_refund_allocation", err.Error())
	case errors.Is(err, sales.ErrRefundAmount):
		fail(c, 409, "return_refund_amount", err.Error())
	case errors.Is(err, sales.ErrReturnInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, sales.ErrReturnQuantity):
		fail(c, 409, "return_quantity_exceeded", err.Error())
	case errors.Is(err, sales.ErrReturnLocation):
		fail(c, 409, "return_location_unavailable", err.Error())
	case errors.Is(err, sales.ErrReturnKey):
		fail(c, 409, "idempotency_conflict", err.Error())
	case errors.Is(err, inventory.ErrBalance):
		fail(c, 409, "inventory_balance_conflict", err.Error())
	default:
		respondSale(c, status, result, err)
	}
}
