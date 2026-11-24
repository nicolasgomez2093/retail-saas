package httpapi

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"

	"github.com/gin-gonic/gin"
)

func registerExistences(router *gin.RouterGroup, service inventory.Service, identity *auth.Service) {
	router.GET("/inventory/existences", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		storeID, filter, ok := existenceFilter(c)
		if ok {
			result, err := service.Existences(c.Request.Context(), actor, storeID, filter)
			respondInventory(c, http.StatusOK, result, err)
		}
	}))
	router.GET("/inventory/existences/csv", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		storeID, filter, ok := existenceFilter(c)
		if ok {
			downloadCSV(c, "inventario.csv", func(output io.Writer) error {
				return service.ExportExistencesCSV(c.Request.Context(), actor, storeID, filter, output)
			}, func(err error) { respondInventory(c, 0, nil, err) })
		}
	}))
	router.PUT("/inventory/costs", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		var input inventory.CostInput
		if readAdminJSON(c, &input) {
			result, err := service.SetCosts(c.Request.Context(), actor, input)
			respondInventory(c, http.StatusOK, gin.H{"items": result}, err)
		}
	}))
	router.GET("/inventory/existences/:variantId", authenticated(identity, func(c *gin.Context, actor auth.Identity) {
		params, ok := existenceParams(c, "store_id")
		if !ok {
			return
		}
		result, err := service.Existence(c.Request.Context(), actor, params.Get("store_id"), c.Param("variantId"))
		respondInventory(c, http.StatusOK, result, err)
	}))
}

// existenceFilter reads the Existencias filters shared by the listing and its CSV.
func existenceFilter(c *gin.Context) (string, inventory.ExistenceFilter, bool) {
	params, ok := existenceParams(c, "store_id", "q", "categoryId", "brandId", "producto", "variante", "saldo", "ubicacion", "bajo", "page")
	if !ok {
		return "", inventory.ExistenceFilter{}, false
	}
	page, err := strconv.Atoi(params.Get("page"))
	if strings.ContainsAny(params.Get("page"), "+-") {
		err = inventory.ErrInvalid
	}
	if !params.Has("page") {
		page, err = 1, nil
	}
	low := params.Get("bajo")
	if err != nil || (low != "" && low != "0" && low != "1") {
		respondInventory(c, 0, nil, inventory.ErrInvalid)
		return "", inventory.ExistenceFilter{}, false
	}
	return params.Get("store_id"), inventory.ExistenceFilter{
		Query: params.Get("q"), Category: params.Get("categoryId"), Brand: params.Get("brandId"),
		ProductStatus: params.Get("producto"), VariantStatus: params.Get("variante"), Balance: params.Get("saldo"),
		Location: params.Get("ubicacion"), LowOnly: low == "1", Page: page,
	}, true
}

func existenceParams(c *gin.Context, keys ...string) (url.Values, bool) {
	params, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err == nil {
		allowed := make(map[string]bool, len(keys))
		for _, key := range keys {
			allowed[key] = true
		}
		for key, entries := range params {
			if !allowed[key] || len(entries) != 1 {
				respondInventory(c, 0, nil, inventory.ErrInvalid)
				return nil, false
			}
		}
		return params, true
	}
	respondInventory(c, 0, nil, inventory.ErrInvalid)
	return nil, false
}
