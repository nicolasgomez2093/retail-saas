package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerCatalog(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	service := catalog.New(db)
	registerCatalogCSV(router, service, authService)
	registerCatalogAttributes(router, service, authService)
	registerCatalogIdentifiers(router, service, authService)
	registerCatalogBatches(router, service, authService)
	registerQuickProducts(router, service, authService)
	registerProductPhotos(router, service, authService)
	router.GET("/catalog/classifications", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := service.Classifications(c.Request.Context(), actor)
		respondCatalog(c, http.StatusOK, result, err)
	}))
	for _, kind := range []catalog.ClassificationKind{catalog.Categories, catalog.Brands} {
		router.GET("/"+string(kind), authenticated(authService, func(c *gin.Context, actor auth.Identity) {
			terms, err := service.Terms(c.Request.Context(), actor, kind)
			respondCatalog(c, http.StatusOK, gin.H{"items": terms}, err)
		}))
		save := func(status int) gin.HandlerFunc {
			return authenticated(authService, func(c *gin.Context, actor auth.Identity) {
				var input struct {
					Name string `json:"name"`
				}
				if readAdminJSON(c, &input) {
					result, err := service.SaveTerm(c.Request.Context(), actor, kind, c.Param("id"), input.Name)
					respondCatalog(c, status, result, err)
				}
			})
		}
		router.POST("/"+string(kind), save(http.StatusCreated))
		router.PATCH("/"+string(kind)+"/:id", save(http.StatusOK))
	}
	router.GET("/products", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "25"))
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limitErr != nil || offsetErr != nil {
			respondCatalog(c, 0, nil, catalog.ErrInvalid)
			return
		}
		page, err := service.List(c.Request.Context(), actor, c.Query("q"), c.Query("categoryId"), c.Query("brandId"), c.Query("store_id"), limit, offset)
		respondCatalog(c, http.StatusOK, page, err)
	}))
	router.POST("/products", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input catalog.ProductInput
		if readAdminJSON(c, &input) {
			product, err := service.Create(c.Request.Context(), actor, input)
			respondCatalog(c, http.StatusCreated, product, err)
		}
	}))
	router.GET("/products/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		detail, err := service.Detail(c.Request.Context(), actor, c.Param("id"))
		respondCatalog(c, http.StatusOK, detail, err)
	}))
	router.PATCH("/products/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input catalog.ProductChanges
		if readAdminJSON(c, &input) {
			product, err := service.Update(c.Request.Context(), actor, c.Param("id"), input)
			respondCatalog(c, http.StatusOK, product, err)
		}
	}))
	router.POST("/products/:id/variants", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input catalog.VariantCreate
		if readAdminJSON(c, &input) {
			variant, err := service.AddVariant(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
			respondCatalog(c, http.StatusCreated, variant, err)
		}
	}))
	router.PATCH("/products/:id/variants/:variantId", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input catalog.VariantChanges
		if readAdminJSON(c, &input) {
			result, err := service.UpdateVariant(c.Request.Context(), actor, c.Param("id"), c.Param("variantId"), input)
			respondCatalog(c, http.StatusOK, result, err)
		}
	}))
	router.GET("/barcodes/:code/lookup", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := service.Lookup(c.Request.Context(), actor, c.Param("code"))
		respondCatalog(c, http.StatusOK, result, err)
	}))
}

func respondCatalog(c *gin.Context, status int, result any, err error) {
	var field *catalog.FieldError
	var duplicate *catalog.AttributeDuplicate
	switch {
	case err == nil:
		c.JSON(status, result)
	case errors.As(err, &field):
		c.AbortWithStatusJSON(400, gin.H{"error": gin.H{"code": "invalid_field", "message": field.Message, "field": field.Field}})
	case errors.As(err, &duplicate):
		c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "attribute_duplicate", "message": duplicate.Error(), "field": duplicate.Field}})
	case errors.Is(err, catalog.ErrAttributeInactive):
		fail(c, 409, "attribute_inactive", err.Error())
	case errors.Is(err, catalog.ErrAttributeCombination):
		fail(c, 409, "attribute_combination", err.Error())
	case errors.Is(err, catalog.ErrAttributeChanged):
		fail(c, 409, "attribute_changed", err.Error())
	case errors.Is(err, catalog.ErrRevisionConflict):
		fail(c, 409, "revision_conflict", err.Error())
	case errors.Is(err, catalog.ErrSensitiveHistory):
		fail(c, 409, "sensitive_history", err.Error())
	case errors.Is(err, catalog.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, catalog.ErrClassificationNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, catalog.ErrClassificationName):
		fail(c, 409, "classification_name_taken", err.Error())
	case errors.Is(err, catalog.ErrIdentifierRetired):
		fail(c, 409, "identifier_retired", err.Error())
	case errors.Is(err, catalog.ErrBarcodeReuse):
		fail(c, 409, "barcode_reuse_unsafe", err.Error())
	case errors.Is(err, catalog.ErrInternalExists):
		fail(c, 409, "internal_barcode_exists", err.Error())
	case errors.Is(err, catalog.ErrIdentifierCapacity):
		fail(c, 409, "identifier_capacity", err.Error())
	case errors.Is(err, catalog.ErrVariantCreateKey):
		fail(c, 409, "idempotency_conflict", err.Error())

	case errors.Is(err, catalog.ErrBatchLimit):
		fail(c, 400, "variant_batch_limit", err.Error())
	case errors.Is(err, catalog.ErrSKU):
		fail(c, 409, "sku_taken", err.Error())
	case errors.Is(err, catalog.ErrBarcode):
		fail(c, 409, "barcode_taken", err.Error())
	default:
		respondError(c, err)
	}
}
