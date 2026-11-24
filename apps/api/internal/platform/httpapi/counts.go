package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"

	"github.com/gin-gonic/gin"
)

func registerCounts(router *gin.RouterGroup, svc inventory.Service, authService *auth.Service) {
	const path = "/inventory/counts"
	router.GET(path, authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Counts(c.Request.Context(), actor, c.Query("store_id"), c.Query("location_id"))
		respondInventory(c, http.StatusOK, gin.H{"items": result}, err)
	}))
	router.GET(path+"/history", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		p, ok := existenceParams(c, "store_id", "location_id", "status", "q", "page")
		if !ok {
			return
		}
		page := 1
		if p.Has("page") {
			var err error
			page, err = strconv.Atoi(p.Get("page"))
			if err != nil {
				respondInventory(c, 0, nil, inventory.ErrInvalid)
				return
			}
		}
		result, err := svc.CountHistory(c.Request.Context(), actor, p.Get("store_id"), p.Get("location_id"), p.Get("status"), p.Get("q"), page)
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.PUT(path+"/:id/draft", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.CountDraft
		if !readAdminJSON(c, &input) {
			return
		}
		result, err := svc.SaveCountDraft(c.Request.Context(), actor, c.Param("id"), input)
		respondCountWorkflow(c, result, err)
	}))
	router.GET(path+"/:id/review", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		p, ok := existenceParams(c, "store_id")
		if !ok {
			return
		}
		result, err := svc.ReviewCount(c.Request.Context(), actor, p.Get("store_id"), c.Param("id"))
		respondCountWorkflow(c, result, err)
	}))
	router.POST(path+"/:id/close", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.CountClose
		if !readAdminJSON(c, &input) {
			return
		}
		result, err := svc.CloseCount(c.Request.Context(), actor, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
		respondCountWorkflow(c, result, err)
	}))
	router.GET(path+"/:id/close-result", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		p, ok := existenceParams(c, "store_id", "key")
		if !ok {
			return
		}
		result, err := svc.CountCloseResult(c.Request.Context(), actor, p.Get("store_id"), c.Param("id"), p.Get("key"))
		respondCountWorkflow(c, result, err)
	}))
	router.GET(path+"/:id", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		result, err := svc.Count(c.Request.Context(), actor, c.Query("store_id"), c.Param("id"))
		respondInventory(c, http.StatusOK, result, err)
	}))
	router.POST(path, authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.NewCount
		if readAdminJSON(c, &input) {
			result, err := svc.StartCount(c.Request.Context(), actor, c.GetHeader("Idempotency-Key"), input)
			respondInventory(c, http.StatusCreated, result, err)
		}
	}))
	router.POST(path+"/:id/complete", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input inventory.CountCompletion
		if readAdminJSON(c, &input) {
			result, err := svc.CompleteCount(c.Request.Context(), actor, c.Param("id"), input)
			respondInventory(c, http.StatusOK, result, err)
		}
	}))
	router.POST(path+"/:id/cancel", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		var input struct {
			StoreID string `json:"storeId"`
		}
		if readAdminJSON(c, &input) {
			result, err := svc.CancelCount(c.Request.Context(), actor, input.StoreID, c.Param("id"))
			respondInventory(c, http.StatusOK, result, err)
		}
	}))
}

func respondCountWorkflow(c *gin.Context, result any, err error) {
	var conflict *inventory.CountConflicts
	if errors.As(err, &conflict) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": conflict.Code, "message": conflict.Error(), "variantIds": conflict.Variants}})
		return
	}
	respondInventory(c, http.StatusOK, result, err)
}
