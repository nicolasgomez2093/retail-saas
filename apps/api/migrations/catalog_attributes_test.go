package migrations

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogAttributesLegacyMigration(t *testing.T) {
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
	schema := "test_e4_legacy_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	exec("CREATE SCHEMA " + schema)
	exec("SET LOCAL search_path TO " + schema)
	names, err := scripts.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name.Name() >= "039_catalog_attributes.sql" {
			break
		}
		data, err := scripts.ReadFile(name.Name())
		if err != nil {
			t.Fatal(err)
		}
		exec(string(data))
	}
	exec(`INSERT INTO tenants (slug,name,currency_code,timezone) VALUES ('uno','Uno','ARS','UTC'),('dos','Dos','ARS','UTC');
		INSERT INTO products(tenant_id,name) SELECT id,'Prenda' FROM tenants;
		INSERT INTO product_variants(id,tenant_id,product_id,sku,size,color,revision)
		SELECT x.id::uuid,t.id,p.id,x.sku,x.size,x.color,x.revision
		FROM (VALUES
		  ('00000000-0000-4000-8000-000000000001','uno','A',' 42 ','Negro',19),
		  ('00000000-0000-4000-8000-000000000002','uno','B','M','Azul',6),
		  ('00000000-0000-4000-8000-000000000003','uno','C','m','azul',7),
		  ('00000000-0000-4000-8000-000000000004','uno','D','','Blanco',5),
		  ('00000000-0000-4000-8000-000000000005','uno','E','Único','',4),
		  ('00000000-0000-4000-8000-000000000006','dos','A','M','Azul',1)
		) x(id,slug,sku,size,color,revision)
		JOIN tenants t ON t.slug=x.slug JOIN products p ON p.tenant_id=t.id;
		INSERT INTO stores(tenant_id,code,name) SELECT id,'A','Sucursal' FROM tenants WHERE slug='uno';
		INSERT INTO users(tenant_id,email,name,password_hash) SELECT id,'prueba@example.test','Prueba','fixture' FROM tenants WHERE slug='uno';
		INSERT INTO stock_locations(tenant_id,store_id,code,name,type) SELECT tenant_id,id,'A','Depósito','warehouse' FROM stores;
		INSERT INTO stock_movements(tenant_id,location_id,variant_id,actor_id,sku_snapshot,movement_type,
			quantity_delta,quantity_before,quantity_after,reason)
		SELECT v.tenant_id,l.id,v.id,u.id,'SKU-ANTERIOR','adjustment',1,0,1,'fixture'
		FROM product_variants v JOIN stock_locations l ON l.tenant_id=v.tenant_id JOIN users u ON u.tenant_id=v.tenant_id
		WHERE v.id='00000000-0000-4000-8000-000000000001';`)
	migration, err := scripts.ReadFile("039_catalog_attributes.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	rows, err := tx.Query(ctx, `SELECT v.id::text,v.size,v.color,v.revision,
		 COALESCE(s.name,''),COALESCE(c.name,'')
		 FROM product_variants v
		 LEFT JOIN catalog_attributes s ON (s.tenant_id,s.kind,s.id)=(v.tenant_id,v.size_kind,v.size_id)
		 LEFT JOIN catalog_attributes c ON (c.tenant_id,c.kind,c.id)=(v.tenant_id,v.color_kind,v.color_id)
		 ORDER BY v.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := [][6]string{
		{"1", " 42 ", "Negro", "19", "42", "Negro"},
		{"2", "M", "Azul", "6", "", ""},
		{"3", "m", "azul", "7", "", ""},
		{"4", "", "Blanco", "5", "", "Blanco"},
		{"5", "Único", "", "4", "Único", ""},
		{"6", "M", "Azul", "1", "M", "Azul"},
	}
	for i, expected := range want {
		if !rows.Next() {
			t.Fatalf("falta la variante %d: %v", i, rows.Err())
		}
		var got [6]string
		var revision int
		if err := rows.Scan(&got[0], &got[1], &got[2], &revision, &got[4], &got[5]); err != nil {
			t.Fatal(err)
		}
		if got[0] != "00000000-0000-4000-8000-00000000000"+expected[0] {
			t.Fatal("cambió el UUID", got[0])
		}
		got[0] = expected[0]
		got[3] = strconv.Itoa(revision)
		if got != expected {
			t.Fatalf("variante %d: %q, esperado %q", i, got, expected)
		}
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatal("se alteró el conjunto de variantes", rows.Err())
	}
	var variantID, sku string
	if err := tx.QueryRow(ctx, "SELECT variant_id,sku_snapshot FROM stock_movements").Scan(&variantID, &sku); err != nil ||
		variantID != "00000000-0000-4000-8000-000000000001" || sku != "SKU-ANTERIOR" {
		t.Fatalf("snapshot operativo modificado: %s %s %v", variantID, sku, err)
	}
}
