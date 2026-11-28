package httpapi

import (
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/labels"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerLabels(router *gin.RouterGroup, db *pgxpool.Pool, authService *auth.Service) {
	svc := labels.New(db)
	router.GET("/settings/labels", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Get(c.Request.Context(), actor)
		respondLabels(c, result, err)
	}))
	router.PUT("/settings/labels", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			ShowPrice *bool `json:"showPrice"`
		}
		if !readAdminJSON(c, &input) {
			return
		}
		if input.ShowPrice == nil {
			fail(c, 400, "invalid_request", "indicá si las etiquetas llevan precio")
			return
		}
		result, err := svc.Save(c.Request.Context(), actor, labels.Settings{ShowPrice: *input.ShowPrice})
		respondLabels(c, result, err)
	}))
}

func respondLabels(c *gin.Context, result labels.Settings, err error) {
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
