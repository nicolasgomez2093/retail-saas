package auth

import (
	"strings"

	"github.com/go-playground/validator/v10"
)

var emailValidator = validator.New()

func NormalizeEmail(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	return email, len(email) <= 254 && emailValidator.Var(email, "required,email") == nil
}
