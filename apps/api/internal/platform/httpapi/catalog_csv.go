package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
)

const csvContentType = "text/csv; charset=utf-8"

func downloadCSV(c *gin.Context, filename string, export func(io.Writer) error, respond func(error)) {
	file, err := os.CreateTemp("", "retail-export-*.csv")
	if err != nil {
		respondError(c, err)
		return
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := export(file); err != nil {
		respond(err)
		return
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		respondError(c, err)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		respondError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.DataFromReader(http.StatusOK, size, csvContentType, file, nil)
}

func registerCatalogCSV(router *gin.RouterGroup, service catalog.Service, authService *auth.Service) {
	router.GET("/catalog/csv/template", authenticated(authService, func(c *gin.Context, _ auth.Identity) {
		var output bytes.Buffer
		if err := catalog.WriteCSVTemplate(&output); err != nil {
			respondError(c, err)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="plantilla-catalogo.csv"`)
		c.Data(http.StatusOK, csvContentType, output.Bytes())
	}))
	router.GET("/catalog/csv/export", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		file, err := os.CreateTemp("", "retail-catalog-*.csv")
		if err != nil {
			respondError(c, err)
			return
		}
		defer func() {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}()
		if err := service.ExportCSV(c.Request.Context(), actor, file); err != nil {
			respondCatalog(c, 0, nil, err)
			return
		}
		info, err := file.Stat()
		if err != nil {
			respondError(c, err)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			respondError(c, err)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="catalogo.csv"`)
		c.DataFromReader(http.StatusOK, info.Size(), csvContentType, file, nil)
	}))
	router.POST("/catalog/csv/import", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		kind, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || kind != "text/csv" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
			fail(c, 415, "unsupported_media_type", "Enviá un CSV codificado en UTF-8")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, catalog.MaxCSVBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				failCSVLimit(c)
			} else {
				fail(c, 400, "invalid_request", "No se pudo leer el archivo CSV")
			}
			return
		}
		result, err := service.ImportCSV(c.Request.Context(), actor, bytes.NewReader(body))
		switch {
		case errors.Is(err, catalog.ErrCSVLimit):
			failCSVLimit(c)
		case errors.Is(err, catalog.ErrCSVInvalid):
			fail(c, 400, "invalid_request", err.Error())
		default:
			respondCatalog(c, http.StatusCreated, result, err)
		}
	}))
}

func failCSVLimit(c *gin.Context) {
	fail(c, 413, "catalog_csv_limit", "El archivo debe tener hasta 2 MB y 1000 filas de datos")
}
