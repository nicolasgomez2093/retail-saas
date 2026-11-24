package httpapi

import (
	"errors"
	"mime"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"

	"github.com/gin-gonic/gin"
)

func registerProductPhotos(router *gin.RouterGroup, service catalog.Service, authService *auth.Service) {
	const path = "/products/:id/images"
	router.POST(path, authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || (contentType != "image/jpeg" && contentType != "image/png") {
			fail(c, 415, "unsupported_media_type", "Seleccioná una imagen JPEG o PNG")
			return
		}
		photos, err := service.UploadPhoto(c.Request.Context(), actor, c.Param("id"), contentType[len("image/"):], c.Request.Body)
		respondPhotos(c, http.StatusCreated, photos, err)
	}))
	router.GET(path+"/:imageId", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		body, err := service.GetPhoto(c.Request.Context(), actor, c.Param("id"), c.Param("imageId"))
		if err != nil {
			respondCatalog(c, 0, nil, err)
			return
		}
		c.Data(http.StatusOK, "image/jpeg", body)
	}))
	router.PUT(path+"/:imageId/primary", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		photos, err := service.SetPrimaryPhoto(c.Request.Context(), actor, c.Param("id"), c.Param("imageId"))
		respondPhotos(c, http.StatusOK, photos, err)
	}))
	router.DELETE(path+"/:imageId", authenticated(authService, func(c *gin.Context, actor auth.Identity) {
		photos, err := service.DeletePhoto(c.Request.Context(), actor, c.Param("id"), c.Param("imageId"))
		respondPhotos(c, http.StatusOK, photos, err)
	}))
}

func respondPhotos(c *gin.Context, status int, photos []catalog.Photo, err error) {
	switch {
	case errors.Is(err, catalog.ErrPhotoInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, catalog.ErrPhotoBytes):
		fail(c, 413, "photo_size_limit", err.Error())
	case errors.Is(err, catalog.ErrPhotoCount):
		fail(c, 409, "photo_count_limit", err.Error())
	default:
		respondCatalog(c, status, gin.H{"images": photos}, err)
	}
}
