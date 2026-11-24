package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5/pgxpool"
)

func runProvision(args []string) error {
	var input tenancy.Input
	flags := flag.NewFlagSet("provision-tenant", flag.ContinueOnError)
	flags.StringVar(&input.Slug, "slug", "", "slug para iniciar sesión")
	flags.StringVar(&input.Name, "name", "", "nombre del comercio")
	flags.StringVar(&input.Timezone, "timezone", "", "zona horaria IANA o UTC")
	flags.StringVar(&input.Currency, "currency", "", "código de moneda")
	flags.StringVar(&input.StoreCode, "store-code", "", "código de la sucursal inicial")
	flags.StringVar(&input.StoreName, "store-name", "", "nombre de la sucursal inicial")
	flags.StringVar(&input.OwnerEmail, "owner-email", "", "correo del propietario")
	flags.StringVar(&input.OwnerName, "owner-name", "", "nombre del propietario")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("argumentos inválidos para provision-tenant")
	}
	if flags.NArg() != 0 {
		return errors.New("provision-tenant solo acepta opciones con nombre")
	}
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 1026))
	password, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.New("no se pudo leer la contraseña desde la entrada estándar")
	}
	input.Password = strings.TrimSuffix(password, "\n")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL es obligatoria")
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalContext, 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("DATABASE_URL inválida")
	}
	defer db.Close()
	result, err := tenancy.Provision(ctx, db, input)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
