package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/config"
	"retail-platform/api/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBulkPricesAndLabelSettings(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("configurá TEST_DATABASE_URL para usar PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "test_bulk_prices_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	execSQL(t, ctx, admin, "CREATE SCHEMA "+schema)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		execSQL(t, cleanup, admin, "DROP SCHEMA "+schema+" CASCADE")
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	const password = "contraseña de prueba"
	const staffPassword = "contraseña equipo de prueba"
	one, err := tenancy.Provision(ctx, db, tenancy.Input{Slug: "tienda-uno", Name: "Tienda Uno", Timezone: "America/Argentina/Buenos_Aires",
		Currency: "ARS", StoreCode: "centro", StoreName: "Centro", OwnerEmail: "uno@example.test", OwnerName: "Uno", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	two, err := tenancy.Provision(ctx, db, tenancy.Input{Slug: "tienda-dos", Name: "Tienda Dos", Timezone: "America/Argentina/Buenos_Aires",
		Currency: "ARS", StoreCode: "centro", StoreName: "Centro", OwnerEmail: "dos@example.test", OwnerName: "Dos", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	service, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://retail.example"
	router := New(config.Config{WebOrigin: origin, Secure: true}, db, service)
	raw := func(method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if method != "GET" {
			req.Header.Set("Origin", origin)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	request := func(method, path, body string, cookie *http.Cookie, want int, headers ...map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		extra := map[string]string{}
		if len(headers) > 0 {
			extra = headers[0]
		}
		res := raw(method, path, body, cookie, extra)
		if res.Code != want {
			t.Fatalf("%s %s: esperado %d, recibido %d %s", method, path, want, res.Code, res.Body)
		}
		return res
	}
	loginAs := func(commerce, email, password string) *http.Cookie {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"commerce": commerce, "email": email, "password": password})
		return request("POST", "/api/v1/auth/login", string(body), nil, 204).Result().Cookies()[0]
	}
	owner := loginAs("tienda-uno", "uno@example.test", password)
	otherOwner := loginAs("tienda-dos", "dos@example.test", password)
	for _, role := range []string{"STORE_MANAGER", "CASHIER"} {
		body, _ := json.Marshal(map[string]any{"name": role, "email": strings.ToLower(role) + "@example.test", "role": role,
			"password": staffPassword, "storeIds": []string{one.StoreID}})
		request("POST", "/api/v1/users", string(body), owner, 201)
	}
	manager := loginAs("tienda-uno", "store_manager@example.test", staffPassword)
	cashier := loginAs("tienda-uno", "cashier@example.test", staffPassword)
	readID := func(res *httptest.ResponseRecorder) string {
		t.Helper()
		var body struct{ ID string }
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.ID == "" {
			t.Fatalf("respuesta sin id: %s", res.Body)
		}
		return body.ID
	}

	// Catalog: a shirt in two colors and three sizes, a cap without stock, a hidden-to-be jacket,
	// a product without photo and an inactive product. Prices and stock live in the Centro branch.
	location := readID(request("POST", "/api/v1/locations", `{"storeId":"`+one.StoreID+`","code":"SALON","name":"Salón","type":"sales_floor","isSellable":true}`, owner, 201))
	priceList := readID(request("POST", "/api/v1/price-lists", `{"name":"General"}`, owner, 201))
	request("PUT", "/api/v1/stores/"+one.StoreID+"/price-list", `{"listId":"`+priceList+`"}`, owner, 204)
	quick := func(key, name string, sizes, colors []string, price string, quantity int) map[string]string {
		t.Helper()
		var variants []map[string]any
		for _, size := range sizes {
			for _, colorName := range colors {
				variants = append(variants, map[string]any{"values": []string{size, colorName}, "sku": "", "barcode": map[string]string{"mode": "internal", "code": ""},
					"price": price, "quantity": quantity})
			}
		}
		body, _ := json.Marshal(map[string]any{"product": map[string]string{"name": name}, "storeId": one.StoreID, "locationId": location,
			"attributes": []map[string]string{{"name": "Talle", "role": "size"}, {"name": "Color", "role": "color"}}, "variants": variants})
		res := request("POST", "/api/v1/quick-products", string(body), owner, 201, map[string]string{"Idempotency-Key": "precios-" + key})
		var created struct {
			Product  struct{ ID string }
			Variants []struct {
				Variant struct{ ID, Size, Color string }
			}
		}
		if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		ids := map[string]string{"product": created.Product.ID}
		for _, v := range created.Variants {
			ids[v.Variant.Size+" "+v.Variant.Color] = v.Variant.ID
		}
		return ids
	}
	shirt := quick("remera", "Remera Lino Hombre", []string{"S", "M"}, []string{"Blanco"}, "25000", 3)
	cap := quick("gorra", "Gorra Trucker", []string{"U"}, []string{"Negro"}, "18000", 1)
	other := quick("short", "Short de Baño", []string{"M"}, []string{"Azul"}, "32000", 1)
	// Remera M Blanco has no price in the list: it is counted, never invented.
	execSQL(t, ctx, db, "DELETE FROM price_list_items WHERE variant_id=$1", shirt["M Blanco"])
	priceOf := func(variant string) string {
		t.Helper()
		var amount string
		if err := db.QueryRow(ctx, "SELECT amount::text FROM price_list_items WHERE variant_id=$1", variant).Scan(&amount); err != nil {
			t.Fatal(err)
		}
		return amount
	}
	adjustPath := "/api/v1/price-lists/" + priceList + "/adjustments"
	adjust := func(cookie *http.Cookie, mode, direction, value, round string, apply bool, want int, products ...string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"productIds": products, "mode": mode, "direction": direction, "value": value, "round": round, "apply": apply})
		res := request("POST", adjustPath, string(body), cookie, want)
		var out map[string]any
		_ = json.Unmarshal(res.Body.Bytes(), &out)
		return out
	}
	afterOf := func(result map[string]any) []string {
		var values []string
		for _, item := range result["items"].([]any) {
			line := item.(map[string]any)
			values = append(values, line["productName"].(string)+" "+line["before"].(string)+"→"+line["after"].(string))
		}
		return values
	}

	t.Run("vista previa sin escribir", func(t *testing.T) {
		preview := adjust(owner, "percent", "up", "10", "100", false, 200, shirt["product"], cap["product"])
		got := strings.Join(afterOf(preview), " | ")
		if got != "Gorra Trucker 18000.00→19800.00 | Remera Lino Hombre 25000.00→27500.00" || preview["withoutPrice"].(float64) != 1 || preview["applied"].(bool) {
			t.Fatalf("vista previa: %s %v", got, preview)
		}
		if priceOf(shirt["S Blanco"]) != "25000.00" {
			t.Fatal("la vista previa modificó el precio")
		}
	})
	t.Run("aplicar suba fija y baja porcentual", func(t *testing.T) {
		applied := adjust(owner, "amount", "up", "1000", "", true, 200, shirt["product"], cap["product"])
		if !applied["applied"].(bool) || priceOf(shirt["S Blanco"]) != "26000.00" || priceOf(cap["U Negro"]) != "19000.00" || priceOf(other["M Azul"]) != "32000.00" {
			t.Fatalf("suba fija: %v %s %s", applied, priceOf(shirt["S Blanco"]), priceOf(cap["U Negro"]))
		}
		adjust(owner, "percent", "down", "5", "", true, 200, shirt["product"])
		if priceOf(shirt["S Blanco"]) != "24700.00" {
			t.Fatalf("baja de 5 %%: %s", priceOf(shirt["S Blanco"]))
		}
		var events int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM price_admin_events WHERE variant_id=$1 AND action='set_price'", shirt["S Blanco"]).Scan(&events); err != nil || events != 3 { // alta rápida + 2 cambios masivos
			t.Fatalf("auditoría de cambios masivos: %d %v", events, err)
		}
	})
	t.Run("rechazos", func(t *testing.T) {
		adjust(owner, "amount", "down", "100000", "", true, 409, shirt["product"])
		if priceOf(shirt["S Blanco"]) != "24700.00" {
			t.Fatal("un cambio rechazado escribió precios")
		}
		adjust(owner, "percent", "down", "100", "", false, 400, shirt["product"])
		adjust(owner, "percent", "up", "0", "", false, 400, shirt["product"])
		adjust(owner, "percent", "sideways", "5", "", false, 400, shirt["product"])
		adjust(owner, "amount", "up", "-5", "", false, 400, shirt["product"])
		adjust(owner, "amount", "up", "5", "", false, 400)
		adjust(owner, "amount", "up", "5", "", false, 400, "no-es-un-id")
		adjust(manager, "amount", "up", "5", "", false, 403, shirt["product"])
		adjust(cashier, "amount", "up", "5", "", false, 403, shirt["product"])
		adjust(otherOwner, "amount", "up", "5", "", false, 404, shirt["product"])
		// Another commerce's products are never touched, even with this commerce's list.
		foreign := adjust(owner, "amount", "up", "5", "", false, 200, two.StoreID)
		if len(foreign["items"].([]any)) != 0 {
			t.Fatalf("productos ajenos en la vista previa: %v", foreign)
		}
	})
	t.Run("etiquetas con o sin precio", func(t *testing.T) {
		if got := request("GET", "/api/v1/settings/labels", "", cashier, 200).Body.String(); !strings.Contains(got, `"showPrice":true`) {
			t.Fatalf("por defecto las etiquetas llevan precio: %s", got)
		}
		request("PUT", "/api/v1/settings/labels", `{"showPrice":false}`, cashier, 403)
		request("PUT", "/api/v1/settings/labels", `{}`, owner, 400)
		request("PUT", "/api/v1/settings/labels", `{"showPrice":false}`, owner, 200)
		if got := request("GET", "/api/v1/settings/labels", "", cashier, 200).Body.String(); !strings.Contains(got, `"showPrice":false`) {
			t.Fatalf("el ajuste no se guardó: %s", got)
		}
		if got := request("GET", "/api/v1/settings/labels", "", otherOwner, 200).Body.String(); !strings.Contains(got, `"showPrice":true`) {
			t.Fatalf("el ajuste se filtró a otro comercio: %s", got)
		}
		request("GET", "/api/v1/settings/labels", "", nil, 401)
	})
}
