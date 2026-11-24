package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrSlugTaken = errors.New("ya existe un comercio con ese slug")

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type Input struct {
	Slug, Name, Timezone, Currency  string
	StoreCode, StoreName            string
	OwnerEmail, OwnerName, Password string
}

type Result struct {
	TenantID string `json:"tenantId"`
	OwnerID  string `json:"ownerId"`
	StoreID  string `json:"storeId"`
}

func Provision(ctx context.Context, db interface {
	Begin(context.Context) (pgx.Tx, error)
}, input Input) (Result, error) {
	input, err := normalize(input)
	if err != nil {
		return Result{}, err
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO tenants(slug,name,timezone,currency_code)
			VALUES ($1,$2,$3,$4) RETURNING id`, input.Slug, input.Name, input.Timezone, input.Currency).Scan(&result.TenantID); err != nil {
			var dbErr *pgconn.PgError
			if errors.As(err, &dbErr) && dbErr.Code == "23505" && dbErr.ConstraintName == "tenants_slug_key" {
				return ErrSlugTaken
			}
			return fmt.Errorf("no se pudo crear el comercio: %w", err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO stores(tenant_id,code,name)
			VALUES ($1,$2,$3) RETURNING id`, result.TenantID, input.StoreCode, input.StoreName).Scan(&result.StoreID); err != nil {
			return fmt.Errorf("no se pudo crear la sucursal: %w", err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO users(tenant_id,email,name,password_hash)
			VALUES ($1,$2,$3,$4) RETURNING id`, result.TenantID, input.OwnerEmail, input.OwnerName, hash).Scan(&result.OwnerID); err != nil {
			return fmt.Errorf("no se pudo crear el propietario: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(tenant_id,user_id,role_code) VALUES ($1,$2,'OWNER')`, result.TenantID, result.OwnerID); err != nil {
			return fmt.Errorf("no se pudo asignar el rol: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_stores(tenant_id,user_id,store_id) VALUES ($1,$2,$3)`, result.TenantID, result.OwnerID, result.StoreID); err != nil {
			return fmt.Errorf("no se pudo asignar la sucursal: %w", err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func normalize(input Input) (Input, error) {
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Timezone = strings.TrimSpace(input.Timezone)
	if len(input.Slug) > 80 || !slugPattern.MatchString(input.Slug) {
		return Input{}, errors.New("el slug debe tener entre 1 y 80 caracteres: letras ASCII, números y guiones entre segmentos")
	}
	email, valid := auth.NormalizeEmail(input.OwnerEmail)
	if !valid {
		return Input{}, errors.New("correo del propietario inválido")
	}
	input.OwnerEmail = email
	if !currencyPattern.MatchString(input.Currency) {
		return Input{}, errors.New("la moneda debe ser un código de tres letras")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil || input.Timezone == "" || input.Timezone == "Local" {
		return Input{}, errors.New("la zona horaria debe ser un identificador IANA o UTC")
	}
	fields := []struct {
		name  string
		value *string
		limit int
	}{
		{"nombre del comercio", &input.Name, 160},
		{"código de sucursal", &input.StoreCode, 40},
		{"nombre de sucursal", &input.StoreName, 160},
		{"nombre del propietario", &input.OwnerName, 160},
	}
	for _, field := range fields {
		*field.value = strings.TrimSpace(*field.value)
		if *field.value == "" || !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.limit || strings.ContainsRune(*field.value, 0) {
			return Input{}, fmt.Errorf("%s inválido (máximo %d caracteres)", field.name, field.limit)
		}
	}
	return input, nil
}
