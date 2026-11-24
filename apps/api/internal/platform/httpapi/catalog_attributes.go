package httpapi

import (
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
)

func registerCatalogAttributes(router *gin.RouterGroup, service catalog.Service, identity *auth.Service) {
	for _, kind := range []catalog.AttributeKind{catalog.Sizes, catalog.Colors, catalog.SizeGroups} {
		path := "/catalog/" + string(kind)
		router.GET(path, authenticated(identity, func(c *gin.Context, actor auth.Identity) {
			items, err := service.Attributes(c.Request.Context(), actor, kind)
			respondCatalog(c, http.StatusOK, gin.H{"items": items}, err)
		}))
		save := func(status int) gin.HandlerFunc {
			return authenticated(identity, func(c *gin.Context, actor auth.Identity) {
				var input catalog.AttributeWrite
				if !readAdminJSON(c, &input) {
					return
				}
				result, err := service.SaveAttribute(c.Request.Context(), actor, kind, c.Param("id"), input)
				respondCatalog(c, status, result, err)
			})
		}
		router.POST(path, save(http.StatusCreated))
		router.PATCH(path+"/:id", save(http.StatusOK))
	}
	router.GET("/catalog/pending-attributes", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if err != nil {
			respondCatalog(c, 0, nil, catalog.ErrInvalid)
			return
		}
		result, err := service.PendingAttributeVariants(c.Request.Context(), actor, c.Query("q"), offset)
		respondCatalog(c, http.StatusOK, result, err)
	}))
}
