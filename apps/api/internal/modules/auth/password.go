package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

const hashPrefix = "argon2id-v19-m65536-t3-p1"

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", errors.New("la contraseña debe tener entre 12 y 1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	encoding := base64.RawStdEncoding
	return hashPrefix + "$" + encoding.EncodeToString(salt) + "$" + encoding.EncodeToString(derive(password, salt)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != hashPrefix {
		return false
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[1])
	hash, hashErr := base64.RawStdEncoding.DecodeString(parts[2])
	if saltErr != nil || hashErr != nil || len(salt) != 16 || len(hash) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(hash, derive(password, salt)) == 1
}

func derive(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
}
