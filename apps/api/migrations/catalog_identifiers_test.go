package migrations

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogIdentifiersLegacyMigration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("configurá TEST_DATABASE_URL para usar PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	exec := func(sql string) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	schema := "test_e5_legacy_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	exec("CREATE SCHEMA " + schema)
	exec("SET LOCAL search_path TO " + schema)
	names, err := scripts.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name.Name() >= "040_catalog_identifiers.sql" {
			break
		}
		data, err := scripts.ReadFile(name.Name())
		if err != nil {
			t.Fatal(err)
		}
		exec(string(data))
	}
	exec(`INSERT INTO tenants(slug,name,currency_code,timezone) VALUES ('e5','E5','ARS','UTC');
		INSERT INTO users(tenant_id,email,name,password_hash) SELECT id,'owner@example.test','Prueba','fixture' FROM tenants;
		INSERT INTO products(tenant_id,name) SELECT id,'Prenda' FROM tenants;
		INSERT INTO product_variants(tenant_id,product_id,sku,revision) SELECT tenant_id,id,'000002',9 FROM products;
		INSERT INTO barcodes(tenant_id,variant_id,code,is_primary)
			SELECT tenant_id,id,'ACTUAL-E5',true FROM product_variants;
		INSERT INTO catalog_sensitive_events(tenant_id,actor_id,variant_id,action,reason,before_data,after_data)
			SELECT v.tenant_id,u.id,v.id,'correct_variant','fixture',
				'{"sku":"000001","barcodes":["ACTUAL-E5","RETIRADO-E5"]}'::jsonb,'{"sku":"000002"}'::jsonb
			FROM product_variants v JOIN users u ON u.tenant_id=v.tenant_id;`)
	migration, err := scripts.ReadFile("040_catalog_identifiers.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	var retiredSKU, retiredCode, keepActive, keepSnapshot bool
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*),bool_or(kind='sku' AND value='000001' AND action='retired' AND actor_id IS NULL),
		bool_or(kind='barcode' AND value='RETIRADO-E5' AND action='retired' AND actor_id IS NULL),
		EXISTS(SELECT 1 FROM product_variants v JOIN barcodes b ON (b.tenant_id,b.variant_id)=(v.tenant_id,v.id)
			WHERE v.sku='000002' AND v.revision=9 AND b.code='ACTUAL-E5' AND b.is_primary AND NOT b.is_internal),
		EXISTS(SELECT 1 FROM catalog_sensitive_events WHERE before_data->>'sku'='000001' AND before_data->'barcodes' ? 'RETIRADO-E5')
		FROM catalog_identifier_events`).Scan(&count, &retiredSKU, &retiredCode, &keepActive, &keepSnapshot)
	if err != nil || count != 2 || !retiredSKU || !retiredCode || !keepActive || !keepSnapshot {
		t.Fatalf("migración: %d %v %v %v %v (%v)", count, retiredSKU, retiredCode, keepActive, keepSnapshot, err)
	}
}
