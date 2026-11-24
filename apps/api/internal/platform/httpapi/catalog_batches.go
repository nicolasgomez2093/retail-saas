package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
)

func registerCatalogBatches(router *gin.RouterGroup, service catalog.Service, identity *auth.Service) {
	type payload struct {
		Rows []catalog.BatchVariantInput `json:"rows"`
	}
	router.POST("/products/:id/variants/preview", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		var input payload
		if !readVariantBatchJSON(c, &input) {
			return
		}
		result, err := service.PreviewVariantBatch(c.Request.Context(), actor, c.Param("id"), input.Rows)
		respondCatalog(c, http.StatusOK, gin.H{"rows": result}, err)
	}))
	router.POST("/products/:id/variants/batch", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		var input payload
		if !readVariantBatchJSON(c, &input) {
			return
		}
		result, err := service.CreateVariantBatch(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input.Rows)
		var issue *catalog.VariantBatchError
		if errors.As(err, &issue) {
			status := http.StatusConflict
			for _, row := range issue.Issues {
				if row.Code == "variant_write_failed" {
					status = http.StatusInternalServerError
					break
				}
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{
				"code": "variant_batch_invalid", "message": issue.Error(), "rows": issue.Issues,
			}})
			return
		}
		respondCatalog(c, http.StatusCreated, result, err)
	}))
}

// registerQuickProducts exposes the one-screen product creation. The whole
// request is confirmed in a single transaction; the Idempotency-Key makes a
// retry after a lost response return the original result.
func registerQuickProducts(router *gin.RouterGroup, service catalog.Service, identity *auth.Service) {
	router.POST("/quick-products", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		var input catalog.QuickProductInput
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if contentType != "application/json" || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			fail(c, 400, "invalid_request", "Solicitud JSON de alta inválida")
			return
		}
		result, err := service.QuickCreate(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
		var issue *catalog.VariantBatchError
		switch {
		case errors.As(err, &issue):
			status := http.StatusConflict
			for _, row := range issue.Issues {
				if row.Code == "variant_write_failed" {
					status = http.StatusInternalServerError
				}
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{
				"code": "quick_product_invalid", "message": "El producto no se guardó. Revisá las variantes indicadas.", "rows": issue.Issues,
			}})
		case errors.Is(err, catalog.ErrQuickNoList):
			fail(c, 409, "price_list_missing", "La sucursal no tiene una lista de precios asignada. Asignala en Precios o guardá sin precios.")
		case errors.Is(err, catalog.ErrQuickLocation):
			c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "invalid_field", "field": "locationId", "message": "La ubicación no pertenece a la sucursal o está inactiva."}})
		case errors.Is(err, catalog.ErrQuickKeyRace):
			fail(c, 409, "idempotency_in_progress", err.Error())
		default:
			respondCatalog(c, http.StatusCreated, result, err)
		}
	}))
}

// The individual admin-body cap cannot hold all 50 valid rows with Unicode SKUs.
func readVariantBatchJSON(c *gin.Context, output any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if contentType != "application/json" || decoder.Decode(output) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(c, 400, "invalid_request", "Solicitud JSON de variantes inválida")
		return false
	}
	return true
}
