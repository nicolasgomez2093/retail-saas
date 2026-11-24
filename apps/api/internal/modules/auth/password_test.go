package auth

import "testing"

func TestPassword(t *testing.T) {
	const password = "una frase de prueba"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == otherHash || !verifyPassword(password, hash) ||
		verifyPassword("contraseña incorrecta", hash) || verifyPassword(password, "hash inválido") {
		t.Fatal("falló la validación de contraseña o la generación de salt")
	}
	if _, err := HashPassword("corta"); err == nil {
		t.Fatal("se aceptó una contraseña demasiado corta")
	}
}
