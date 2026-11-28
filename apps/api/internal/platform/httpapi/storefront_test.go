package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/storefront"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/config"
	"retail-platform/api/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStorefront(t *testing.T) {
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
	schema := "test_storefront_" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
	warehouse := readID(request("POST", "/api/v1/locations", `{"storeId":"`+one.StoreID+`","code":"DEP","name":"Depósito","type":"warehouse","isSellable":false}`, owner, 201))
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
		res := request("POST", "/api/v1/quick-products", string(body), owner, 201, map[string]string{"Idempotency-Key": "tienda-" + key})
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
	shirt := quick("remera", "Remera Lino Hombre", []string{"S", "M", "L"}, []string{"Blanco", "Negro"}, "25000", 3)
	cap := quick("gorra", "Gorra Trucker", []string{"U"}, []string{"Negro"}, "18000", 0)
	jacket := quick("campera", "Campera de Jean", []string{"M"}, []string{"Celeste"}, "120000", 1)
	nophoto := quick("short", "Short de Baño", []string{"M"}, []string{"Azul"}, "32000", 2)
	inactive := quick("vestido", "Vestido Lino", []string{"S"}, []string{"Arena"}, "70000", 2)
	execSQL(t, ctx, db, "UPDATE products SET status='inactive' WHERE id=$1", inactive["product"])
	// Shirt L Negro: its only units are reserved; shirt S Blanco: units only in the warehouse.
	execSQL(t, ctx, db, "UPDATE stock_balances SET quantity_reserved=quantity_on_hand WHERE variant_id=$1", shirt["L Negro"])
	execSQL(t, ctx, db, "UPDATE stock_balances SET location_id=$2 WHERE variant_id=$1", shirt["S Blanco"], warehouse)
	// Shirt M Negro: price dropped from the list; it is not offered.
	execSQL(t, ctx, db, "DELETE FROM price_list_items WHERE variant_id=$1", shirt["M Negro"])

	var photo bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	for y := range 1200 {
		for x := range 1600 {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	if err := png.Encode(&photo, picture); err != nil {
		t.Fatal(err)
	}
	upload := func(product string) string {
		t.Helper()
		res := request("POST", "/api/v1/products/"+product+"/images", photo.String(), owner, 201, map[string]string{"Content-Type": "image/png"})
		var body struct{ Images []struct{ ID string } }
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || len(body.Images) == 0 {
			t.Fatalf("foto: %s", res.Body)
		}
		return body.Images[len(body.Images)-1].ID
	}
	shirtPhoto := upload(shirt["product"])
	shirtBack := upload(shirt["product"])
	upload(cap["product"])
	jacketPhoto := upload(jacket["product"])
	upload(inactive["product"])
	_ = nophoto

	const settings = "/api/v1/storefront"
	const public = "/api/v1/public/storefronts/tienda-uno"
	t.Run("Permisos de administración", func(t *testing.T) {
		request("GET", settings, "", nil, 401)
		request("GET", settings, "", manager, 403)
		request("GET", settings, "", cashier, 403)
		request("PUT", settings, `{"enabled":false,"storeId":null,"whatsapp":"","title":"","welcome":"","showSoldOut":true}`, cashier, 403)
		request("PUT", settings+"/products/"+shirt["product"], `{"hidden":true}`, manager, 403)
		// Another commerce cannot hide this product.
		request("PUT", settings+"/products/"+shirt["product"], `{"hidden":true}`, otherOwner, 404)
	})

	t.Run("Tienda desactivada no se publica", func(t *testing.T) {
		request("GET", public, "", nil, 404)
		var view storefront.Admin
		if err := json.Unmarshal(request("GET", settings, "", owner, 200).Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.Enabled || view.Slug != "tienda-uno" || view.StoreID != nil || len(view.Stores) != 1 || !view.ShowSoldOut {
			t.Fatalf("configuración inicial: %+v", view.Settings)
		}
		// The preview uses the only branch: shirt and jacket visible, cap sold out, short without photo.
		statuses := map[string]string{}
		for _, product := range view.Products {
			statuses[product.Name] = product.Status
		}
		want := map[string]string{"Remera Lino Hombre": "published", "Gorra Trucker": "sold_out", "Campera de Jean": "published", "Short de Baño": "no_photo"}
		for name, status := range want {
			if statuses[name] != status {
				t.Fatalf("estado de %s: %q, esperado %q (%v)", name, statuses[name], status, statuses)
			}
		}
		if _, listed := statuses["Vestido Lino"]; listed {
			t.Fatal("un producto inactivo no se lista")
		}
	})

	t.Run("Validaciones", func(t *testing.T) {
		request("PUT", settings, `{"enabled":true,"storeId":null,"whatsapp":"5491155556666","title":"","welcome":"","showSoldOut":true}`, owner, 400)
		request("PUT", settings, `{"enabled":true,"storeId":"`+one.StoreID+`","whatsapp":"","title":"","welcome":"","showSoldOut":true}`, owner, 400)
		request("PUT", settings, `{"enabled":false,"storeId":null,"whatsapp":"abc","title":"","welcome":"","showSoldOut":true}`, owner, 400)
		request("PUT", settings, `{"enabled":false,"storeId":null,"whatsapp":"123","title":"","welcome":"","showSoldOut":true}`, owner, 400)
		request("PUT", settings, `{"enabled":false,"storeId":null,"whatsapp":"","title":"`+strings.Repeat("a", 81)+`","welcome":"","showSoldOut":true}`, owner, 400)
		request("PUT", settings, `{"enabled":false,"storeId":"`+two.StoreID+`","whatsapp":"","title":"","welcome":"","showSoldOut":true}`, owner, 404)
		request("PUT", settings, `{"enabled":false,"extra":1}`, owner, 400)
		request("PUT", settings+"/products/"+shirt["product"], `{}`, owner, 400)
		request("PUT", settings+"/products/no-es-uuid", `{"hidden":true}`, owner, 404)
	})

	var store storefront.PublicStore
	t.Run("Publicación", func(t *testing.T) {
		res := request("PUT", settings, `{"enabled":true,"storeId":"`+one.StoreID+`","whatsapp":"+54 9 11 5555-6666","title":"Uno Indumentaria","welcome":"Envíos en el día","showSoldOut":true}`, owner, 200)
		var view storefront.Admin
		if err := json.Unmarshal(res.Body.Bytes(), &view); err != nil || view.WhatsApp != "5491155556666" || !view.Enabled {
			t.Fatalf("guardado: %s", res.Body)
		}
		res = request("GET", public, "", nil, 200)
		if res.Header().Get("Set-Cookie") != "" || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("la tienda pública no usa sesión ni se guarda en caché")
		}
		body := res.Body.String()
		for _, secret := range []string{"quantity", "onHand", "sellable", "cost", "sku", one.TenantID, location} {
			if strings.Contains(body, secret) {
				t.Fatalf("la tienda pública expone %q: %s", secret, body)
			}
		}
		if err := json.Unmarshal(res.Body.Bytes(), &store); err != nil {
			t.Fatal(err)
		}
		if store.Name != "Uno Indumentaria" || store.WhatsApp != "5491155556666" || store.Welcome != "Envíos en el día" || store.Branch != "Centro" || store.Currency != "ARS" {
			t.Fatalf("datos de la tienda: %+v", store)
		}
		// One card per product: the shirt is a single product, not six variants. Available first.
		names := []string{}
		for _, product := range store.Products {
			names = append(names, product.Name)
		}
		if strings.Join(names, "|") != "Campera de Jean|Remera Lino Hombre|Gorra Trucker" {
			t.Fatalf("productos publicados: %v", names)
		}
		if store.Products[1].ImageID != shirtPhoto || store.Products[1].PriceFrom != "25000.00" || !store.Products[1].Available || store.Products[2].Available {
			t.Fatalf("tarjetas: %+v", store.Products)
		}
	})

	t.Run("Ficha del producto", func(t *testing.T) {
		res := request("GET", public+"/products/"+shirt["product"], "", nil, 200)
		var detail storefront.PublicProductDetail
		if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Name != "Remera Lino Hombre" || len(detail.Images) != 2 || detail.Images[0] != shirtPhoto || detail.Images[1] != shirtBack {
			t.Fatalf("ficha: %+v", detail)
		}
		available := map[string]bool{}
		for _, variant := range detail.Variants {
			available[variant.Size+" "+variant.Color] = variant.Available
			if variant.Price != "25000.00" {
				t.Fatalf("precio de variante: %+v", variant)
			}
		}
		// M Negro has no price: not offered. L Negro only reserved and S Blanco only in the warehouse: sold out.
		want := map[string]bool{"S Blanco": false, "S Negro": true, "M Blanco": true, "L Blanco": true, "L Negro": false}
		if len(available) != len(want) {
			t.Fatalf("variantes ofrecidas: %v", available)
		}
		for key, value := range want {
			if got, ok := available[key]; !ok || got != value {
				t.Fatalf("disponibilidad de %s: %v (%v)", key, got, available)
			}
		}
		if strings.Contains(res.Body.String(), "quantity") || strings.Contains(res.Body.String(), "sku") {
			t.Fatal("la ficha no expone cantidades ni SKU")
		}
		request("GET", public+"/products/"+nophoto["product"], "", nil, 404)
		request("GET", public+"/products/"+inactive["product"], "", nil, 404)
		request("GET", "/api/v1/public/storefronts/tienda-dos/products/"+shirt["product"], "", nil, 404)
		request("GET", public+"/products/no-es-uuid", "", nil, 404)
	})

	t.Run("Fotos reducidas", func(t *testing.T) {
		for size, side := range map[string]int{"card": storefront.SizeCard, "detail": storefront.SizeDetail} {
			res := request("GET", public+"/images/"+shirtPhoto+"?size="+size, "", nil, 200)
			if res.Header().Get("Content-Type") != "image/jpeg" || res.Header().Get("Cache-Control") != "public, max-age=3600" {
				t.Fatalf("cabeceras de foto: %v", res.Header())
			}
			config, err := jpeg.DecodeConfig(bytes.NewReader(res.Body.Bytes()))
			if err != nil || config.Width != side || config.Height != side*3/4 {
				t.Fatalf("foto %s: %dx%d %v", size, config.Width, config.Height, err)
			}
		}
		request("GET", public+"/images/"+shirtPhoto+"?size=huge", "", nil, 404)
		request("GET", "/api/v1/public/storefronts/tienda-dos/images/"+shirtPhoto, "", nil, 404)
	})

	t.Run("Ocultar y agotados", func(t *testing.T) {
		request("PUT", settings+"/products/"+jacket["product"], `{"hidden":true}`, owner, 204)
		request("GET", public+"/products/"+jacket["product"], "", nil, 404)
		request("GET", public+"/images/"+jacketPhoto, "", nil, 404)
		request("PUT", settings, `{"enabled":true,"storeId":"`+one.StoreID+`","whatsapp":"5491155556666","title":"","welcome":"","showSoldOut":false}`, owner, 200)
		var current storefront.PublicStore
		if err := json.Unmarshal(request("GET", public, "", nil, 200).Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if len(current.Products) != 1 || current.Products[0].Name != "Remera Lino Hombre" || current.Name != "Tienda Uno" {
			t.Fatalf("sin ocultos ni agotados: %+v", current)
		}
		var view storefront.Admin
		if err := json.Unmarshal(request("GET", settings, "", owner, 200).Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.Summary.Visible != 1 || view.Summary.Hidden != 1 || view.Summary.SoldOut != 1 || view.Summary.NoPhoto != 1 {
			t.Fatalf("resumen: %+v", view.Summary)
		}
		request("PUT", settings+"/products/"+jacket["product"], `{"hidden":false}`, owner, 204)
		request("GET", public+"/products/"+jacket["product"], "", nil, 200)
	})

	t.Run("Sucursal o comercio inactivos", func(t *testing.T) {
		execSQL(t, ctx, db, "UPDATE stores SET status='inactive' WHERE id=$1", one.StoreID)
		request("GET", public, "", nil, 404)
		execSQL(t, ctx, db, "UPDATE stores SET status='active' WHERE id=$1", one.StoreID)
		execSQL(t, ctx, db, "UPDATE tenants SET status='inactive' WHERE id=$1", one.TenantID)
		request("GET", public, "", nil, 404)
		execSQL(t, ctx, db, "UPDATE tenants SET status='active' WHERE id=$1", one.TenantID)
		request("GET", public, "", nil, 200)
		request("GET", "/api/v1/public/storefronts/Mayusculas", "", nil, 404)
		request("GET", "/api/v1/public/storefronts/inexistente", "", nil, 404)
		request("PUT", settings, `{"enabled":false,"storeId":"`+one.StoreID+`","whatsapp":"5491155556666","title":"","welcome":"","showSoldOut":true}`, owner, 200)
		request("GET", public, "", nil, 404)
	})

	t.Run("Límite por cliente", func(t *testing.T) {
		limited := 0
		for range 80 {
			if raw("GET", "/api/v1/public/storefronts/inexistente", "", nil, map[string]string{"X-Forwarded-For": "203.0.113.9"}).Code == 429 {
				limited++
			}
		}
		if limited == 0 {
			t.Fatal("se esperaba el límite de consultas")
		}
	})
}
