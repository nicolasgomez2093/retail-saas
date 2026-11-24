package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/users"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerUsers(router *gin.RouterGroup, db *pgxpool.Pool, service *auth.Service) {
	admin := users.New(db)
	router.GET("/users", authenticated(service, func(c *gin.Context, identity auth.Identity) {
		result, err := admin.List(c.Request.Context(), identity)
		if err != nil {
			respondUserError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	}))
	router.POST("/users", authenticated(service, func(c *gin.Context, identity auth.Identity) {
		var input users.CreateInput
		if !readAdminJSON(c, &input) {
			return
		}
		result, err := admin.Create(c.Request.Context(), identity, input)
		if err != nil {
			respondUserError(c, err)
			return
		}
		c.JSON(http.StatusCreated, result)
	}))
	router.PATCH("/users/:id", authenticated(service, func(c *gin.Context, identity auth.Identity) {
		var input users.UpdateInput
		if !readAdminJSON(c, &input) {
			return
		}
		result, err := admin.Update(c.Request.Context(), identity, c.Param("id"), input)
		if err != nil {
			respondUserError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	}))
}

func readAdminJSON(c *gin.Context, output any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if contentType != "application/json" || decoder.Decode(output) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(c, 400, "invalid_request", "Solicitud JSON inválida")
		return false
	}
	return true
}

func respondUserError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, users.ErrInvalid):
		fail(c, 400, "invalid_request", err.Error())
	case errors.Is(err, users.ErrNotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, users.ErrEmailTaken):
		fail(c, 409, "email_taken", err.Error())
	default:
		respondError(c, err)
	}
}
