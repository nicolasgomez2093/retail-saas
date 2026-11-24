package httpapi

import (
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"

	"github.com/gin-gonic/gin"
)

func registerMovementHistory(router *gin.RouterGroup, svc inventory.Service, identity *auth.Service) {
	router.GET("/inventory/movements/history", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		p, ok := existenceParams(c, "store_id", "q", "tipo", "desde", "hasta", "ubicacion", "variante", "responsable", "referencia", "siguiente", "anterior")
		if !ok {
			return
		}
		result, err := svc.MovementHistory(c.Request.Context(), actor, p.Get("store_id"), inventory.MovementFilter{
			Query: p.Get("q"), Kind: p.Get("tipo"), From: p.Get("desde"), To: p.Get("hasta"),
			Location: p.Get("ubicacion"), Variant: p.Get("variante"), Actor: p.Get("responsable"),
			Reference: p.Get("referencia"), Older: p.Get("siguiente"), Newer: p.Get("anterior"),
		})
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.GET("/inventory/movements/history/:id", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		p, ok := existenceParams(c, "store_id")
		if !ok {
			return
		}
		result, err := svc.MovementDetail(c.Request.Context(), actor, p.Get("store_id"), c.Param("id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
}
