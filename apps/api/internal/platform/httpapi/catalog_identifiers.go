package httpapi

import (
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
)

func registerCatalogIdentifiers(router *gin.RouterGroup, service catalog.Service, identity *auth.Service) {
	router.GET("/catalog/sku-suggestion", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		after, err := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
		if err != nil {
			respondCatalog(c, 0, nil, catalog.ErrInvalid)
			return
		}
		result, err := service.SuggestSKU(c.Request.Context(), actor, after)
		respondCatalog(c, http.StatusOK, result, err)
	}))
	router.GET("/catalog/identifier-settings", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		result, err := service.IdentifierSettings(c.Request.Context(), actor)
		respondCatalog(c, http.StatusOK, result, err)
	}))
	router.PATCH("/catalog/identifier-settings", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			Prefix   string `json:"skuPrefix"`
			Width    int    `json:"skuWidth"`
			Expected int64  `json:"expectedRevision"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		result, err := service.ConfigureIdentifiers(c.Request.Context(), actor, catalog.IdentifierSettings{Prefix: input.Prefix, Width: input.Width}, input.Expected)
		respondCatalog(c, http.StatusOK, result, err)
	}))
	router.GET("/catalog/identifier-history", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		result, err := service.IdentifierHistory(c.Request.Context(), actor, c.Query("kind"), c.Query("value"))
		respondCatalog(c, http.StatusOK, gin.H{"items": result}, err)
	}))
}
