package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/catalog"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/modules/purchases"
	"retail-platform/api/internal/modules/sales"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/config"
	"retail-platform/api/migrations"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHealthLive(t *testing.T) {
	router := New(config.Config{}, nil, nil)
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest("GET", "/api/health/live", nil))
	if result.Code != 200 || result.Body.String() != `{"status":"ok"}` {
		t.Fatalf("healthcheck: %d %s", result.Code, result.Body)
	}
}

func TestIdentityAcrossTenants(t *testing.T) {
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
	schema := "test_retail_" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
	for range 2 {
		if err := migrations.Apply(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	const password = "contraseña de prueba"
	initial := tenancy.Input{
		Slug: " UNO ", Name: "Uno", Timezone: "America/Argentina/Tucuman", Currency: "ars",
		StoreCode: "visible", StoreName: "Sucursal Uno",
		OwnerEmail: " PERSONA@EXAMPLE.TEST ", OwnerName: "Uno", Password: password,
	}
	created, err := tenancy.Provision(ctx, db, initial)
	if err != nil {
		t.Fatal(err)
	}
	if created.TenantID == "" || created.OwnerID == "" || created.StoreID == "" {
		t.Fatal("el alta debe devolver tres identificadores")
	}
	if _, err := tenancy.Provision(ctx, db, initial); !errors.Is(err, tenancy.ErrSlugTaken) {
		t.Fatalf("se esperaba un conflicto por slug: %v", err)
	}
	other := initial
	other.Slug, other.Name, other.OwnerName, other.StoreName = "dos", "Dos", "Dos", "Sucursal Dos"
	if _, err := tenancy.Provision(ctx, db, other); err != nil {
		t.Fatal(err)
	}
	invalid := initial
	invalid.Slug, invalid.Timezone = "invalido", "America/NoExiste"
	if _, err := tenancy.Provision(ctx, db, invalid); err == nil {
		t.Fatal("se aceptó una zona horaria inválida")
	}
	execSQL(t, ctx, db, "ALTER TABLE user_stores ADD CONSTRAINT test_reject_assignment CHECK (false) NOT VALID")
	rejected := initial
	rejected.Slug = "rollback"
	if _, err := tenancy.Provision(ctx, db, rejected); err == nil {
		t.Fatal("se esperaba un fallo al asignar la sucursal")
	}
	execSQL(t, ctx, db, "ALTER TABLE user_stores DROP CONSTRAINT test_reject_assignment")
	for _, table := range []string{"tenants", "stores", "users", "user_roles", "user_stores"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 2 {
			t.Fatalf("alta parcial o duplicada en %s: %d %v", table, count, err)
		}
	}
	execSQL(t, ctx, db, `INSERT INTO stores(tenant_id, code, name,status)
		SELECT id,'inactiva','Inactiva','inactive' FROM tenants WHERE slug='uno'`)
	execSQL(t, ctx, db, `INSERT INTO stores(tenant_id, code, name)
		SELECT id,'oculta','Sin asignar' FROM tenants WHERE slug='uno'`)
	execSQL(t, ctx, db, `INSERT INTO user_stores
		SELECT u.tenant_id,u.id,s.id FROM users u JOIN stores s ON s.tenant_id=u.tenant_id WHERE s.code='inactiva'`)
	_, err = db.Exec(ctx, `INSERT INTO user_stores
		SELECT u.tenant_id,u.id,s.id FROM users u CROSS JOIN stores s WHERE u.tenant_id<>s.tenant_id LIMIT 1`)
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != "23503" {
		t.Fatalf("se esperaba FK compuesta, se recibió %v", err)
	}
	service, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://retail.example"
	router := New(config.Config{WebOrigin: origin, Secure: true}, db, service)
	request := func(method, path, body, from string, cookie *http.Cookie, want int, mediaType ...string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			contentType := "application/json"
			if path == "/api/v1/catalog/csv/import" {
				contentType = "text/csv; charset=UTF-8"
			}
			if len(mediaType) > 0 {
				contentType = mediaType[0]
			}
			req.Header.Set("Content-Type", contentType)
		}
		if from != "" {
			req.Header.Set("Origin", from)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: esperado %d, recibido %d %s", method, path, want, res.Code, res.Body)
		}
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("falta Cache-Control: no-store")
		}
		return res
	}
	const login = "/api/v1/auth/login"
	const logout = "/api/v1/auth/logout"
	const me = "/api/v1/me"
	const stores = "/api/v1/stores"
	const credentials = `{"commerce":"uno","email":"persona@example.test","password":"contraseña de prueba"}`
	request("GET", "/api/health/ready", "", "", nil, 200)
	request("GET", me, "", "", nil, 401)
	request("POST", login, credentials, "https://other.example", nil, 403)
	request("POST", login, strings.Replace(credentials, password, "incorrecta", 1), origin, nil, 401)
	res := request("POST", login, credentials, origin, nil, 204)
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie insegura: %v", cookies)
	}
	cookie := cookies[0]
	second := request("POST", login, strings.Replace(credentials, `"uno"`, `"dos"`, 1), origin, nil, 204).Result().Cookies()[0]
	unscoped := strings.Replace(credentials, `"commerce":"uno",`, "", 1)
	choiceResponse := request("POST", login, unscoped, origin, nil, 200)
	var choice auth.LoginChoice
	if err := json.Unmarshal(choiceResponse.Body.Bytes(), &choice); err != nil || len(choice.Commerces) != 2 || choice.Token == "" ||
		len(choiceResponse.Result().Cookies()) != 0 {
		t.Fatalf("se deben ofrecer ambos comercios antes de emitir la sesión: %v", err)
	}
	const chooseCommerce = "/api/v1/auth/choose-commerce"
	selection := func(id string) string {
		return fmt.Sprintf(`{"token":%q,"commerceId":%q}`, choice.Token, id)
	}
	request("POST", chooseCommerce, selection(created.OwnerID), origin, nil, 401)
	selected := request("POST", chooseCommerce, selection(choice.Commerces[1].ID), origin, nil, 204).Result().Cookies()[0]
	if result := request("GET", stores, "", "", selected, 200).Body.String(); !strings.Contains(result, "Sucursal Uno") || strings.Contains(result, "Sucursal Dos") {
		t.Fatalf("la selección debe abrir solo el comercio elegido: %s", result)
	}
	request("POST", chooseCommerce, selection(choice.Commerces[0].ID), origin, nil, 401)
	request("GET", me, "", "", cookie, 200)
	result := request("GET", stores+"?tenant_id=dos", "", "", cookie, 200).Body.String()
	if !strings.Contains(result, "Sucursal Uno") || strings.Contains(result, "Sucursal Dos") ||
		strings.Contains(result, "Sin asignar") || strings.Contains(result, "Inactiva") {
		t.Fatalf("se filtraron o faltan sucursales: %s", result)
	}
	result = request("GET", stores, "", "", second, 200).Body.String()
	if !strings.Contains(result, "Sucursal Dos") || strings.Contains(result, "Sucursal Uno") {
		t.Fatalf("sesión del segundo comercio: %s", result)
	}
	const usersPath = "/api/v1/users"
	const managerPassword = "contraseña gerente de prueba"
	const staffPassword = "contraseña equipo de prueba"
	readID := func(result *httptest.ResponseRecorder) string {
		t.Helper()
		var body struct{ ID string }
		if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil || body.ID == "" {
			t.Fatalf("respuesta de usuario inválida: %s (%v)", result.Body, err)
		}
		return body.ID
	}
	create := func(email, role, password string, storeIDs ...string) string {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"name": email, "email": email, "role": role, "password": password, "storeIds": storeIDs,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	loginAs := func(email, password string) *http.Cookie {
		t.Helper()
		body, err := json.Marshal(map[string]string{"email": email, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		return request("POST", login, string(body), origin, nil, 204).Result().Cookies()[0]
	}
	var unassigned, otherStore string
	if err := db.QueryRow(ctx, "SELECT id FROM stores WHERE tenant_id=$1 AND code='oculta'", created.TenantID).Scan(&unassigned); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT id FROM stores WHERE tenant_id<>$1", created.TenantID).Scan(&otherStore); err != nil {
		t.Fatal(err)
	}
	managerBody := create("manager@example.test", "STORE_MANAGER", managerPassword, created.StoreID)
	request("POST", usersPath, managerBody, origin, cookie, 201)
	request("POST", usersPath, managerBody, origin, cookie, 409)
	manager := loginAs("manager@example.test", managerPassword)
	request("POST", usersPath, create("extra@example.test", "OWNER", staffPassword, created.StoreID), origin, manager, 403)
	request("POST", usersPath, create("extra@example.test", "STORE_MANAGER", staffPassword, created.StoreID), origin, manager, 403)
	request("POST", usersPath, create("extra@example.test", "CASHIER", staffPassword, unassigned), origin, manager, 403)
	request("POST", usersPath, create("extra@example.test", "CASHIER", staffPassword, otherStore), origin, manager, 403)
	multi := readID(request("POST", usersPath, create("multi@example.test", "CASHIER", staffPassword, created.StoreID, unassigned), origin, cookie, 201))
	staff := readID(request("POST", usersPath, create("equipo@example.test", "CASHIER", staffPassword, created.StoreID), origin, manager, 201))
	otherUser := readID(request("POST", usersPath, create("otro@example.test", "CASHIER", staffPassword, otherStore), origin, second, 201))
	request("PATCH", usersPath+"/"+otherUser, `{"status":"inactive"}`, origin, manager, 404)
	request("PATCH", usersPath+"/"+multi, `{"status":"inactive"}`, origin, manager, 404)
	request("PATCH", usersPath+"/"+created.OwnerID, `{"status":"inactive"}`, origin, manager, 404)
	request("PATCH", usersPath+"/"+staff, `{"storeIds":["`+unassigned+`"]}`, origin, manager, 403)
	request("PATCH", usersPath+"/"+staff, `{"status":"inactive"}`, "https://other.example", manager, 403)
	directory := request("GET", usersPath, "", "", manager, 200).Body.String()
	if !strings.Contains(directory, staff) || strings.Contains(directory, multi) || strings.Contains(directory, unassigned) {
		t.Fatalf("directorio fuera de alcance: %s", directory)
	}
	staffCookie := loginAs("equipo@example.test", staffPassword)
	request("GET", usersPath, "", "", staffCookie, 403)
	request("PATCH", usersPath+"/"+staff, `{"status":"inactive"}`, origin, manager, 200)
	request("GET", me, "", "", staffCookie, 401)
	request("PATCH", usersPath+"/"+staff, `{"status":"active"}`, origin, manager, 200)
	request("GET", me, "", "", staffCookie, 401)
	var auditCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM user_admin_events WHERE user_id=$1
		AND NOT (after_data ? 'password') AND NOT (after_data ? 'password_hash')`, staff).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("auditoría del usuario: %d %v", auditCount, err)
	}
	// Los permisos de configuración y cambio de rol pertenecen al propietario del comercio.
	const branchBody = `{"code":"NORTE","name":"Sucursal Norte","address":"Mitre 100"}`
	request("GET", stores+"/admin", "", "", manager, 403)
	request("POST", stores, branchBody, origin, manager, 403)
	request("PATCH", stores+"/"+created.StoreID, `{"status":"inactive"}`, origin, manager, 403)
	branch := readID(request("POST", stores, branchBody, origin, cookie, 201))
	request("POST", stores, branchBody, origin, cookie, 409)
	request("POST", stores, branchBody, origin, second, 201)
	request("PATCH", stores+"/"+otherStore, `{"status":"inactive"}`, origin, cookie, 404)
	ownerBranches := request("GET", stores+"/admin", "", "", cookie, 200).Body.String()
	if !strings.Contains(ownerBranches, "Mitre 100") || !strings.Contains(ownerBranches, "Inactiva") ||
		strings.Contains(ownerBranches, "Sucursal Dos") {
		t.Fatalf("listado administrativo incorrecto: %s", ownerBranches)
	}
	assigned := request("GET", stores, "", "", cookie, 200).Body.String()
	if !strings.Contains(assigned, branch) {
		t.Fatal("la sucursal nueva debe quedar asignada al propietario que la creó")
	}
	request("PATCH", stores+"/"+branch, `{"code":"visible"}`, origin, cookie, 409)
	request("PATCH", stores+"/"+branch, `{"address":""}`, origin, cookie, 200)
	request("PATCH", stores+"/"+created.StoreID, `{"status":"inactive"}`, origin, cookie, 200)
	if got := request("GET", stores, "", "", manager, 200).Body.String(); got != `{"items":[]}` {
		t.Fatalf("el encargado conserva acceso operativo a una sucursal inactiva: %s", got)
	}
	request("PATCH", usersPath+"/"+staff, `{"name":"Equipo"}`, origin, manager, 404)
	request("PATCH", stores+"/"+created.StoreID, `{"status":"active"}`, origin, cookie, 200)
	if got := request("GET", stores, "", "", manager, 200).Body.String(); !strings.Contains(got, created.StoreID) {
		t.Fatalf("la reactivación debe recuperar la asignación: %s", got)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM store_admin_events WHERE tenant_id=$1 AND store_id=$2 AND actor_id=$3`,
		created.TenantID, branch, created.OwnerID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("auditoría de sucursal: %d %v", auditCount, err)
	}

	request("PATCH", usersPath+"/"+staff, `{"role":"INVENTORY_OPERATOR"}`, origin, manager, 403)
	request("PATCH", usersPath+"/"+staff, `{"role":"OWNER"}`, origin, cookie, 403)
	request("PATCH", usersPath+"/"+created.OwnerID, `{"role":"CASHIER"}`, origin, cookie, 404)
	request("PATCH", usersPath+"/"+otherUser, `{"role":"STORE_MANAGER"}`, origin, cookie, 404)
	freshStaff := loginAs("equipo@example.test", staffPassword)
	result = request("PATCH", usersPath+"/"+staff, `{"role":"INVENTORY_OPERATOR"}`, origin, cookie, 200).Body.String()
	if !strings.Contains(result, `"roles":["INVENTORY_OPERATOR"]`) || !strings.Contains(result, created.StoreID) {
		t.Fatalf("rol o asignaciones incorrectas: %s", result)
	}
	request("GET", me, "", "", freshStaff, 401)
	request("GET", me, "", "", loginAs("equipo@example.test", staffPassword), 200)
	if err := db.QueryRow(ctx, `SELECT count(*) FROM user_admin_events WHERE user_id=$1 AND actor_id=$2
		AND before_data->'roles' @> '["CASHIER"]'::jsonb AND after_data->'roles' = '["INVENTORY_OPERATOR"]'::jsonb`,
		staff, created.OwnerID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("auditoría de cambio de rol: %d %v", auditCount, err)
	}
	const productsPath = "/api/v1/products"
	// Legacy catalog scenarios exercise business behavior with the current revision.
	// Version-specific tests bypass this helper and send an explicit precondition.
	catalogPatch := func(path, body, from string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		parts := strings.Split(path, "/")
		table := "products"
		if len(parts) == 7 {
			table = "product_variants"
		}
		var revision int64
		if err := db.QueryRow(ctx, "SELECT revision FROM "+table+" WHERE id=$1", parts[len(parts)-1]).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		if err := json.Unmarshal([]byte(body), &input); err != nil {
			t.Fatal(err)
		}
		input["expectedRevision"] = revision
		if table == "product_variants" {
			input["reason"] = "Corrección de fixture"
			input["confirmSensitive"] = true
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		return request("PATCH", path, string(encoded), from, cookie, want)
	}

	const lookupPath = "/api/v1/barcodes/000123/lookup"
	request("GET", productsPath, "", "", nil, 401)
	request("POST", productsPath, `{"name":"No autorizado"}`, origin, manager, 403)
	request("POST", productsPath, `{"name":" "}`, origin, cookie, 400)
	product := readID(request("POST", productsPath, `{"name":"Remera Básica","description":"Algodón"}`, origin, cookie, 201))
	otherProduct := readID(request("POST", productsPath, `{"name":"Remera Otro Comercio"}`, origin, second, 201))
	request("GET", productsPath+"/"+otherProduct, "", "", cookie, 404)
	catalogPatch(productsPath+"/"+otherProduct, `{"name":"Cruzado"}`, origin, cookie, 404)
	variantInput := `{"sku":"REM-M-NEGRO","size":"M","color":"Negro","barcodes":["000123","LOCAL-1"]}`
	request("POST", productsPath+"/"+otherProduct+"/variants", variantInput, origin, cookie, 404)
	request("POST", productsPath+"/"+product+"/variants", variantInput, origin, manager, 403)
	request("GET", lookupPath, "", "", second, 404)
	variantID := readID(request("POST", productsPath+"/"+product+"/variants", variantInput, origin, cookie, 201))
	request("POST", productsPath+"/"+product+"/variants", `{"sku":"rem-m-negro"}`, origin, cookie, 409)
	request("POST", productsPath+"/"+product+"/variants", `{"sku":"ROLLBACK","barcodes":["000123"]}`, origin, cookie, 409)
	spare := readID(request("POST", productsPath+"/"+product+"/variants", `{"sku":"ROLLBACK"}`, origin, cookie, 201))
	otherVariantID := readID(request("POST", productsPath+"/"+otherProduct+"/variants", variantInput, origin, second, 201))
	match := request("GET", lookupPath, "", "", manager, 200).Body.String()
	if !strings.Contains(match, "Remera Básica") || !strings.Contains(match, "000123") ||
		!strings.Contains(match, `"barcodes":["000123","LOCAL-1"]`) || strings.Contains(match, "Remera Otro Comercio") {
		t.Fatalf("la búsqueda no conserva el código o el comercio: %s", match)
	}
	otherMatch := request("GET", lookupPath, "", "", second, 200).Body.String()
	if !strings.Contains(otherMatch, "Remera Otro Comercio") || strings.Contains(otherMatch, "Remera Básica") {
		t.Fatalf("búsqueda cruzada: %s", otherMatch)
	}
	listed := request("GET", productsPath+"?q=rem-m-negro&limit=1", "", "", manager, 200).Body.String()
	if !strings.Contains(listed, product) || strings.Contains(listed, otherProduct) || !strings.Contains(listed, `"canManage":false`) {
		t.Fatalf("búsqueda por SKU: %s", listed)
	}
	request("GET", productsPath+"?limit=101", "", "", cookie, 400)
	detail := request("GET", productsPath+"/"+product, "", "", cookie, 200).Body.String()
	if !strings.Contains(detail, `"barcodes":[]`) || !strings.Contains(detail, "ROLLBACK") {
		t.Fatalf("detalle de variante sin código: %s", detail)
	}
	catalogPatch(productsPath+"/"+product, `{"status":"inactive"}`, origin, cookie, 200)
	request("GET", lookupPath, "", "", cookie, 404)
	request("GET", productsPath+"/"+product, "", "", manager, 404)
	request("GET", productsPath+"/"+product, "", "", cookie, 200)
	catalogPatch(productsPath+"/"+product, `{"status":"active"}`, origin, cookie, 200)
	request("GET", lookupPath, "", "", manager, 200)
	_, err = db.Exec(ctx, `INSERT INTO product_variants(tenant_id,product_id,sku) VALUES ($1,$2,'CROSS')`, created.TenantID, otherProduct)
	if !errors.As(err, &constraint) || constraint.Code != "23503" {
		t.Fatalf("se esperaba FK de catálogo por comercio, se recibió %v", err)
	}

	const classifications = "/api/v1/catalog/classifications"
	const categoryPath = "/api/v1/categories"
	const brandPath = "/api/v1/brands"
	request("GET", classifications, "", "", nil, 401)
	request("POST", categoryPath, `{"name":"Remeras"}`, origin, manager, 403)
	category := readID(request("POST", categoryPath, `{"name":"Remeras"}`, origin, cookie, 201))
	request("POST", categoryPath, `{"name":" rEMERas "}`, origin, cookie, 409)
	otherCategory := readID(request("POST", categoryPath, `{"name":"Remeras"}`, origin, second, 201))
	brand := readID(request("POST", brandPath, `{"name":"Marca Local"}`, origin, cookie, 201))
	otherBrand := readID(request("POST", brandPath, `{"name":"Marca Ajena"}`, origin, second, 201))
	request("PATCH", categoryPath+"/"+otherCategory, `{"name":"Ajena"}`, origin, cookie, 404)
	request("PATCH", categoryPath+"/"+category, `{"name":"Básicos"}`, origin, cookie, 200)
	for _, tc := range []struct{ path, own, foreign string }{
		{categoryPath, category, otherCategory}, {brandPath, brand, otherBrand},
	} {
		request("GET", tc.path, "", "", nil, 401)
		body := request("GET", tc.path, "", "", manager, 200).Body.String()
		if !strings.Contains(body, tc.own) || strings.Contains(body, tc.foreign) {
			t.Fatalf("clasificación con alcance incorrecto en %s: %s", tc.path, body)
		}
	}
	values := request("GET", classifications, "", "", manager, 200).Body.String()
	if !strings.Contains(values, "Básicos") || !strings.Contains(values, brand) || strings.Contains(values, otherBrand) {
		t.Fatalf("clasificaciones cruzadas o desactualizadas: %s", values)
	}
	request("POST", productsPath, `{"name":"Cruce","categoryId":"`+otherCategory+`"}`, origin, cookie, 404)
	catalogPatch(productsPath+"/"+product, `{"brandId":"`+otherBrand+`"}`, origin, cookie, 404)
	catalogPatch(productsPath+"/"+product, `{"brandId":"incorrecto"}`, origin, cookie, 400)
	catalogPatch(productsPath+"/"+product, `{"categoryId":"`+category+`","brandId":"`+brand+`"}`, origin, cookie, 200)
	combined := request("GET", productsPath+"?categoryId="+category+"&brandId="+brand+"&q=rem-m-negro", "", "", manager, 200).Body.String()
	if !strings.Contains(combined, product) || strings.Contains(combined, otherProduct) {
		t.Fatalf("filtro combinado incorrecto: %s", combined)
	}
	result = request("GET", lookupPath, "", "", manager, 200).Body.String()
	if !strings.Contains(result, category) || !strings.Contains(result, brand) {
		t.Fatalf("faltan las clasificaciones al buscar un código: %s", result)
	}
	cleared := catalogPatch(productsPath+"/"+product, `{"categoryId":null}`, origin, cookie, 200).Body.String()
	if !strings.Contains(cleared, `"categoryId":null`) || !strings.Contains(cleared, brand) {
		t.Fatalf("quitar categoría debe conservar la marca: %s", cleared)
	}
	filtered := request("GET", productsPath+"?categoryId="+category, "", "", cookie, 200).Body.String()
	if !strings.Contains(filtered, `"items":[]`) {
		t.Fatalf("la categoría quitada sigue afectando el filtro: %s", filtered)
	}
	_, err = db.Exec(ctx, "UPDATE products SET brand_id=$1 WHERE id=$2", otherBrand, product)
	if !errors.As(err, &constraint) || constraint.Code != "23503" {
		t.Fatalf("se esperaba FK de marca por comercio, se recibió %v", err)
	}

	const prices = "/api/v1/price-lists"
	resolveAt := func(storeID, itemID string) string {
		return stores + "/" + storeID + "/variants/" + itemID + "/price"
	}
	itemPath := func(listID, itemID string) string {
		return prices + "/" + listID + "/items/" + itemID
	}
	assignAt := func(storeID string) string { return stores + "/" + storeID + "/price-list" }
	assignBody := func(listID string) string { return `{"listId":"` + listID + `"}` }
	checkPrice := func(result *httptest.ResponseRecorder, reason string, amount *string) {
		t.Helper()
		var resolved struct {
			Amount   *string
			Reason   string
			Currency string
		}
		if err := json.Unmarshal(result.Body.Bytes(), &resolved); err != nil ||
			resolved.Reason != reason || resolved.Currency != "ARS" ||
			(resolved.Amount == nil) != (amount == nil) ||
			(amount != nil && resolved.Amount != nil && *resolved.Amount != *amount) {
			t.Fatalf("precio inesperado %s (%v)", result.Body, err)
		}
	}
	centralPrice := resolveAt(created.StoreID, variantID)
	nortePrice := resolveAt(branch, variantID)
	request("GET", prices, "", "", manager, 403)
	request("POST", prices, `{"name":"General"}`, origin, manager, 403)
	request("GET", centralPrice, "", "", nil, 401)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "no_list", nil)
	general := readID(request("POST", prices, `{"name":"General"}`, origin, cookie, 201))
	norte := readID(request("POST", prices, `{"name":"Norte"}`, origin, cookie, 201))
	request("POST", prices, `{"name":"general"}`, origin, cookie, 409)
	otherList := readID(request("POST", prices, `{"name":"General"}`, origin, second, 201))
	request("PUT", assignAt(created.StoreID), assignBody(otherList), origin, cookie, 404)
	request("PUT", assignAt(otherStore), assignBody(general), origin, cookie, 404)
	request("PUT", assignAt(created.StoreID), assignBody(general), origin, manager, 403)
	request("PUT", assignAt(created.StoreID), assignBody(general), "https://other.example", cookie, 403)
	request("PUT", assignAt(created.StoreID), `{}`, origin, cookie, 400)
	request("PUT", assignAt(created.StoreID), assignBody(general), origin, cookie, 204)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "no_price", nil)
	request("PUT", assignAt(branch), assignBody(norte), origin, cookie, 204)
	request("PUT", itemPath(general, variantID), `{"amount":12.30}`, origin, cookie, 400)
	request("PUT", itemPath(general, variantID), `{"amount":"12.301"}`, origin, cookie, 400)
	request("PUT", itemPath(general, variantID), `{"amount":"-1"}`, origin, cookie, 400)
	request("PUT", itemPath(general, variantID), `{"amount":"10000000000000000.00"}`, origin, cookie, 400)
	request("PUT", itemPath(general, variantID), `{}`, origin, cookie, 400)
	request("PUT", itemPath(otherList, variantID), `{"amount":"12.30"}`, origin, cookie, 404)
	request("PUT", itemPath(general, otherVariantID), `{"amount":"12.30"}`, origin, cookie, 404)
	maxAmount := "9999999999999999.99"
	request("PUT", itemPath(general, variantID), `{"amount":"`+maxAmount+`"}`, origin, cookie, 200)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "configured", &maxAmount)
	checkPrice(request("GET", nortePrice, "", "", cookie, 200), "no_price", nil)
	request("GET", nortePrice, "", "", manager, 404)
	request("GET", resolveAt(created.StoreID, otherVariantID), "", "", cookie, 404)
	request("PUT", assignAt(branch), assignBody(general), origin, cookie, 204)
	checkPrice(request("GET", nortePrice, "", "", cookie, 200), "configured", &maxAmount)
	zero := "0.00"
	request("PUT", itemPath(general, variantID), `{"amount":"0"}`, origin, cookie, 200)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "configured", &zero)
	request("PUT", itemPath(general, variantID), `{"amount":null}`, origin, cookie, 200)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "no_price", nil)
	request("PUT", assignAt(branch), `{"listId":null}`, origin, cookie, 204)
	checkPrice(request("GET", nortePrice, "", "", cookie, 200), "no_list", nil)
	request("GET", itemPath(general, variantID), "", "", cookie, 200)
	request("PATCH", stores+"/"+branch, `{"status":"inactive"}`, origin, cookie, 200)
	request("GET", nortePrice, "", "", cookie, 404)
	request("PATCH", stores+"/"+branch, `{"status":"active"}`, origin, cookie, 200)
	config := request("GET", prices, "", "", cookie, 200).Body.String()
	if !strings.Contains(config, general) || !strings.Contains(config, branch) || strings.Contains(config, otherList) {
		t.Fatalf("configuración cruzada de listas: %s", config)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM price_admin_events WHERE tenant_id=$1 AND variant_id=$2 AND action='set_price'`,
		created.TenantID, variantID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("auditoría de precios: %d %v", auditCount, err)
	}
	variantPath := productsPath + "/" + product + "/variants/" + variantID
	catalogPatch(variantPath, `{"sku":"DENIED"}`, origin, manager, 403)
	catalogPatch(productsPath+"/"+otherProduct+"/variants/"+variantID, `{"size":"X"}`, origin, cookie, 404)
	catalogPatch(productsPath+"/"+product+"/variants/"+otherVariantID, `{"size":"X"}`, origin, cookie, 404)
	catalogPatch(variantPath, `{"barcodes":null}`, origin, cookie, 400)
	catalogPatch(variantPath, `{"sku":"rollback"}`, origin, cookie, 409)
	catalogPatch(productsPath+"/"+product+"/variants/"+spare, `{"barcodes":["TAKEN-1"]}`, origin, cookie, 200)
	catalogPatch(variantPath, `{"sku":"CAMBIO-REVERTIDO","barcodes":["TAKEN-1"]}`, origin, cookie, 409)
	result = request("GET", lookupPath, "", "", manager, 200).Body.String()
	if !strings.Contains(result, "REM-M-NEGRO") || strings.Contains(result, "CAMBIO-REVERTIDO") {
		t.Fatalf("el error de código debe revertir toda la variante: %s", result)
	}
	newAmount := "25.40"
	request("PUT", itemPath(general, variantID), `{"amount":"25.40"}`, origin, cookie, 200)
	result = catalogPatch(variantPath, `{"sku":"REM-EDITADO","size":"L","color":"Azul","barcodes":["LOCAL-1","000124"]}`, origin, cookie, 200).Body.String()
	if !strings.Contains(result, variantID) || !strings.Contains(result, `"barcodes":["LOCAL-1","000124"]`) ||
		!strings.Contains(result, `"color":"Azul"`) || !strings.Contains(result, `"size":"L"`) {
		t.Fatalf("cambio de variante incorrecto: %s", result)
	}
	request("GET", lookupPath, "", "", manager, 404)
	request("GET", lookupPath, "", "", second, 200)
	newLookup := "/api/v1/barcodes/000124/lookup"
	request("GET", newLookup, "", "", manager, 200)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "configured", &newAmount)
	catalogPatch(variantPath, `{"status":"inactive"}`, origin, cookie, 200)
	request("GET", newLookup, "", "", cookie, 404)
	request("GET", centralPrice, "", "", manager, 404)
	catalogPatch(productsPath+"/"+product+"/variants/"+spare, `{"barcodes":["000124"]}`, origin, cookie, 409)
	result = request("GET", productsPath+"/"+product, "", "", manager, 200).Body.String()
	if strings.Contains(result, variantID) {
		t.Fatalf("la variante inactiva debe ocultarse en operación: %s", result)
	}
	result = request("GET", productsPath+"?q=REM-EDITADO", "", "", manager, 200).Body.String()
	if !strings.Contains(result, `"items":[]`) {
		t.Fatalf("el SKU inactivo se encontró en la búsqueda operativa: %s", result)
	}
	catalogPatch(variantPath, `{"status":"active"}`, origin, cookie, 200)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "configured", &newAmount)
	request("GET", newLookup, "", "", manager, 200)
	catalogPatch(variantPath, `{"barcodes":[]}`, origin, cookie, 200)
	request("GET", newLookup, "", "", manager, 404)
	checkPrice(request("GET", centralPrice, "", "", manager, 200), "configured", &newAmount)

	const csvImport = "/api/v1/catalog/csv/import"
	const csvExport = "/api/v1/catalog/csv/export"
	const csvTemplate = "/api/v1/catalog/csv/template"
	const csvHead = "product_ref,name,description,category,brand,product_status,sku,size,color,variant_status,barcodes\n"
	template := request("GET", csvTemplate, "", "", cookie, 200)
	if !strings.HasPrefix(template.Body.String(), "\ufeff"+csvHead) ||
		!strings.HasPrefix(template.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("plantilla incorrecta: %s", template.Body)
	}
	request("GET", csvExport, "", "", manager, 403)
	request("POST", csvImport, template.Body.String(), origin, manager, 403)
	request("POST", csvImport, "name,sku\n", origin, cookie, 400)
	request("POST", csvImport, csvHead+"test,Prueba,,,,active,SKU-1", origin, cookie, 400)
	request("POST", csvImport, csvHead+"test,Prueba,,Ajena,,active,,,,,\n", origin, cookie, 400)
	request("POST", csvImport, csvHead+strings.Repeat("a", 2<<20), origin, cookie, 413)
	badBatch := csvHead +
		"lote,Importación fallida,,,,active,CSV-ROLLBACK,M,Azul,active,CSV-900\n" +
		"lote,Importación fallida,,,,active,REM-EDITADO,L,Azul,active,CSV-901\n"
	failed := request("POST", csvImport, badBatch, origin, cookie, 409).Body.String()
	if !strings.Contains(failed, "Línea 3") {
		t.Fatalf("falta el número de línea del conflicto: %s", failed)
	}
	request("GET", "/api/v1/barcodes/CSV-900/lookup", "", "", cookie, 404)
	if result := request("GET", productsPath+"?q=CSV-ROLLBACK", "", "", cookie, 200).Body.String(); !strings.Contains(result, `"items":[]`) {
		t.Fatalf("la importación fallida dejó un producto: %s", result)
	}
	goodBatch := "\ufeff" + strings.ReplaceAll(csvHead, ",", ";") +
		"lote;Importación CSV;=1+1;Básicos;Marca Local;active;CSV-ROLLBACK;M;Azul;active;000901|CSV-ALT\n" +
		"lote;Importación CSV;=1+1;Básicos;Marca Local;active;CSV-INACTIVA;L;Azul;inactive;000902\n" +
		"solo;Producto sin variantes;;;;inactive;;;;;\n"
	imported := request("POST", csvImport, goodBatch, origin, cookie, 201)
	if imported.Body.String() != `{"products":2,"variants":2}` {
		t.Fatalf("recuento de importación incorrecto: %s", imported.Body)
	}
	csvMatch := request("GET", "/api/v1/barcodes/000901/lookup", "", "", manager, 200).Body.String()
	if !strings.Contains(csvMatch, category) || !strings.Contains(csvMatch, brand) || !strings.Contains(csvMatch, "CSV-ROLLBACK") {
		t.Fatalf("faltan datos importados: %s", csvMatch)
	}
	request("GET", "/api/v1/barcodes/000902/lookup", "", "", manager, 404)
	export := request("GET", csvExport, "", "", cookie, 200)
	if !strings.HasPrefix(export.Body.String(), "\ufeff"+csvHead) ||
		!strings.Contains(export.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(export.Body.String(), "'=1+1") || !strings.Contains(export.Body.String(), "'000902") ||
		!strings.Contains(export.Body.String(), "Producto sin variantes") ||
		strings.Contains(export.Body.String(), "Remera Otro Comercio") {
		t.Fatalf("CSV exportado inválido: %s", export.Body)
	}
	request("POST", csvImport, csvHead+strings.Repeat("x,Producto,,,,active,,,,,\n", 1001), origin, cookie, 400)
	var limitRows strings.Builder
	limitRows.WriteString(csvHead)
	for i := range 1001 {
		fmt.Fprintf(&limitRows, "row-%d,Producto,,,,active,,,,,\n", i)
	}
	request("POST", csvImport, limitRows.String(), origin, cookie, 413)

	photoPath := productsPath + "/" + product + "/images"
	otherPhotoPath := productsPath + "/" + otherProduct + "/images"
	var pngImage bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 12, 7))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 255})
	if err := png.Encode(&pngImage, picture); err != nil {
		t.Fatal(err)
	}
	const trailingMetadata = "prueba de metadatos originales"
	source := pngImage.String() + trailingMetadata
	request("POST", photoPath, source, origin, manager, 403, "image/png")
	request("POST", photoPath, source, origin, cookie, 400, "image/jpeg")
	request("POST", photoPath, source, origin, cookie, 415, "image/svg+xml")
	request("POST", photoPath, "imagen inválida", origin, cookie, 400, "image/png")
	request("POST", photoPath, strings.Repeat("a", (5<<20)+1), origin, cookie, 413, "image/jpeg")
	request("POST", otherPhotoPath, source, origin, cookie, 404, "image/png")
	readPhotos := func(recorder *httptest.ResponseRecorder) []struct {
		ID      string
		Width   int
		Height  int
		Primary bool
	} {
		t.Helper()
		var result struct {
			Images []struct {
				ID      string
				Width   int
				Height  int
				Primary bool
			}
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.Images == nil {
			t.Fatalf("respuesta de fotos inválida: %s (%v)", recorder.Body, err)
		}
		return result.Images
	}
	images := readPhotos(request("POST", photoPath, source, origin, cookie, 201, "image/png"))
	if len(images) != 1 || !images[0].Primary || images[0].Width != 12 || images[0].Height != 7 {
		t.Fatalf("la primera foto debe quedar como principal: %+v", images)
	}
	firstPhoto := images[0].ID
	images = readPhotos(request("POST", photoPath, source, origin, cookie, 201, "image/png"))
	if len(images) != 2 || images[0].ID != firstPhoto || images[1].Primary {
		t.Fatalf("orden inicial de fotos incorrecto: %+v", images)
	}
	secondPhoto := images[1].ID
	firstImageURL := photoPath + "/" + firstPhoto
	secondImageURL := photoPath + "/" + secondPhoto
	request("GET", firstImageURL, "", "", nil, 401)
	request("GET", firstImageURL, "", "", second, 404)
	request("GET", otherPhotoPath+"/"+firstPhoto, "", "", cookie, 404)
	firstImage := request("GET", firstImageURL, "", "", manager, 200)
	if firstImage.Header().Get("Content-Type") != "image/jpeg" || bytes.Contains(firstImage.Body.Bytes(), []byte(trailingMetadata)) {
		t.Fatal("la respuesta debe servir JPEG reencodificado")
	}
	cfgImage, err := jpeg.DecodeConfig(firstImage.Body)
	if err != nil || cfgImage.Width != 12 || cfgImage.Height != 7 {
		t.Fatalf("JPEG de salida incorrecto: %+v (%v)", cfgImage, err)
	}
	request("PUT", secondImageURL+"/primary", "", origin, manager, 403)
	images = readPhotos(request("PUT", secondImageURL+"/primary", "", origin, cookie, 200))
	if len(images) != 2 || images[0].ID != secondPhoto || !images[0].Primary || images[1].Primary {
		t.Fatalf("orden de fotos incorrecto: %+v", images)
	}
	detail = request("GET", productsPath+"/"+product, "", "", manager, 200).Body.String()
	if !strings.Contains(detail, secondPhoto) || strings.Contains(detail, "jpeg_data") {
		t.Fatalf("el detalle debe incluir solo metadatos de las fotos: %s", detail)
	}
	catalogPatch(productsPath+"/"+product, `{"status":"inactive"}`, origin, cookie, 200)
	request("GET", firstImageURL, "", "", manager, 404)
	request("GET", firstImageURL, "", "", cookie, 200)
	catalogPatch(productsPath+"/"+product, `{"status":"active"}`, origin, cookie, 200)
	request("DELETE", secondImageURL, "", "https://other.example", cookie, 403)
	request("DELETE", secondImageURL, "", origin, manager, 403)
	images = readPhotos(request("DELETE", secondImageURL, "", origin, cookie, 200))
	if len(images) != 1 || images[0].ID != firstPhoto || !images[0].Primary {
		t.Fatalf("al quitar la principal debe promoverse la restante: %+v", images)
	}
	request("GET", secondImageURL, "", "", cookie, 404)
	for range 5 {
		request("POST", photoPath, source, origin, cookie, 201, "image/png")
	}
	request("POST", photoPath, source, origin, cookie, 409, "image/png")

	const locationsPath = "/api/v1/locations"
	const adjustmentsPath = "/api/v1/inventory/adjustments"
	locBody := func(storeID, code string) string {
		return `{"storeId":"` + storeID + `","code":"` + code + `","name":"Depósito","type":"warehouse","isSellable":false}`
	}
	listAt := locationsPath + "?store_id=" + created.StoreID
	request("GET", listAt, "", "", nil, 401)
	request("GET", locationsPath+"?store_id="+branch, "", "", manager, 404)
	request("GET", locationsPath+"?store_id="+otherStore, "", "", cookie, 404)
	request("POST", locationsPath, locBody(created.StoreID, "DEP"), origin, manager, 403)
	location := readID(request("POST", locationsPath, locBody(created.StoreID, "DEP"), origin, cookie, 201))
	request("POST", locationsPath, locBody(created.StoreID, "dep"), origin, cookie, 409)
	northLocation := readID(request("POST", locationsPath, locBody(branch, "DEP"), origin, cookie, 201))
	request("POST", locationsPath, locBody(otherStore, "DEP"), origin, second, 201)
	locs := request("GET", listAt, "", "", manager, 200).Body.String()
	if !strings.Contains(locs, location) || strings.Contains(locs, northLocation) ||
		!strings.Contains(locs, `"canAdjust":true`) || !strings.Contains(locs, `"canManage":false`) {
		t.Fatalf("ubicaciones o permisos incorrectos: %s", locs)
	}
	balances := "/api/v1/inventory?store_id=" + created.StoreID + "&location_id=" + location
	valuationPath := "/api/v1/reports/inventory?store_id=" + created.StoreID
	valuationCSVPath := "/api/v1/reports/inventory/csv?store_id=" + created.StoreID
	readValuationCSV := func(actor *http.Cookie) [][]string {
		t.Helper()
		response := request("GET", valuationCSVPath, "", "", actor, 200)
		if response.Header().Get("Content-Disposition") != `attachment; filename="valuacion-inventario.csv"` ||
			response.Header().Get("Content-Type") != csvContentType {
			t.Fatalf("encabezados inesperados para la valuación: %v", response.Header())
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	readValuation := func(actor *http.Cookie) inventory.Valuation {
		t.Helper()
		var result inventory.Valuation
		response := request("GET", valuationPath, "", "", actor, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	locationValuation := func(value inventory.Valuation, id string) inventory.LocationValuation {
		t.Helper()
		for _, row := range value.Locations {
			if row.LocationID == id {
				return row
			}
		}
		t.Fatalf("falta la ubicación %s en el desglose: %+v", id, value.Locations)
		return inventory.LocationValuation{}
	}
	request("GET", valuationPath, "", "", nil, 401)
	request("GET", valuationPath, "", "", second, 404)
	request("GET", valuationCSVPath, "", "", nil, 401)
	request("GET", valuationCSVPath, "", "", second, 404)
	request("GET", "/api/v1/reports/inventory?store_id="+unassigned, "", "", manager, 404)
	if value := readValuation(manager); value.Currency != "ARS" || value.UnitsWithCost != 0 || value.UnitsWithoutCost != 0 ||
		value.KnownValue != nil || value.TotalValue == nil || *value.TotalValue != "0.00" {
		t.Fatalf("una sucursal sin stock debe tener valor total cero: %+v", value)
	}
	if row := locationValuation(readValuation(manager), location); row.Code != "DEP" ||
		row.KnownValue != nil || row.TotalValue == nil || *row.TotalValue != "0.00" {
		t.Fatalf("una ubicación vacía debe figurar con valuación cero: %+v", row)
	}
	if rows := readValuationCSV(manager); len(rows) < 3 || rows[1][0] != "sucursal" || rows[1][1] != created.StoreID ||
		rows[1][7] != "" || rows[1][8] != "0.00" {
		t.Fatalf("el CSV debe incluir el resumen de la sucursal vacía con valor cero: %v", rows)
	}
	history := "/api/v1/inventory/movements?store_id=" + created.StoreID + "&location_id=" + location
	request("GET", "/api/v1/inventory?store_id="+created.StoreID+"&location_id="+northLocation, "", "", cookie, 404)
	if body := request("GET", balances, "", "", manager, 200).Body.String(); body != `{"items":[]}` {
		t.Fatalf("el saldo inicial debe estar vacío: %s", body)
	}
	adjustBody := func(storeID, locationID, variantID string, delta int, reason string) string {
		data, err := json.Marshal(map[string]any{"storeId": storeID, "locationId": locationID,
			"variantId": variantID, "delta": delta, "reason": reason})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	adjust := func(body, key string, actor *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", adjustmentsPath, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req.AddCookie(actor)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expect := func(res *httptest.ResponseRecorder, status int) *httptest.ResponseRecorder {
		t.Helper()
		if res.Code != status {
			t.Fatalf("ajuste: esperado %d, recibido %d %s", status, res.Code, res.Body)
		}
		return res
	}
	receipt := adjustBody(created.StoreID, location, variantID, 5, "Inventario inicial")
	expect(adjust(receipt, "", manager), 400)
	expect(adjust(adjustBody(created.StoreID, northLocation, variantID, 1, "Cruce de ubicación"), "location-cross", cookie), 404)
	expect(adjust(adjustBody(created.StoreID, location, otherVariantID, 1, "Cruce de variante"), "variant-cross", cookie), 404)
	cashier := loginAs("multi@example.test", staffPassword)
	expect(adjust(receipt, "cashier-denied", cashier), 403)
	original := expect(adjust(receipt, "receipt-1", manager), 201)
	movementID := readID(original)
	replay := expect(adjust(receipt, "receipt-1", manager), 201)
	if original.Body.String() != replay.Body.String() {
		t.Fatal("el reintento debe devolver el movimiento original")
	}
	expect(adjust(receipt, "receipt-1", cookie), 409)
	expect(adjust(adjustBody(created.StoreID, location, variantID, 6, "Inventario inicial"), "receipt-1", manager), 409)
	expect(adjust(adjustBody(created.StoreID, location, variantID, -6, "Stock insuficiente"), "too-many", manager), 409)
	execSQL(t, ctx, db, "UPDATE stock_balances SET quantity_reserved=3 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, location, variantID)
	expect(adjust(adjustBody(created.StoreID, location, variantID, -3, "Intento sobre reservado"), "reserved-guard", manager), 409)
	reserved := request("GET", balances, "", "", cashier, 200).Body.String()
	if !strings.Contains(reserved, `"onHand":5,"reserved":3,"available":2`) {
		t.Fatalf("saldo reservado incorrecto: %s", reserved)
	}
	execSQL(t, ctx, db, "UPDATE stock_balances SET quantity_reserved=0 WHERE tenant_id=$1 AND location_id=$2", created.TenantID, location)
	withdrawal := adjustBody(created.StoreID, location, variantID, -4, "Egreso")
	concurrent := make(chan int, 2)
	for _, key := range []string{"race-1", "race-2"} {
		go func() { concurrent <- adjust(withdrawal, key, manager).Code }()
	}
	firstStatus, secondStatus := <-concurrent, <-concurrent
	if !((firstStatus == 201 && secondStatus == 409) || (firstStatus == 409 && secondStatus == 201)) {
		t.Fatalf("dos egresos concurrentes: %d y %d", firstStatus, secondStatus)
	}
	after := request("GET", balances, "", "", manager, 200).Body.String()
	if !strings.Contains(after, `"onHand":1,"reserved":0,"available":1`) {
		t.Fatalf("saldo después de concurrencia: %s", after)
	}
	historyBody := request("GET", history, "", "", cashier, 200).Body.String()
	if strings.Count(historyBody, `"type":"adjustment"`) != 2 || !strings.Contains(historyBody, movementID) {
		t.Fatalf("el historial contiene movimientos duplicados o inválidos: %s", historyBody)
	}
	if _, err := db.Exec(ctx, "UPDATE stock_movements SET reason='alterado' WHERE id=$1", movementID); err == nil {
		t.Fatal("se pudo editar un movimiento confirmado")
	}
	if _, err := db.Exec(ctx, "DELETE FROM stock_movements WHERE id=$1", movementID); err == nil {
		t.Fatal("se pudo borrar un movimiento confirmado")
	}
	_, err = db.Exec(ctx, `INSERT INTO stock_balances(tenant_id,location_id,variant_id)
		VALUES ($1,$2,$3)`, created.TenantID, location, otherVariantID)
	if !errors.As(err, &constraint) || constraint.Code != "23503" {
		t.Fatalf("se esperaba FK compuesta de stock: %v", err)
	}

	const transfersPath = "/api/v1/transfers"
	transferStore := func(id string) string { return `{"storeId":"` + id + `"}` }
	transferDraft := func(to string, items string) string {
		return `{"storeId":"` + created.StoreID + `","originLocationId":"` + location +
			`","destinationLocationId":"` + to + `","items":[` + items + `]}`
	}
	transferItem := func(id string, quantity int) string {
		return fmt.Sprintf(`{"variantId":"%s","quantity":%d}`, id, quantity)
	}
	openTransfer := func(body, key string, actor *http.Cookie, want int) string {
		t.Helper()
		req := httptest.NewRequest("POST", transfersPath, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("Idempotency-Key", key)
		req.AddCookie(actor)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("alta de transferencia: esperado %d, recibido %d %s", want, res.Code, res.Body)
		}
		if want == 201 {
			return readID(res)
		}
		return ""
	}
	recipientBalances := "/api/v1/inventory?store_id=" + branch + "&location_id=" + northLocation
	var foreignLocation string
	if err := db.QueryRow(ctx, "SELECT id FROM stock_locations WHERE tenant_id<>$1", created.TenantID).Scan(&foreignLocation); err != nil {
		t.Fatal(err)
	}
	openTransfer(transferDraft(foreignLocation, transferItem(variantID, 1)), "cross-destination", manager, 404)
	openTransfer(transferDraft(northLocation, transferItem(otherVariantID, 1)), "cross-transfer-variant", manager, 404)
	openTransfer(transferDraft(location, transferItem(variantID, 1)), "same-location", manager, 400)
	openTransfer(transferDraft(northLocation, transferItem(variantID, 1)), "", manager, 400)
	openTransfer(transferDraft(northLocation, transferItem(variantID, 1)), "read-only", cashier, 403)
	expect(adjust(adjustBody(created.StoreID, location, variantID, 9, "Ingreso para transferencias"), "transfer-stock", manager), 201)
	broken := openTransfer(transferDraft(northLocation, transferItem(variantID, 2)+","+transferItem(spare, 1)), "broken-transfer", manager, 201)
	request("POST", transfersPath+"/"+broken+"/dispatch", transferStore(created.StoreID), origin, manager, 409)
	balanceBeforeTransfer := request("GET", balances, "", "", manager, 200).Body.String()
	if !strings.Contains(balanceBeforeTransfer, `"onHand":10`) {
		t.Fatalf("un despacho fallido dejó efectos parciales: %s", balanceBeforeTransfer)
	}
	request("POST", transfersPath+"/"+broken+"/cancel", transferStore(created.StoreID), origin, manager, 200)
	request("POST", transfersPath+"/"+broken+"/cancel", transferStore(created.StoreID), origin, manager, 200)
	request("POST", transfersPath+"/"+broken+"/dispatch", transferStore(created.StoreID), origin, manager, 409)

	payload := transferDraft(northLocation, transferItem(variantID, 4))
	transferID := openTransfer(payload, "cross-branch", manager, 201)
	if id := openTransfer(payload, "cross-branch", manager, 201); id != transferID {
		t.Fatal("el reintento de creación duplicó el borrador")
	}
	openTransfer(transferDraft(northLocation, transferItem(variantID, 5)), "cross-branch", manager, 409)
	link := transfersPath + "/" + transferID
	receivingBody := `{"storeId":"` + branch + `","items":[` + transferItem(variantID, 3) + `],"reason":"Una unidad faltante"}`
	request("POST", link+"/receive", receivingBody, origin, cookie, 409)
	request("POST", link+"/dispatch", transferStore(branch), origin, cookie, 403)
	request("GET", link+"?store_id="+otherStore, "", "", second, 404)
	existenceAcrossBranches := "/api/v1/inventory/existences/" + variantID + "?store_id=" + branch
	if detail := request("GET", existenceAcrossBranches, "", "", cookie, 200).Body.String(); !strings.Contains(detail, `"sellable":null`) {
		t.Fatalf("el tránsito no debe aparecer como vendible en el destino: %s", detail)
	}
	if got := request("GET", recipientBalances, "", "", cookie, 200).Body.String(); got != `{"items":[]}` {
		t.Fatalf("saldo destino en borrador: %s", got)
	}
	delivery := request("POST", link+"/dispatch", transferStore(created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(delivery, `"status":"DISPATCHED"`) {
		t.Fatalf("despacho incorrecto: %s", delivery)
	}
	request("POST", link+"/dispatch", transferStore(created.StoreID), origin, manager, 200)
	request("POST", link+"/cancel", transferStore(created.StoreID), origin, manager, 409)
	if got := request("GET", balances, "", "", manager, 200).Body.String(); !strings.Contains(got, `"onHand":6`) {
		t.Fatalf("saldo origen despachado: %s", got)
	}
	if got := request("GET", recipientBalances, "", "", cookie, 200).Body.String(); got != `{"items":[]}` {
		t.Fatalf("destino recibió antes de confirmar: %s", got)
	}
	request("POST", link+"/receive", receivingBody, origin, manager, 404)
	request("POST", link+"/receive", `{"storeId":"`+branch+`","items":[`+transferItem(variantID, 5)+`]}`, origin, cookie, 400)
	request("POST", link+"/receive", `{"storeId":"`+branch+`","items":[`+transferItem(variantID, 3)+`]}`, origin, cookie, 400)
	request("POST", link+"/receive", `{"storeId":"`+branch+`","items":[{"variantId":"`+variantID+`"}],"reason":"Vacío"}`, origin, cookie, 400)
	received := request("POST", link+"/receive", receivingBody, origin, cookie, 200).Body.String()
	if !strings.Contains(received, `"status":"RECEIVED"`) || !strings.Contains(received, `"received":3`) ||
		!strings.Contains(received, "Una unidad faltante") {
		t.Fatalf("recepción incorrecta: %s", received)
	}
	if repeated := request("POST", link+"/receive", receivingBody, origin, cookie, 200).Body.String(); repeated != received {
		t.Fatal("el reintento de recepción debe conservar la respuesta")
	}
	request("POST", link+"/receive", strings.Replace(receivingBody, `"quantity":3`, `"quantity":4`, 1), origin, cookie, 409)
	if got := request("GET", recipientBalances, "", "", cookie, 200).Body.String(); !strings.Contains(got, `"onHand":3`) {
		t.Fatalf("saldo destino real: %s", got)
	}
	if detail := request("GET", existenceAcrossBranches, "", "", cookie, 200).Body.String(); !strings.Contains(detail, `"onHand":3,"reserved":0,"sellable":0`) {
		t.Fatalf("el depósito no vendible de destino debe mostrar tres unidades físicas, sin disponibilidad: %s", detail)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE transfer_id=$1", transferID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("la transferencia debe generar dos movimientos: %d %v", auditCount, err)
	}
	if got := request("GET", transfersPath+"?store_id="+created.StoreID, "", "", cashier, 200).Body.String(); !strings.Contains(got, transferID) || !strings.Contains(got, `"canTransfer":false`) || !strings.Contains(got, `"destinations":[]`) {
		t.Fatalf("alcance de lectura incorrecto: %s", got)
	}

	salesFloor := readID(request("POST", locationsPath, locBody(created.StoreID, "SAL"), origin, cookie, 201))
	internal := openTransfer(transferDraft(salesFloor, transferItem(variantID, 1)), "same-branch", manager, 201)
	internalPath := transfersPath + "/" + internal
	request("POST", internalPath+"/dispatch", transferStore(created.StoreID), origin, manager, 200)
	zeroReceipt := `{"storeId":"` + created.StoreID + `","items":[` + transferItem(variantID, 0) + `],"reason":"Embalaje vacío"}`
	noCredit := request("POST", internalPath+"/receive", zeroReceipt, origin, manager, 200).Body.String()
	if !strings.Contains(noCredit, `"status":"RECEIVED"`) || !strings.Contains(noCredit, `"received":0`) {
		t.Fatalf("recepción interna sin unidades: %s", noCredit)
	}
	if got := request("GET", "/api/v1/inventory?store_id="+created.StoreID+"&location_id="+salesFloor, "", "", manager, 200).Body.String(); got != `{"items":[]}` {
		t.Fatalf("la recepción de cero unidades acreditó saldo: %s", got)
	}
	const countsPath = "/api/v1/inventory/counts"
	startCount := func(key, countLocation string, variants []string, actor *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"storeId": created.StoreID, "locationId": countLocation, "variantIds": variants})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", countsPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("Idempotency-Key", key)
		req.AddCookie(actor)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		if result.Code != want {
			t.Fatalf("abrir conteo: esperado %d, recibido %d %s", want, result.Code, result.Body)
		}
		return result
	}
	completeBody := func(entries, note string) string {
		body, err := json.Marshal(map[string]any{"storeId": created.StoreID, "items": json.RawMessage("[" + entries + "]"), "note": note})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	startCount("cashier-count-denied", location, []string{variantID}, cashier, 403)
	startCount("count-foreign-variant", location, []string{otherVariantID}, manager, 404)
	startCount("count-foreign-location", northLocation, []string{variantID}, manager, 404)
	startCount("count-duplicate-variant", location, []string{variantID, variantID}, manager, 400)
	initialCount := startCount("physical-1", location, []string{variantID, spare}, manager, 201)
	countID := readID(initialCount)
	if !strings.Contains(initialCount.Body.String(), `"expected":5`) || !strings.Contains(initialCount.Body.String(), `"expected":0`) {
		t.Fatalf("saldos de apertura incorrectos: %s", initialCount.Body)
	}
	if replay := startCount("physical-1", location, []string{spare, variantID}, manager, 201); readID(replay) != countID {
		t.Fatal("el reintento creó otro conteo")
	}
	startCount("physical-1", location, []string{variantID}, manager, 409)
	countURL := countsPath + "/" + countID
	request("GET", countURL+"?store_id="+otherStore, "", "", second, 404)
	request("GET", countURL+"?store_id="+branch, "", "", cookie, 404)
	request("POST", countURL+"/complete", completeBody(transferItem(variantID, 3), ""), origin, manager, 400)
	request("POST", countURL+"/complete", completeBody(transferItem(variantID, 3)+`,{"variantId":"`+spare+`"}`, ""), origin, manager, 400)
	physical := completeBody(transferItem(variantID, 3)+","+transferItem(spare, 0), "Revisión Depósito")
	request("POST", countURL+"/complete", physical, origin, cashier, 403)
	completed := request("POST", countURL+"/complete", physical, origin, manager, 200).Body.String()
	if !strings.Contains(completed, `"status":"COMPLETED"`) || !strings.Contains(completed, `"counted":3`) ||
		!strings.Contains(completed, `"counted":0`) {
		t.Fatalf("resultado del conteo incorrecto: %s", completed)
	}
	if retry := request("POST", countURL+"/complete", physical, origin, manager, 200).Body.String(); retry != completed {
		t.Fatal("el reintento de confirmación alteró el conteo")
	}
	request("POST", countURL+"/complete", completeBody(transferItem(variantID, 4)+","+transferItem(spare, 0), "Revisión Depósito"), origin, manager, 409)
	request("POST", countURL+"/cancel", transferStore(created.StoreID), origin, manager, 409)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE count_id=$1", countID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el conteo debe registrar solo la variante con diferencia: %d %v", auditCount, err)
	}
	if stock := request("GET", balances, "", "", manager, 200).Body.String(); !strings.Contains(stock, `"onHand":3`) {
		t.Fatalf("saldo contado incorrecto: %s", stock)
	}
	stale := readID(startCount("physical-stale", location, []string{variantID}, manager, 201))
	expect(adjust(adjustBody(created.StoreID, location, variantID, 1, "Ingreso que cambia la revisión"), "count-revision-1", manager), 201)
	expect(adjust(adjustBody(created.StoreID, location, variantID, -1, "Egreso que vuelve al mismo saldo"), "count-revision-2", manager), 201)
	staleURL := countsPath + "/" + stale
	conflictBody := request("POST", staleURL+"/complete", completeBody(transferItem(variantID, 4), ""), origin, manager, 409).Body.String()
	if !strings.Contains(conflictBody, "count_stale") {
		t.Fatalf("el conteo vencido debe detectarse aunque el saldo sea igual: %s", conflictBody)
	}
	request("POST", staleURL+"/cancel", transferStore(created.StoreID), origin, manager, 200)
	request("POST", staleURL+"/cancel", transferStore(created.StoreID), origin, manager, 200)
	execSQL(t, ctx, db, `UPDATE stock_balances SET quantity_reserved=2 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`,
		created.TenantID, location, variantID)
	reservedCount := readID(startCount("physical-reserved", location, []string{variantID, spare}, manager, 201))
	request("POST", countsPath+"/"+reservedCount+"/complete",
		completeBody(transferItem(variantID, 1)+","+transferItem(spare, 2), ""), origin, manager, 409)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE count_id IN ($1,$2)", stale, reservedCount).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un conteo rechazado dejó movimientos: %d %v", auditCount, err)
	}
	if results := request("GET", countsPath+"?store_id="+created.StoreID+"&location_id="+location, "", "", cashier, 200).Body.String(); !strings.Contains(results, countID) || !strings.Contains(results, `"status":"CANCELLED"`) {
		t.Fatalf("listado de conteos incorrecto: %s", results)
	}

	const minimumPath = "/api/v1/inventory/minimums"
	minimumBody := func(storeID, locationID, itemID, value string) string {
		return fmt.Sprintf(`{"storeId":"%s","locationId":"%s","variantId":"%s","minimum":%s}`, storeID, locationID, itemID, value)
	}
	request("GET", balances+"&low_stock=truth", "", "", manager, 400)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1"), origin, cashier, 403)
	operator := loginAs("equipo@example.test", staffPassword)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1"), origin, operator, 403)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "-1"), origin, manager, 400)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, `"1"`), origin, manager, 400)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1.5"), origin, manager, 400)
	request("PUT", minimumPath, strings.Replace(minimumBody(created.StoreID, location, variantID, "0"), `,"minimum":0`, "", 1), origin, manager, 400)
	request("PUT", minimumPath, minimumBody(created.StoreID, northLocation, variantID, "0"), origin, manager, 404)
	request("PUT", minimumPath, minimumBody(branch, northLocation, variantID, "0"), origin, manager, 404)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, otherVariantID, "0"), origin, manager, 404)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1"), "https://other.example", manager, 403)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1"), origin, manager, 204)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "1"), origin, manager, 204)
	request("PUT", minimumPath, minimumBody(branch, northLocation, variantID, "8"), origin, cookie, 204)
	type inventoryRow struct {
		VariantID string
		OnHand    int
		Reserved  int
		Available int
		Minimum   *int
		LowStock  bool
		Active    bool
	}
	readBalances := func(path string, actor *http.Cookie) []inventoryRow {
		t.Helper()
		res := request("GET", path, "", "", actor, 200)
		var result struct{ Items []inventoryRow }
		if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Items
	}
	lowPath := balances + "&low_stock=true"
	uncostedBalances := request("GET", balances, "", "", cashier, 200).Body.String()
	if value := readValuation(cashier); value.UnitsWithCost != 0 || value.UnitsWithoutCost < 3 ||
		value.KnownValue != nil || value.TotalValue != nil {
		t.Fatalf("el stock sin costo debe carecer de valuación: %+v", value)
	}
	if !strings.Contains(uncostedBalances, `"currency":"ARS"`) ||
		!strings.Contains(uncostedBalances, `"costStatus":"unknown","unitCost":null,"inventoryValue":null`) {
		t.Fatalf("el inventario debe distinguir el costo desconocido sin valorarlo como cero: %s", uncostedBalances)
	}
	if got := readBalances(lowPath, cashier); len(got) != 1 || got[0].VariantID != variantID || got[0].OnHand != 3 ||
		got[0].Reserved != 2 || got[0].Available != 1 || got[0].Minimum == nil || *got[0].Minimum != 1 || !got[0].LowStock {
		t.Fatalf("la alerta debe comparar el disponible e incluir el mínimo exacto: %+v", got)
	}
	request("PUT", minimumPath, minimumBody(created.StoreID, location, spare, "0"), origin, manager, 204)
	lowRows := readBalances(lowPath, cashier)
	foundWithoutBalance := false
	for _, row := range lowRows {
		if row.VariantID == spare && row.OnHand == 0 && row.Minimum != nil && *row.Minimum == 0 && row.LowStock {
			foundWithoutBalance = true
		}
	}
	if len(lowRows) != 2 || !foundWithoutBalance {
		t.Fatalf("mínimo cero sin saldo: %+v", lowRows)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, location, spare).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("configurar mínimo creó un saldo: %d %v", auditCount, err)
	}
	sparePath := productsPath + "/" + product + "/variants/" + spare
	catalogPatch(sparePath, `{"status":"inactive"}`, origin, cookie, 200)
	if got := readBalances(lowPath, cashier); len(got) != 1 || got[0].VariantID != variantID {
		t.Fatalf("alerta de una variante inactiva: %+v", got)
	}
	catalogPatch(sparePath, `{"status":"active"}`, origin, cookie, 200)
	if got := readBalances(lowPath, cashier); len(got) != 2 {
		t.Fatalf("reactivar debe recuperar el mínimo: %+v", got)
	}
	request("PUT", minimumPath, minimumBody(created.StoreID, location, spare, "null"), origin, manager, 204)
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "0"), origin, manager, 204)
	if got := readBalances(lowPath, cashier); len(got) != 0 {
		t.Fatalf("mínimos retirados o menores no deben alertar: %+v", got)
	}
	request("PUT", minimumPath, minimumBody(created.StoreID, location, variantID, "null"), origin, manager, 204)
	if got := readBalances(balances, cashier); len(got) != 1 || got[0].Minimum != nil || got[0].OnHand != 3 {
		t.Fatalf("quitar mínimo debe conservar existencias: %+v", got)
	}
	if got := readBalances(recipientBalances+"&low_stock=true", cookie); len(got) != 1 || got[0].Minimum == nil || *got[0].Minimum != 8 {
		t.Fatalf("el mínimo de Norte debe ser independiente: %+v", got)
	}

	const registersPath = "/api/v1/cash-registers"
	const sessionsPath = "/api/v1/cash-sessions"
	registerBody := func(storeID, code string) string {
		return fmt.Sprintf(`{"storeId":"%s","code":"%s","name":"Caja 1"}`, storeID, code)
	}
	request("GET", registersPath+"?store_id="+created.StoreID, "", "", operator, 403)
	request("POST", registersPath, registerBody(created.StoreID, "CAJA-1"), origin, manager, 403)
	registerID := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-1"), origin, cookie, 201))
	request("POST", registersPath, registerBody(created.StoreID, "caja-1"), origin, cookie, 409)
	northRegister := readID(request("POST", registersPath, registerBody(branch, "CAJA-1"), origin, cookie, 201))
	foreignRegister := readID(request("POST", registersPath, registerBody(otherStore, "CAJA-1"), origin, second, 201))
	request("GET", registersPath+"?store_id="+branch, "", "", manager, 404)
	request("GET", registersPath+"?store_id="+otherStore, "", "", cookie, 404)
	keyedCash := func(path, body, key string, actor *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("Idempotency-Key", key)
		req.AddCookie(actor)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	openBody := func(storeID, id, amount string) string {
		return fmt.Sprintf(`{"storeId":"%s","registerId":"%s","openingCash":%s}`, storeID, id, amount)
	}
	cashOpening := openBody(created.StoreID, registerID, `"100.05"`)
	expect(keyedCash(sessionsPath, cashOpening, "", cashier), 400)
	expect(keyedCash(sessionsPath, openBody(created.StoreID, registerID, "100.05"), "bad-money-value", cashier), 400)
	expect(keyedCash(sessionsPath, openBody(created.StoreID, northRegister, `"0"`), "wrong-register-store", manager), 404)
	expect(keyedCash(sessionsPath, openBody(created.StoreID, foreignRegister, `"0"`), "foreign-register", manager), 404)
	openRes := expect(keyedCash(sessionsPath, cashOpening, "cash-open-start", cashier), 201)
	sessionID := readID(openRes)
	if replay := expect(keyedCash(sessionsPath, cashOpening, "cash-open-start", cashier), 201); replay.Body.String() != openRes.Body.String() {
		t.Fatal("la apertura repetida debe devolver la misma sesión")
	}
	expect(keyedCash(sessionsPath, openBody(created.StoreID, registerID, `"101"`), "cash-open-start", cashier), 409)
	expect(keyedCash(sessionsPath, cashOpening, "cash-open-other", manager), 409)
	sessionPath := sessionsPath + "/" + sessionID
	cashQuery := "?store_id=" + created.StoreID
	movePath := sessionPath + "/movements"
	cashMove := func(kind, amount, reason string) string {
		return fmt.Sprintf(`{"storeId":"%s","type":"%s","amount":%s,"reason":"%s"}`, created.StoreID, kind, amount, reason)
	}
	deposit := cashMove("manual_in", `"0.1"`, "Ingreso inicial")
	depositRes := expect(keyedCash(movePath, deposit, "cash-deposit-1", cashier), 201)
	if repeat := expect(keyedCash(movePath, deposit, "cash-deposit-1", cashier), 201); repeat.Body.String() != depositRes.Body.String() {
		t.Fatal("el reintento duplicó un movimiento de caja")
	}
	expect(keyedCash(movePath, cashMove("manual_in", `"0.20"`, "Segundo ingreso"), "cash-deposit-2", manager), 201)
	expect(keyedCash(movePath, cashMove("manual_out", `"50.05"`, "Retiro"), "cash-withdrawal", cashier), 201)
	expect(keyedCash(movePath, cashMove("manual_out", `"50.31"`, "Retiro excesivo"), "cash-negative", cashier), 409)
	expect(keyedCash(movePath, cashMove("manual_in", `"0"`, "En cero"), "cash-zero-move", cashier), 400)
	expect(keyedCash(movePath, cashMove("manual_in", `"0.201"`, "Tres decimales"), "cash-fraction", cashier), 400)
	expect(keyedCash(movePath, cashMove("transfer", `"40.00"`, "Transferencia bancaria"), "cash-noncash", cashier), 400)
	currentCash := request("GET", sessionPath+cashQuery, "", "", manager, 200).Body.String()
	if !strings.Contains(currentCash, `"expectedCash":"50.30"`) {
		t.Fatalf("efectivo esperado incorrecto: %s", currentCash)
	}
	request("GET", sessionPath+"?store_id="+otherStore, "", "", second, 404)
	closeBody := fmt.Sprintf(`{"storeId":"%s","declaredCash":"50.29"}`, created.StoreID)
	closed := request("POST", sessionPath+"/close", closeBody, origin, manager, 200).Body.String()
	if !strings.Contains(closed, `"status":"CLOSED"`) || !strings.Contains(closed, `"expectedCash":"50.30"`) ||
		!strings.Contains(closed, `"declaredCash":"50.29"`) || !strings.Contains(closed, `"difference":"-0.01"`) {
		t.Fatalf("arqueo incorrecto: %s", closed)
	}
	if retry := request("POST", sessionPath+"/close", closeBody, origin, cashier, 200).Body.String(); retry != closed {
		t.Fatal("el reintento de cierre debe conservar el arqueo original")
	}
	request("POST", sessionPath+"/close", strings.Replace(closeBody, "50.29", "50.30", 1), origin, manager, 409)
	expect(keyedCash(movePath, deposit, "cash-deposit-1", cashier), 201)
	expect(keyedCash(movePath, deposit, "cash-post-close", cashier), 409)
	if data := request("GET", movePath+cashQuery, "", "", cashier, 200).Body.String(); strings.Count(data, `"amount":`) != 3 {
		t.Fatalf("historial efectivo incorrecto: %s", data)
	}
	moveID := readID(depositRes)
	if _, err := db.Exec(ctx, "UPDATE cash_movements SET reason='alterado' WHERE id=$1", moveID); err == nil {
		t.Fatal("se pudo modificar un movimiento de caja")
	}
	if _, err := db.Exec(ctx, "DELETE FROM cash_movements WHERE id=$1", moveID); err == nil {
		t.Fatal("se pudo eliminar un movimiento de caja")
	}
	parallelOpen := make(chan *httptest.ResponseRecorder, 2)
	for _, key := range []string{"cash-concurrent-1", "cash-concurrent-2"} {
		go func() {
			parallelOpen <- keyedCash(sessionsPath, openBody(created.StoreID, registerID, `"0"`), key, manager)
		}()
	}
	cash1, cash2 := <-parallelOpen, <-parallelOpen
	if cash1.Code == 409 {
		cash1, cash2 = cash2, cash1
	}
	expect(cash1, 201)
	expect(cash2, 409)
	managerSession := readID(cash1)
	listedCash := request("GET", registersPath+cashQuery, "", "", cashier, 200).Body.String()
	if !strings.Contains(listedCash, `"occupied":true,"openSessionId":null`) || strings.Contains(listedCash, managerSession) {
		t.Fatalf("estado de caja de otra persona incorrecto: %s", listedCash)
	}
	request("GET", sessionsPath+"/"+managerSession+cashQuery, "", "", cashier, 404)
	request("POST", sessionsPath+"/"+managerSession+"/close", fmt.Sprintf(`{"storeId":"%s","declaredCash":"0"}`, created.StoreID), origin, cashier, 404)
	recentCash := request("GET", sessionsPath+cashQuery+"&register_id="+registerID, "", "", manager, 200).Body.String()
	if !strings.Contains(recentCash, managerSession) || !strings.Contains(recentCash, sessionID) {
		t.Fatalf("historial de supervisión incorrecto: %s", recentCash)
	}

	const salesPath = "/api/v1/sales"
	sellable := readID(request("POST", locationsPath, fmt.Sprintf(`{"storeId":"%s","code":"POS","name":"Salón POS","type":"sales_floor","isSellable":true}`, created.StoreID), origin, cookie, 201))
	pointRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-POS"), origin, cookie, 201))
	pointSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, pointRegister, `"0"`), "pos-second-session", cashier), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Existencias de venta"), "pos-stock-seed", manager), 201)
	line := func(id string, qty int, price string) map[string]any {
		return map[string]any{"variantId": id, "quantity": qty, "expectedUnitPrice": price}
	}
	saleBody := func(session, location, received string, items ...map[string]any) string {
		data, err := json.Marshal(map[string]any{"storeId": created.StoreID, "cashSessionId": session, "locationId": location, "cashReceived": received, "items": items})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	lineOne := line(variantID, 1, "25.40")
	sellableBalances := "/api/v1/inventory?store_id=" + created.StoreID + "&location_id=" + sellable
	expect(keyedCash(salesPath, saleBody(sessionID, sellable, "30", lineOne), "pos-closed-session", cashier), 409)
	expect(keyedCash(salesPath, saleBody(managerSession, location, "30", lineOne), "pos-not-sellable", manager), 409)
	expect(keyedCash(salesPath, saleBody(managerSession, northLocation, "30", lineOne), "pos-other-location", manager), 409)
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "30", lineOne), "pos-other-session", cashier), 404)
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "30", lineOne), "pos-inventory-operator", operator), 403)
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "20", lineOne), "pos-underpaid", manager), 409)
	changed := expect(keyedCash(salesPath, saleBody(managerSession, sellable, "30", line(variantID, 1, "25.39")), "pos-stale-price", manager), 409)
	if !strings.Contains(changed.Body.String(), "sale_price_changed") {
		t.Fatalf("precio obsoleto: %s", changed.Body)
	}
	unpriced := expect(keyedCash(salesPath, saleBody(managerSession, sellable, "30", line(spare, 1, "1")), "pos-no-price", manager), 409)
	if !strings.Contains(unpriced.Body.String(), "sale_price_missing") {
		t.Fatalf("variante sin cotizar: %s", unpriced.Body)
	}
	request("PUT", itemPath(general, spare), `{"amount":"1.00"}`, origin, cookie, 200)
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "30", lineOne, line(spare, 1, "1")), "pos-rollback-cart", manager), 409)
	execSQL(t, ctx, db, `UPDATE stock_balances SET quantity_reserved=1 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`, created.TenantID, sellable, variantID)
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "60", line(variantID, 2, "25.40")), "pos-reserved-stock", manager), 409)
	execSQL(t, ctx, db, `UPDATE stock_balances SET quantity_reserved=0 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`, created.TenantID, sellable, variantID)
	if rows := readBalances(sellableBalances, manager); len(rows) != 1 || rows[0].OnHand != 2 {
		t.Fatalf("una venta fallida cambió stock: %+v", rows)
	}
	execSQL(t, ctx, db, `UPDATE inventory_cost_balances SET status='known',average_unit_cost=12.34
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID)
	knownBalances := request("GET", sellableBalances, "", "", manager, 200).Body.String()
	if row := locationValuation(readValuation(manager), sellable); row.UnitsWithCost != 2 || row.UnitsWithoutCost != 0 ||
		row.TotalValue == nil || *row.TotalValue != "24.68" {
		t.Fatalf("la valuación por ubicación debe coincidir con el stock valuado: %+v", row)
	}
	if !strings.Contains(knownBalances, `"unitCost":"12.34"`) || !strings.Contains(knownBalances, `"inventoryValue":"24.68"`) {
		t.Fatalf("el inventario debe exponer costo y valuación decimal conocidos: %s", knownBalances)
	}
	payloadSale := saleBody(managerSession, sellable, "30", lineOne)
	sold := expect(keyedCash(salesPath, payloadSale, "pos-success", manager), 201)
	saleID := readID(sold)
	if body := sold.Body.String(); !strings.Contains(body, `"number":"1"`) || !strings.Contains(body, `"total":"25.40"`) ||
		!strings.Contains(body, `"change":"4.60"`) || !strings.Contains(body, `"cashReceived":"30.00"`) ||
		!strings.Contains(body, `"unitCostSnapshot":"12.34"`) {
		t.Fatalf("venta calculada incorrectamente: %s", body)
	}
	reportPath := "/api/v1/reports/sales?store_id=" + created.StoreID
	readMarginReport := func(path string, actor *http.Cookie) sales.SalesMarginPage {
		t.Helper()
		response := request("GET", path, "", "", actor, 200)
		var page sales.SalesMarginPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	knownReport := readMarginReport(reportPath, manager)
	checkSalePaymentTotals := func(response *httptest.ResponseRecorder, actor *http.Cookie, amounts map[string]string) {
		t.Helper()
		var sale sales.Sale
		if err := json.Unmarshal(response.Body.Bytes(), &sale); err != nil {
			t.Fatal(err)
		}
		from := sale.CreatedAt.Format(time.RFC3339Nano)
		to := sale.CreatedAt.Add(time.Microsecond).Format(time.RFC3339Nano)
		totals := readMarginReport(reportPath+"&from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to)+"&limit=1", actor).PaymentTotals
		if len(totals) != len(amounts) {
			t.Fatalf("cantidad incorrecta de medios para venta %s: %+v", sale.ID, totals)
		}
		for _, total := range totals {
			if total.Currency != "ARS" || total.Amount != amounts[total.Method] || total.SaleCount != 1 {
				t.Fatalf("importe incorrecto para venta %s: %+v", sale.ID, total)
			}
		}
	}
	checkSalePaymentTotals(sold, manager, map[string]string{"cash": "25.40"})
	checkSalePaymentTotals(sold, cashier, nil)
	knownSummary := knownReport.Summary
	if len(knownSummary) != 1 || knownSummary[0].NetSales != "25.40" ||
		knownSummary[0].GrossMarginWithCost == nil || *knownSummary[0].GrossMarginWithCost != "51.4173" {
		t.Fatalf("resumen de una venta con costo incorrecto: %+v", knownSummary)
	}
	report := request("GET", reportPath+"&from=2020-01-01T00:00:00Z&to=2030-01-01T00:00:00Z", "", "", manager, 200).Body.String()
	if !strings.Contains(report, `"saleId":"`+saleID+`"`) || !strings.Contains(report, `"netSales":"25.40"`) ||
		!strings.Contains(report, `"costOfGoods":"12.34"`) || !strings.Contains(report, `"grossProfit":"13.06"`) ||
		!strings.Contains(report, `"grossMarginPercent":"51.4173"`) || !strings.Contains(report, `"timezone":"America/Argentina/Tucuman"`) {
		t.Fatalf("el reporte debe calcular el margen bruto con los snapshots de la venta: %s", report)
	}
	request("GET", reportPath+"&from=fecha-invalida", "", "", manager, 400)
	request("GET", reportPath+"&from=2030-01-01T00:00:00Z&to=2020-01-01T00:00:00Z", "", "", manager, 400)
	request("GET", reportPath, "", "", second, 404)
	var localSaleDay, nextLocalDay string
	if err := db.QueryRow(ctx, `SELECT day::text,(day+1)::text FROM (
		SELECT (created_at AT TIME ZONE 'America/Argentina/Tucuman')::date AS day
		FROM sales WHERE tenant_id=$1 AND id=$2
	) sold`, created.TenantID, saleID).Scan(&localSaleDay, &nextLocalDay); err != nil {
		t.Fatal(err)
	}
	if daily := knownReport.DailyTotals; len(daily) != 1 || daily[0].Date != localSaleDay || daily[0].Currency != "ARS" ||
		daily[0].SaleCount != 1 || daily[0].UnitsSold != 1 || daily[0].NetSales != "25.40" {
		t.Fatalf("el desglose debe usar la fecha local y el importe exacto: %+v", daily)
	}
	if daily := readMarginReport(reportPath+"&from_date="+nextLocalDay, manager).DailyTotals; daily == nil || len(daily) != 0 {
		t.Fatalf("un período vacío debe devolver un desglose vacío: %+v", daily)
	}
	for _, filter := range []string{"&from_date=" + localSaleDay, "&to_date=" + localSaleDay, "&from_date=" + localSaleDay + "&to_date=" + localSaleDay} {
		got := request("GET", reportPath+filter, "", "", manager, 200).Body.String()
		if !strings.Contains(got, saleID) {
			t.Fatalf("el período inclusivo debe contener la venta: %s", got)
		}
	}
	if got := request("GET", reportPath+"&from_date="+nextLocalDay, "", "", manager, 200).Body.String(); strings.Contains(got, saleID) {
		t.Fatalf("el día siguiente debe excluir la venta: %s", got)
	}
	request("GET", reportPath+"&from_date="+nextLocalDay+"&to_date="+localSaleDay, "", "", manager, 400)
	request("GET", reportPath+"&from_date=2026-02-29", "", "", manager, 400)
	request("GET", reportPath+"&from_date="+localSaleDay+"&to=2030-01-01T00:00:00Z", "", "", manager, 400)
	reportCSVPath := "/api/v1/reports/sales/csv?store_id=" + created.StoreID
	readMarginCSV := func(path string, actor *http.Cookie) [][]string {
		t.Helper()
		response := request("GET", path, "", "", actor, 200)
		if response.Header().Get("Content-Disposition") != `attachment; filename="margen-ventas.csv"` ||
			response.Header().Get("Content-Type") != csvContentType {
			t.Fatalf("encabezados de descarga incorrectos: %v", response.Header())
		}
		body := strings.TrimPrefix(response.Body.String(), "\ufeff")
		records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return records
	}
	csvRows := readMarginCSV(reportCSVPath+"&from_date="+localSaleDay+"&to_date="+localSaleDay, manager)
	if len(csvRows) != 2 || csvRows[1][0] != "1" || csvRows[1][6] != "25.40" || csvRows[1][11] != "51.4173" {
		t.Fatalf("la descarga debe conservar el margen y el período: %v", csvRows)
	}
	if csvRows := readMarginCSV(reportCSVPath+"&from_date="+nextLocalDay, manager); len(csvRows) != 1 {
		t.Fatalf("un período vacío solo debe incluir el encabezado: %v", csvRows)
	}
	request("GET", reportCSVPath, "", "", nil, 401)
	request("GET", reportCSVPath, "", "", second, 404)
	if replay := expect(keyedCash(salesPath, payloadSale, "pos-success", manager), 201); replay.Body.String() != sold.Body.String() {
		t.Fatal("el reintento debe conservar la venta original")
	}
	expect(keyedCash(salesPath, saleBody(managerSession, sellable, "31", lineOne), "pos-success", manager), 409)
	request("GET", salesPath+"/"+saleID+cashQuery, "", "", cashier, 404)
	request("GET", salesPath+"/"+saleID+"?store_id="+otherStore, "", "", second, 404)
	if got := request("GET", sessionsPath+"/"+managerSession+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, `"expectedCash":"25.40"`) {
		t.Fatalf("caja debe recibir el total y excluir el vuelto: %s", got)
	}
	ticketPath := salesPath + "/" + saleID + "/ticket"
	originalTicket := request("GET", ticketPath+cashQuery, "", "", manager, 200).Body.String()
	if !strings.Contains(originalTicket, `"merchantName":"Uno"`) || !strings.Contains(originalTicket, `"storeName":"Sucursal Uno"`) ||
		!strings.Contains(originalTicket, `"storeCode":"visible"`) || !strings.Contains(originalTicket, `"total":"25.40"`) {
		t.Fatalf("faltan datos históricos del ticket: %s", originalTicket)
	}
	request("GET", ticketPath+cashQuery, "", "", cashier, 404)
	request("GET", ticketPath+"?store_id="+branch, "", "", cookie, 404)
	request("GET", ticketPath+"?store_id="+otherStore, "", "", second, 404)
	request("GET", ticketPath+cashQuery, "", "", nil, 401)
	request("PATCH", stores+"/"+created.StoreID, `{"name":"Sucursal actualizada","code":"COD-ACTUAL","address":"Calle Nueva 99"}`, origin, cookie, 200)
	request("PUT", itemPath(general, variantID), `{"amount":"30.00"}`, origin, cookie, 200)
	catalogPatch(productsPath+"/"+product, `{"name":"Remera renombrada"}`, origin, cookie, 200)
	historicalLine := knownReport.Items[0]
	for _, query := range []string{strings.ToLower(historicalLine.SKU), strings.ToUpper(historicalLine.Product)} {
		filter := "&from_date=" + localSaleDay + "&q=" + url.QueryEscape(" "+query+" ")
		page := readMarginReport(reportPath+filter, manager)
		if len(page.Items) != 1 || page.Items[0].SaleID != saleID || len(page.Summary) != 1 || page.Summary[0].NetSales != "25.40" {
			t.Fatalf("la tabla y el resumen deben buscar por los datos al vender: %+v", page)
		}
		if len(page.TopVariants) != 1 || page.TopVariants[0].VariantID != variantID ||
			page.TopVariants[0].Product != "Remera renombrada" || page.TopVariants[0].UnitsSold != 1 {
			t.Fatalf("el ranking debe respetar el filtro histórico e identificar la variante actual: %+v", page.TopVariants)
		}
		if len(page.DailyTotals) != 1 || page.DailyTotals[0].NetSales != "25.40" {
			t.Fatalf("el desglose diario debe aplicar la búsqueda histórica: %+v", page.DailyTotals)
		}
		if rows := readMarginCSV(reportCSVPath+filter, manager); len(rows) != 2 || rows[1][3] != historicalLine.SKU {
			t.Fatalf("el CSV debe aplicar la misma búsqueda: %v", rows)
		}
	}
	for _, query := range []string{"Remera renombrada", "%"} {
		filter := "&q=" + url.QueryEscape(query)
		page := readMarginReport(reportPath+filter, manager)
		if len(page.Items) != 0 || len(page.Summary) != 0 || page.TopVariants == nil || len(page.TopVariants) != 0 ||
			len(readMarginCSV(reportCSVPath+filter, manager)) != 1 {
			t.Fatalf("la búsqueda literal sin coincidencias debe devolver resultados vacíos: %q", query)
		}
	}
	request("GET", reportPath+"&q="+strings.Repeat("a", 121), "", "", manager, 400)
	historySale := request("GET", salesPath+"/"+saleID+cashQuery, "", "", manager, 200).Body.String()
	if historySale != sold.Body.String() {
		t.Fatalf("la venta histórica debe conservar nombre y precio: %s", historySale)
	}
	if after := request("GET", ticketPath+cashQuery, "", "", manager, 200).Body.String(); after != originalTicket {
		t.Fatalf("la reimpresión cambió los datos históricos: %s", after)
	}
	raceSales := make(chan int, 2)
	firstRace := saleBody(managerSession, sellable, "30", line(variantID, 1, "30"))
	secondRace := saleBody(pointSession, sellable, "30", line(variantID, 1, "30"))
	go func() { raceSales <- keyedCash(salesPath, firstRace, "last-unit-1", manager).Code }()
	go func() { raceSales <- keyedCash(salesPath, secondRace, "last-unit-2", cashier).Code }()
	aSale, bSale := <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("última unidad simultánea: %d y %d", aSale, bSale)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sales WHERE tenant_id=$1`, created.TenantID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("ventas persistidas: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, `SELECT next_number FROM sale_sequences WHERE tenant_id=$1 AND store_id=$2`,
		created.TenantID, created.StoreID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("numeración de ventas con rollback: %d %v", auditCount, err)
	}
	for _, table := range []string{"stock_movements", "cash_movements", "sale_payments"} {
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE sale_id=$1", saleID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("efecto de venta duplicado en %s: %d %v", table, auditCount, err)
		}
	}
	request("POST", sessionsPath+"/"+managerSession+"/close", fmt.Sprintf(`{"storeId":"%s","declaredCash":"25.40"}`, created.StoreID), origin, manager, 200)
	if replay := expect(keyedCash(salesPath, payloadSale, "pos-success", manager), 201); replay.Body.String() != sold.Body.String() {
		t.Fatal("el reintento posterior al cierre debe devolver la venta confirmada")
	}
	request("PUT", itemPath(general, spare), `{"amount":"0.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 1, "Artículo con valor cero explícito"), "pos-zero-stock", manager), 201)
	if value := readValuation(manager); value.UnitsWithCost < 1 || value.UnitsWithoutCost != 1 ||
		value.KnownValue == nil || value.TotalValue != nil {
		t.Fatalf("la valuación parcial debe informar las unidades sin costo: %+v", value)
	} else {
		row := locationValuation(value, sellable)
		if row.UnitsWithCost != 0 || row.UnitsWithoutCost != 1 || row.KnownValue != nil || row.TotalValue != nil {
			t.Fatalf("la ubicación con stock sin costo debe carecer de valuación: %+v", row)
		}
	}
	partialValuationCSV := readValuationCSV(manager)
	if len(partialValuationCSV) < 3 || partialValuationCSV[1][6] != "1" ||
		partialValuationCSV[1][7] == "" || partialValuationCSV[1][8] != "" {
		t.Fatalf("el CSV debe distinguir el valor conocido de la valuación total desconocida: %v", partialValuationCSV)
	}
	free := expect(keyedCash(salesPath, saleBody(pointSession, sellable, "0", line(spare, 1, "0")), "pos-explicit-zero", cashier), 201)
	freeID := readID(free)
	mixedReport := readMarginReport(reportPath+"&limit=1&offset=1", manager)
	var dailySales, dailyUnits int64
	for _, day := range mixedReport.DailyTotals {
		dailySales += day.SaleCount
		dailyUnits += day.UnitsSold
	}
	if dailySales != 3 || dailyUnits != 3 {
		t.Fatalf("el desglose diario debe contar todas las ventas y unidades del período: %+v", mixedReport.DailyTotals)
	}
	top := mixedReport.TopVariants
	if len(top) != 2 || top[0].VariantID != variantID || top[0].UnitsSold != 2 ||
		top[1].VariantID != spare || top[1].UnitsSold != 1 {
		t.Fatalf("el ranking debe agrupar por variante y contar todo el período: %+v", top)
	}
	var cashierUnits int64
	for _, entry := range readMarginReport(reportPath, cashier).TopVariants {
		cashierUnits += entry.UnitsSold
	}
	wantCashierUnits := int64(1)
	var cashierWonRace bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sales WHERE tenant_id=$1 AND idempotency_key='last-unit-2')`,
		created.TenantID).Scan(&cashierWonRace); err != nil {
		t.Fatal(err)
	}
	if cashierWonRace {
		wantCashierUnits++
	}
	if cashierUnits != wantCashierUnits {
		t.Fatalf("el ranking del cajero debe contar sus propias sesiones: %d", cashierUnits)
	}
	if len(mixedReport.Items) != 1 || len(mixedReport.Summary) != 1 ||
		mixedReport.Summary[0].LineCount != 3 || mixedReport.Summary[0].UnknownCostLines != 1 ||
		mixedReport.Summary[0].NetSales != "55.40" || mixedReport.Summary[0].NetSalesWithoutCost != "0.00" ||
		mixedReport.Summary[0].GrossMarginWithCost == nil || *mixedReport.Summary[0].GrossMarginWithCost != "55.4513" {
		t.Fatalf("el resumen debe usar todo el período y ponderar por importes: %+v", mixedReport.Summary)
	}
	emptySummary := readMarginReport(reportPath+"&from=9999-01-01T00:00:00Z", manager).Summary
	if emptySummary == nil || len(emptySummary) != 0 {
		t.Fatalf("un período vacío debe devolver un arreglo vacío: %+v", emptySummary)
	}
	if allRows := readMarginCSV(reportCSVPath+"&limit=1&offset=1", manager); len(allRows) != 4 {
		t.Fatalf("el CSV debe incluir las tres ventas completas sin paginar: %v", allRows)
	}
	cashierCSV := readMarginCSV(reportCSVPath, cashier)
	if len(cashierCSV) < 2 || cashierCSV[1][6] != "0.00" || cashierCSV[1][7] != "unknown" || cashierCSV[1][11] != "" {
		t.Fatalf("el CSV debe conservar el cero y dejar vacío el margen desconocido: %v", cashierCSV)
	}
	for _, row := range cashierCSV[1:] {
		if row[0] == "1" {
			t.Fatal("el cajero descargó una venta de otra sesión")
		}
	}
	freeReport := request("GET", reportPath, "", "", cashier, 200).Body.String()
	if !strings.Contains(freeReport, `"saleId":"`+freeID+`"`) || !strings.Contains(freeReport, `"netSales":"0.00"`) ||
		!strings.Contains(freeReport, `"costStatus":"unknown"`) || !strings.Contains(freeReport, `"grossProfit":null`) ||
		!strings.Contains(freeReport, `"grossMarginPercent":null`) || strings.Contains(freeReport, `"saleId":"`+saleID+`"`) {
		t.Fatalf("el reporte debe dejar el margen sin costo como no disponible: %s", freeReport)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE sale_id=$1", freeID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("una venta de importe cero no debe generar movimiento de efectivo: %d %v", auditCount, err)
	}

	listFirst := request("GET", salesPath+cashQuery+"&limit=1", "", "", manager, 200).Body.String()
	if !strings.Contains(listFirst, freeID) || !strings.Contains(listFirst, `"hasMore":true`) {
		t.Fatalf("paginación de ventas incorrecta: %s", listFirst)
	}
	listThird := request("GET", salesPath+cashQuery+"&limit=1&offset=2", "", "", manager, 200).Body.String()
	if !strings.Contains(listThird, saleID) || !strings.Contains(listThird, `"hasMore":false`) {
		t.Fatalf("la venta original debe aparecer al paginar: %s", listThird)
	}
	listExact := request("GET", salesPath+cashQuery+"&number=0001", "", "", manager, 200).Body.String()
	if !strings.Contains(listExact, saleID) || strings.Contains(listExact, freeID) {
		t.Fatalf("la búsqueda por número debe filtrar la venta: %s", listExact)
	}
	cashierList := request("GET", salesPath+cashQuery, "", "", cashier, 200).Body.String()
	if strings.Contains(cashierList, saleID) || !strings.Contains(cashierList, freeID) {
		t.Fatalf("el cajero vio tickets de otra sesión: %s", cashierList)
	}
	request("GET", salesPath+cashQuery+"&number=-1", "", "", manager, 400)
	request("GET", salesPath+cashQuery+"&limit=101", "", "", manager, 400)
	request("GET", salesPath+"?store_id="+branch, "", "", manager, 404)
	if reticket := request("GET", ticketPath+cashQuery, "", "", manager, 200).Body.String(); reticket != originalTicket {
		t.Fatal("el ticket debe conservarse después del cierre y varias reimpresiones")
	}
	for _, table := range []string{"stock_movements", "cash_movements", "sale_payments"} {
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE sale_id=$1", saleID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("la reimpresión cambió %s: %d %v", table, auditCount, err)
		}
	}

	paymentRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-PAGOS"), origin, cookie, 201))
	paymentSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, paymentRegister, `"100"`), "payments-open", cashier), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 6, "Stock para pagos múltiples"), "payments-stock", manager), 201)
	payBody := func(session, payments string) string {
		return fmt.Sprintf(`{"storeId":"%s","cashSessionId":"%s","locationId":"%s","payments":%s,
			"items":[{"variantId":"%s","quantity":1,"expectedUnitPrice":"30"}]}`,
			created.StoreID, session, sellable, payments, variantID)
	}
	for _, invalid := range []string{
		`[]`, `[{"method":"voucher","amount":"30"}]`, `[{"method":"qr","amount":"0"}]`,
		`[{"method":"credit","amount":30}]`, `[{"method":"cash","amount":"30"}]`,
		`[{"method":"credit","amount":"30","cashReceived":"30"}]`,
		`[{"method":"credit","amount":"30","cardNumber":"0000000000000000"}]`,
		`[{"method":"transfer","amount":"15"},{"method":"transfer","amount":"15"}]`,
	} {
		expect(keyedCash(salesPath, payBody(paymentSession, invalid), "bad-payments", cashier), 400)
	}
	both := strings.Replace(payBody(paymentSession, `[{"method":"qr","amount":"30"}]`), `"payments":`, `"cashReceived":"30","payments":`, 1)
	expect(keyedCash(salesPath, both, "bad-payments", cashier), 400)
	for _, invalid := range []string{
		`[{"method":"qr","amount":"29.99"}]`, `[{"method":"debit","amount":"30.01"}]`,
		`[{"method":"transfer","amount":"9000000000000000"},{"method":"credit","amount":"9000000000000000"}]`,
		`[{"method":"cash","amount":"10","cashReceived":"9.99"},{"method":"qr","amount":"20"}]`,
	} {
		expect(keyedCash(salesPath, payBody(paymentSession, invalid), "bad-payment-total", cashier), 409)
	}
	if rows := readBalances(sellableBalances, manager); rows[0].OnHand != 6 {
		t.Fatalf("un pago inválido cambió el stock: %+v", rows)
	}
	for _, method := range []string{"transfer", "debit", "credit", "qr"} {
		paid := expect(keyedCash(salesPath, payBody(paymentSession, fmt.Sprintf(`[{"method":"%s","amount":"30"}]`, method)),
			"payment-"+method, cashier), 201)
		id := readID(paid)
		if !strings.Contains(paid.Body.String(), fmt.Sprintf(`"payments":[{"method":"%s","amount":"30.00"}]`, method)) ||
			!strings.Contains(paid.Body.String(), `"cashReceived":"0.00","change":"0.00"`) {
			t.Fatalf("desglose electrónico inválido: %s", paid.Body)
		}
		if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE sale_id=$1", id).Scan(&auditCount); err != nil || auditCount != 0 {
			t.Fatalf("el pago %s impactó en efectivo: %d %v", method, auditCount, err)
		}
	}
	mixed := payBody(paymentSession, `[{"method":"qr","amount":"19.95"},{"method":"cash","amount":"10.05","cashReceived":"12"}]`)
	confirmedMixed := expect(keyedCash(salesPath, mixed, "payment-mixed", cashier), 201)
	checkSalePaymentTotals(confirmedMixed, cashier, map[string]string{"cash": "10.05", "qr": "19.95"})
	mixedID := readID(confirmedMixed)
	if !strings.Contains(confirmedMixed.Body.String(), `"cashReceived":"12.00","change":"1.95"`) ||
		!strings.Contains(confirmedMixed.Body.String(), `"payments":[{"method":"cash","amount":"10.05"},{"method":"qr","amount":"19.95"}]`) {
		t.Fatalf("pagos mixtos incorrectos: %s", confirmedMixed.Body)
	}
	reordered := payBody(paymentSession, `[{"method":"cash","amount":"10.05","cashReceived":"12.00"},{"method":"qr","amount":"19.95"}]`)
	if replay := expect(keyedCash(salesPath, reordered, "payment-mixed", cashier), 201); replay.Body.String() != confirmedMixed.Body.String() {
		t.Fatal("el reintento de un pago mixto debe conservar la venta original")
	}
	expect(keyedCash(salesPath, payBody(paymentSession, `[{"method":"debit","amount":"30"}]`), "payment-mixed", cashier), 409)
	mixedElectronic := expect(keyedCash(salesPath, payBody(paymentSession, `[{"method":"transfer","amount":"10"},{"method":"credit","amount":"20"}]`),
		"payment-mixed-electronic", cashier), 201)
	var cashAmount string
	if err := db.QueryRow(ctx, "SELECT amount::text FROM cash_movements WHERE sale_id=$1", mixedID).Scan(&cashAmount); err != nil || cashAmount != "10.05" {
		t.Fatalf("la caja debe recibir solo el efectivo neto: %s %v", cashAmount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE sale_id=$1", readID(mixedElectronic)).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("el pago mixto electrónico impactó en efectivo: %d %v", auditCount, err)
	}
	if rows := readBalances(sellableBalances, manager); rows[0].OnHand != 0 {
		t.Fatalf("las ventas pagadas deben descontar exactamente seis unidades: %+v", rows)
	}
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock para reintentos entre cajas"), "payments-race-stock", manager), 201)
	checkOtherRegister := payBody(pointSession, `[{"method":"qr","amount":"30"}]`)
	checkPaymentRegister := payBody(paymentSession, `[{"method":"qr","amount":"30"}]`)
	go func() { raceSales <- keyedCash(salesPath, checkOtherRegister, "payments-shared-key", cashier).Code }()
	go func() { raceSales <- keyedCash(salesPath, checkPaymentRegister, "payments-shared-key", cashier).Code }()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("una clave simultánea en dos cajas debe producir una venta y un conflicto: %d y %d", aSale, bSale)
	}
	if rows := readBalances(sellableBalances, manager); rows[0].OnHand != 1 {
		t.Fatalf("una clave compartida debe descontar solo una unidad: %+v", rows)
	}

	closedPayments := request("POST", sessionsPath+"/"+paymentSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"110.05"}`, created.StoreID), origin, cashier, 200).Body.String()
	if !strings.Contains(closedPayments, `"expectedCash":"110.05"`) || !strings.Contains(closedPayments, `"difference":"0.00"`) {
		t.Fatalf("arqueo con medios de pago incorrecto: %s", closedPayments)
	}
	if after := request("GET", salesPath+"/"+mixedID+"/ticket"+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(after, confirmedMixed.Body.String()) {
		t.Fatalf("ticket mixto cerrado no conserva los pagos: %s", after)
	}

	const customersPath = "/api/v1/customers"
	const receivablesPath = "/api/v1/receivables"
	customerBody := func(storeID, name string) string {
		return fmt.Sprintf(`{"storeId":"%s","name":"%s","phone":"381 555 0123"}`, storeID, name)
	}
	request("GET", customersPath+cashQuery, "", "", nil, 401)
	request("GET", customersPath+cashQuery, "", "", operator, 403)
	request("POST", customersPath, customerBody(created.StoreID, " "), origin, cashier, 400)
	request("POST", customersPath, customerBody(created.StoreID, "Sin permiso"), origin, operator, 403)
	request("POST", customersPath, customerBody(branch, "Otra sucursal"), origin, cashier, 404)
	customerID := readID(request("POST", customersPath, customerBody(created.StoreID, "  Ana de Prueba  "), origin, cashier, 201))
	branchCustomer := readID(request("POST", customersPath, customerBody(branch, "Cliente Norte"), origin, cookie, 201))
	otherCustomer := readID(request("POST", customersPath, customerBody(otherStore, "Cliente Otro"), origin, second, 201))
	if list := request("GET", customersPath+cashQuery+"&q=ana", "", "", cashier, 200).Body.String(); !strings.Contains(list, customerID) ||
		!strings.Contains(list, `"name":"Ana de Prueba"`) || strings.Contains(list, branchCustomer) || strings.Contains(list, otherCustomer) {
		t.Fatalf("búsqueda de clientes fuera de alcance o sin normalizar: %s", list)
	}
	if list := request("GET", customersPath+cashQuery+"&q=0123", "", "", cashier, 200).Body.String(); !strings.Contains(list, customerID) {
		t.Fatalf("búsqueda de clientes por teléfono incorrecta: %s", list)
	}
	if empty := request("GET", receivablesPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(empty, `"items":[]`) {
		t.Fatalf("la consulta de fiados vacía debe devolver una lista: %s", empty)
	}
	creditRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-FIADO"), origin, cookie, 201))
	creditSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, creditRegister, `"0"`), "credit-open", cashier), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 1, "Existencias para fiado"), "credit-stock", manager), 201)
	creditBody := func(id, payments string) string {
		return strings.Replace(payBody(creditSession, payments), `"payments":`, fmt.Sprintf(`"onCredit":true,"customerId":"%s","payments":`, id), 1)
	}
	expect(keyedCash(salesPath, creditBody("", "[]"), "credit-invalid", cashier), 400)
	expect(keyedCash(salesPath, creditBody(branchCustomer, "[]"), "credit-invalid", cashier), 404)
	expect(keyedCash(salesPath, creditBody(otherCustomer, "[]"), "credit-invalid", cashier), 404)
	expect(keyedCash(salesPath, creditBody(customerID, `[{"method":"qr","amount":"30"}]`), "credit-invalid", cashier), 409)
	expect(keyedCash(salesPath, creditBody(customerID, `[{"method":"qr","amount":"30.01"}]`), "credit-invalid", cashier), 409)
	checkCreditStock := func(expected int32) {
		t.Helper()
		var onHand int32
		err := db.QueryRow(ctx, `SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`,
			created.TenantID, sellable, variantID).Scan(&onHand)
		if err != nil || onHand != expected {
			t.Fatalf("stock tras confirmar fiado: %d, esperado %d (%v)", onHand, expected, err)
		}
	}
	checkCreditStock(2)
	fullCredit := expect(keyedCash(salesPath, creditBody(customerID, "[]"), "credit-full", cashier), 201)
	fullCreditID := readID(fullCredit)
	if body := fullCredit.Body.String(); !strings.Contains(body, `"payments":[]`) || !strings.Contains(body, `"originalAmount":"30.00"`) {
		t.Fatalf("fiado sin anticipo inválido: %s", body)
	}
	for _, table := range []string{"cash_movements", "sale_payments"} {
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE sale_id=$1", fullCreditID).Scan(&auditCount); err != nil || auditCount != 0 {
			t.Fatalf("el fiado total produjo un cobro en %s: %d %v", table, auditCount, err)
		}
	}
	creditDeposit := creditBody(customerID, `[{"method":"cash","amount":"7.50","cashReceived":"10"},{"method":"qr","amount":"3.25"}]`)
	partCredit := expect(keyedCash(salesPath, creditDeposit, "credit-part", cashier), 201)
	checkSalePaymentTotals(partCredit, cashier, map[string]string{"cash": "7.50", "qr": "3.25", "on_credit": "19.25"})
	partCreditID := readID(partCredit)
	receivablesCSVPath := "/api/v1/reports/receivables/csv" + cashQuery
	readReceivablesCSV := func(actor *http.Cookie, filter string) [][]string {
		t.Helper()
		response := request("GET", receivablesCSVPath+filter, "", "", actor, 200)
		if response.Header().Get("Content-Disposition") != `attachment; filename="fiados.csv"` ||
			response.Header().Get("Content-Type") != csvContentType {
			t.Fatalf("encabezados inesperados del CSV de fiados: %v", response.Header())
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := readReceivablesCSV(cashier, "&limit=1&offset=1"); len(rows) != 3 {
		t.Fatalf("el CSV debe exportar los dos fiados completos sin paginar: %v", rows)
	}
	request("GET", receivablesCSVPath, "", "", nil, 401)
	request("GET", receivablesCSVPath, "", "", second, 404)
	request("GET", receivablesCSVPath+"&status=invalid", "", "", manager, 400)
	readReceivablesPage := func(actor *http.Cookie, filter string) sales.ReceivablePage {
		t.Helper()
		response := request("GET", receivablesPath+cashQuery+filter, "", "", actor, 200)
		var page sales.ReceivablePage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	readReceivablesSummary := func(actor *http.Cookie, filter string) sales.ReceivableSummary {
		t.Helper()
		page := readReceivablesPage(actor, filter)
		if len(page.Summary) != 1 {
			t.Fatalf("se esperaba un resumen por moneda: %+v", page.Summary)
		}
		return page.Summary[0]
	}
	if total := readReceivablesSummary(cashier, "&limit=1"); total.Currency != "ARS" || total.PendingCount != 2 ||
		total.SettledCount != 0 || total.Outstanding != "49.25" || total.Collected != "0.00" || total.DebtCredits != "0.00" ||
		total.PaymentsByMethod == nil || len(total.PaymentsByMethod) != 0 {
		t.Fatalf("el resumen debe contar todos los fiados visibles sin sumar el anticipo como abono: %+v", total)
	} else if len(total.TopDebtors) != 1 || total.TopDebtors[0] != (sales.ReceivableCustomerBalance{
		CustomerID: customerID, CustomerName: "Ana de Prueba", PendingCount: 2, Outstanding: "49.25",
	}) {
		t.Fatalf("el ranking debe agrupar ambas cuentas del cliente aunque se pagine el listado: %+v", total.TopDebtors)
	}
	if body := partCredit.Body.String(); !strings.Contains(body, `"originalAmount":"19.25"`) ||
		!strings.Contains(body, `"change":"2.50"`) || !strings.Contains(body, `"customerName":"Ana de Prueba"`) {
		t.Fatalf("fiado con anticipo inválido: %s", body)
	}
	if err := db.QueryRow(ctx, "SELECT amount::text FROM cash_movements WHERE sale_id=$1", partCreditID).Scan(&cashAmount); err != nil || cashAmount != "7.50" {
		t.Fatalf("solo el efectivo neto incrementa el arqueo: %s %v", cashAmount, err)
	}
	expect(keyedCash(salesPath, creditBody(customerID, "[]"), "credit-no-stock", cashier), 409)
	checkCreditStock(0)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM receivables WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("el rollback de falta de stock dejó un saldo: %d %v", auditCount, err)
	}
	if page := request("GET", receivablesPath+cashQuery+"&limit=1", "", "", manager, 200).Body.String(); !strings.Contains(page, partCreditID) ||
		!strings.Contains(page, `"outstandingAmount":"19.25"`) || !strings.Contains(page, `"hasMore":true`) || strings.Contains(page, fullCreditID) {
		t.Fatalf("consulta del saldo fiado incorrecta: %s", page)
	}
	if page := request("GET", receivablesPath+cashQuery+"&limit=1&offset=1", "", "", cashier, 200).Body.String(); !strings.Contains(page, fullCreditID) ||
		!strings.Contains(page, `"hasMore":false`) {
		t.Fatalf("página de saldos del cajero incorrecta: %s", page)
	}
	request("GET", receivablesPath+cashQuery, "", "", second, 404)
	request("GET", receivablesPath+"?store_id="+branch, "", "", cashier, 404)
	request("GET", receivablesPath+cashQuery, "", "", operator, 403)
	request("POST", sessionsPath+"/"+creditSession+"/close", fmt.Sprintf(`{"storeId":"%s","declaredCash":"7.50"}`, created.StoreID), origin, cashier, 200)
	if replay := expect(keyedCash(salesPath, creditDeposit, "credit-part", cashier), 201); replay.Body.String() != partCredit.Body.String() {
		t.Fatal("el fiado cerrado debe conservar el mismo saldo original al reintentar")
	}
	execSQL(t, ctx, db, "UPDATE customers SET name='Nombre actualizado' WHERE id=$1", customerID)
	for _, query := range []string{" ANA de ", "actualizado"} {
		if got := readReceivablesSummary(cashier, "&q="+url.QueryEscape(query)); got.Outstanding != "49.25" || got.PendingCount != 2 {
			t.Fatalf("la búsqueda debe encontrar los nombres actual e histórico: %+v", got)
		}
	}
	var creditSale sales.Sale
	if err := json.Unmarshal(partCredit.Body.Bytes(), &creditSale); err != nil {
		t.Fatal(err)
	}
	numberQuery := "&q=00" + creditSale.Number
	customerFilter := "&customer_id=" + customerID
	if total := readReceivablesSummary(cashier, customerFilter+"&limit=1"); total.PendingCount != 2 || total.Outstanding != "49.25" {
		t.Fatalf("el filtro exacto debe sumar solo las cuentas del cliente: %+v", total)
	}
	if rows := readReceivablesCSV(manager, customerFilter+numberQuery); len(rows) != 2 || rows[1][1] != creditSale.Number {
		t.Fatalf("el CSV debe aplicar el cliente y la búsqueda juntos: %v", rows)
	}
	for _, id := range []string{branchCustomer, otherCustomer} {
		filter := "&customer_id=" + id
		if list := request("GET", receivablesPath+cashQuery+filter, "", "", manager, 200).Body.String(); !strings.Contains(list, `"items":[]`) ||
			!strings.Contains(list, `"summary":[]`) || len(readReceivablesCSV(manager, filter)) != 1 {
			t.Fatalf("el filtro de cliente no debe cruzar sucursales ni comercios: %s", list)
		}
	}
	request("GET", receivablesPath+cashQuery+"&customer_id=invalido", "", "", manager, 400)
	request("GET", receivablesCSVPath+"&customer_id=invalido", "", "", manager, 400)
	if rows := readReceivablesCSV(manager, numberQuery); len(rows) != 2 || rows[1][1] != creditSale.Number ||
		rows[1][3] != "Ana de Prueba" || rows[1][4] != "pendiente" || rows[1][6] != "19.25" || rows[1][7] != "19.25" {
		t.Fatalf("el CSV debe respetar la búsqueda y conservar el nombre de venta: %v", rows)
	}
	if got := readReceivablesSummary(manager, numberQuery); got.Outstanding != "19.25" || got.PendingCount != 1 {
		t.Fatalf("el número exacto debe limitar también el resumen: %+v", got)
	} else if len(got.TopDebtors) != 1 || got.TopDebtors[0].CustomerName != "Nombre actualizado" ||
		got.TopDebtors[0].PendingCount != 1 || got.TopDebtors[0].Outstanding != "19.25" {
		t.Fatalf("el ranking debe respetar la búsqueda y usar el nombre actual: %+v", got.TopDebtors)
	}
	if list := request("GET", receivablesPath+cashQuery+numberQuery, "", "", manager, 200).Body.String(); !strings.Contains(list, partCreditID) ||
		strings.Contains(list, fullCreditID) {
		t.Fatalf("la búsqueda por número devolvió otro fiado: %s", list)
	}
	if list := request("GET", receivablesPath+cashQuery+"&q=%25", "", "", cashier, 200).Body.String(); !strings.Contains(list, `"summary":[]`) ||
		!strings.Contains(list, `"items":[]`) {
		t.Fatalf("el texto literal sin coincidencias debe devolver resultados vacíos: %s", list)
	}
	request("GET", receivablesPath+cashQuery+"&q="+strings.Repeat("a", 121), "", "", manager, 400)
	if ticket := request("GET", salesPath+"/"+partCreditID+"/ticket"+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(ticket, partCredit.Body.String()) {
		t.Fatalf("el ticket fiado debe conservar los datos al vender: %s", ticket)
	}

	var fullDebtID, partDebtID string
	for sale, dest := range map[string]*string{fullCreditID: &fullDebtID, partCreditID: &partDebtID} {
		if err := db.QueryRow(ctx, "SELECT id FROM receivables WHERE sale_id=$1", sale).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	fullDebtPath := receivablesPath + "/" + fullDebtID + "/payments"
	partDebtPath := receivablesPath + "/" + partDebtID + "/payments"
	cashierCollect := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, creditRegister, `"20"`), "collect-cashier-open", cashier), 201))
	otherCollectRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-COBRO-2"), origin, cookie, 201))
	managerCollect := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, otherCollectRegister, `"9999999999999999.99"`),
		"collect-manager-open", manager), 201))
	collectionBody := func(session, method, amount string) string {
		return fmt.Sprintf(`{"storeId":"%s","cashSessionId":"%s","method":"%s","amount":"%s"}`, created.StoreID, session, method, amount)
	}
	request("GET", fullDebtPath+cashQuery, "", "", second, 404)
	request("GET", fullDebtPath+cashQuery, "", "", operator, 403)
	request("GET", receivablesPath+cashQuery+"&status=invalid", "", "", manager, 400)
	expect(keyedCash(fullDebtPath, collectionBody(creditSession, "cash", "7.25"), "collect-closed", cashier), 409)
	expect(keyedCash(fullDebtPath, collectionBody(managerCollect, "cash", "7.25"), "collect-wrong-register", cashier), 404)
	expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "cash", "0"), "collect-zero", cashier), 400)
	expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "fiado", "1"), "collect-method", cashier), 400)
	expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "cash", "30.01"), "collect-too-much", cashier), 409)
	expect(keyedCash(fullDebtPath, collectionBody(managerCollect, "cash", "7.25"), "collect-cash-overflow", manager), 409)
	if page := request("GET", fullDebtPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(page, `"outstandingAmount":"30.00"`) ||
		!strings.Contains(page, `"items":[]`) {
		t.Fatalf("un abono rechazado cambió el saldo o el historial: %s", page)
	}
	firstCollection := expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "cash", "7.25"), "collect-part", cashier), 201)
	firstCollectionID := readID(firstCollection)
	if !strings.Contains(firstCollection.Body.String(), `"balanceAfter":"22.75"`) {
		t.Fatalf("saldo después del abono en efectivo: %s", firstCollection.Body)
	}
	lastCollection := expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "qr", "22.75"), "collect-settle", cashier), 201)
	recent := readReceivablesPage(cashier, customerFilter+"&status=pending&limit=1").RecentPayments
	if len(recent) != 2 || recent[0].ID != readID(lastCollection) || recent[1].ID != firstCollectionID ||
		recent[0].SaleID != fullCreditID || recent[0].CustomerName != "Nombre actualizado" ||
		recent[0].Method != "qr" || recent[0].Amount != "22.75" || !recent[0].CreatedAt.After(recent[1].CreatedAt) {
		t.Fatalf("los cobros recientes deben respetar el cliente y ordenarse por fecha de abono: %+v", recent)
	}
	if recent := readReceivablesPage(cashier, customerFilter+numberQuery).RecentPayments; recent == nil || len(recent) != 0 {
		t.Fatalf("la búsqueda debe excluir abonos de otras ventas: %+v", recent)
	}
	if rows := readReceivablesCSV(cashier, customerFilter+"&status=settled"); len(rows) != 2 || rows[1][4] != "saldado" ||
		rows[1][6] != "30.00" || rows[1][7] != "0.00" {
		t.Fatalf("el CSV de saldados debe traer solo la cuenta cobrada: %v", rows)
	}
	if total := readReceivablesSummary(manager, "&status=settled"); total.PendingCount != 1 || total.SettledCount != 1 ||
		total.Outstanding != "19.25" || total.Collected != "30.00" || total.DebtCredits != "0.00" {
		t.Fatalf("el resumen debe mantener el saldo de todas las cuentas tras saldar un fiado: %+v", total)
	} else if methods := total.PaymentsByMethod; len(methods) != 2 ||
		methods[0] != (sales.ReceivableMethodTotal{Method: "cash", PaymentCount: 1, Amount: "7.25"}) ||
		methods[1] != (sales.ReceivableMethodTotal{Method: "qr", PaymentCount: 1, Amount: "22.75"}) {
		t.Fatalf("el desglose debe conservar el medio y el importe exacto de cada abono: %+v", methods)
	} else if len(total.TopDebtors) != 1 || total.TopDebtors[0].PendingCount != 1 || total.TopDebtors[0].Outstanding != "19.25" {
		t.Fatalf("una cuenta saldada debe salir del ranking sin ocultar la deuda restante: %+v", total.TopDebtors)
	}
	if total := readReceivablesSummary(cashier, numberQuery); len(total.PaymentsByMethod) != 0 {
		t.Fatalf("el desglose debe incluir solo las cuentas que coinciden con la búsqueda: %+v", total)
	}
	checkSalePaymentTotals(fullCredit, cashier, map[string]string{"on_credit": "30.00"})
	if !strings.Contains(lastCollection.Body.String(), `"balanceAfter":"0.00"`) {
		t.Fatalf("el último abono no saldó la cuenta: %s", lastCollection.Body)
	}
	if got := request("GET", receivablesPath+cashQuery+"&status=pending", "", "", manager, 200).Body.String(); strings.Contains(got, fullDebtID) ||
		!strings.Contains(got, partDebtID) {
		t.Fatalf("el fiado saldado sigue en pendientes: %s", got)
	}
	if got := request("GET", receivablesPath+cashQuery+"&status=settled", "", "", manager, 200).Body.String(); !strings.Contains(got, fullDebtID) ||
		strings.Contains(got, partDebtID) {
		t.Fatalf("el fiado saldado no está en el historial: %s", got)
	}
	if got := request("GET", fullDebtPath+cashQuery+"&limit=1&offset=1", "", "", cashier, 200).Body.String(); !strings.Contains(got, firstCollectionID) ||
		!strings.Contains(got, `"outstandingAmount":"0.00"`) || !strings.Contains(got, `"hasMore":false`) {
		t.Fatalf("historial de abonos paginado: %s", got)
	}
	expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "qr", "0.01"), "collect-already-settled", cashier), 409)
	expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "cash", "7.24"), "collect-part", cashier), 409)

	go func() {
		raceSales <- keyedCash(partDebtPath, collectionBody(cashierCollect, "qr", "15"), "collect-race-a", cashier).Code
	}()
	go func() {
		raceSales <- keyedCash(partDebtPath, collectionBody(managerCollect, "transfer", "15"), "collect-race-b", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("dos cajas intentaron cobrar el mismo saldo: %d y %d", aSale, bSale)
	}
	if page := request("GET", partDebtPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(page, `"outstandingAmount":"4.25"`) {
		t.Fatalf("el saldo concurrente es incorrecto: %s", page)
	}
	closeCollections := request("POST", sessionsPath+"/"+cashierCollect+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"27.25"}`, created.StoreID), origin, cashier, 200).Body.String()
	if !strings.Contains(closeCollections, `"expectedCash":"27.25"`) || !strings.Contains(closeCollections, `"difference":"0.00"`) {
		t.Fatalf("el arqueo incluyó importes electrónicos o duplicó efectivo: %s", closeCollections)
	}
	if replay := expect(keyedCash(fullDebtPath, collectionBody(cashierCollect, "cash", "7.25"), "collect-part", cashier), 201); replay.Body.String() != firstCollection.Body.String() {
		t.Fatal("el reintento tras el cierre debe devolver el abono original con su saldo histórico")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE receivable_payment_id=$1", firstCollectionID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("movimiento de abono duplicado: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE receivable_payment_id=$1", readID(lastCollection)).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un abono por QR impactó en caja: %d %v", auditCount, err)
	}
	if _, err := db.Exec(ctx, "UPDATE receivable_payments SET amount=1 WHERE id=$1", firstCollectionID); err == nil {
		t.Fatal("el abono registrado debe ser inmutable")
	}
	checkCreditStock(0)
	if ticket := request("GET", salesPath+"/"+fullCreditID+"/ticket"+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(ticket, fullCredit.Body.String()) {
		t.Fatalf("un abono cambió el ticket original de venta: %s", ticket)
	}

	const reservePath = "/api/v1/reservations"
	reserveBody := func(customer, qty string) string {
		return fmt.Sprintf(`{"storeId":"%s","locationId":"%s","customerId":"%s","items":[{"variantId":"%s","quantity":%s}]}`,
			created.StoreID, sellable, customer, variantID, qty)
	}
	storeInput := fmt.Sprintf(`{"storeId":"%s"}`, created.StoreID)
	request("GET", reservePath+cashQuery, "", "", operator, 403)
	expect(keyedCash(reservePath, reserveBody(otherCustomer, "1"), "reserve-invalid-customer", cashier), 404)
	expect(keyedCash(reservePath, reserveBody(branchCustomer, "1"), "reserve-other-branch", cashier), 404)
	expect(keyedCash(reservePath, reserveBody(customerID, "0"), "reserve-zero", cashier), 400)
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Existencias para reservas"), "reservation-stock", manager), 201)
	checkReserved := func(physical, reserved int32) {
		t.Helper()
		var onHand, held int32
		err := db.QueryRow(ctx, `SELECT quantity_on_hand,quantity_reserved FROM stock_balances
			WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`, created.TenantID, sellable, variantID).Scan(&onHand, &held)
		if err != nil || onHand != physical || held != reserved {
			t.Fatalf("stock físico y reservado: %d/%d, esperado %d/%d (%v)", onHand, held, physical, reserved, err)
		}
	}
	invalidLines := strings.Replace(reserveBody(customerID, "1"), "]}", fmt.Sprintf(`,{"variantId":"%s","quantity":1}]}`, spare), 1)
	expect(keyedCash(reservePath, invalidLines, "reserve-rollback", cashier), 409)
	checkReserved(2, 0)
	held := expect(keyedCash(reservePath, reserveBody(customerID, "2"), "reserve-held", cashier), 201)
	heldID := readID(held)
	if replay := expect(keyedCash(reservePath, reserveBody(customerID, "2"), "reserve-held", cashier), 201); replay.Body.String() != held.Body.String() {
		t.Fatal("reintentar una reserva debe devolver el mismo registro")
	}
	expect(keyedCash(reservePath, reserveBody(customerID, "1"), "reserve-held", cashier), 409)
	expect(keyedCash(reservePath, reserveBody(customerID, "1"), "reserve-insufficient", cashier), 409)
	checkReserved(2, 2)
	if detail := request("GET", "/api/v1/inventory/existences/"+variantID+"?store_id="+created.StoreID,
		"", "", manager, 200).Body.String(); !strings.Contains(detail, `"onHand":2,"reserved":2,"sellable":0`) {
		t.Fatalf("la reserva completa debe dejar vendible cero en la ubicación: %s", detail)
	}
	request("GET", reservePath+cashQuery, "", "", second, 404)
	request("GET", reservePath+"?store_id="+branch, "", "", cashier, 404)
	request("POST", reservePath+"/"+heldID+"/expire", storeInput, origin, cashier, 409)
	ordinarySale := payBody(managerCollect, `[{"method":"qr","amount":"30"}]`)
	expect(keyedCash(salesPath, ordinarySale, "sale-held-stock", manager), 409)
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, -1, "Protección de reserva"), "reserve-stock-protection", manager), 409)
	bindReservation := func(sale, target string) string {
		return strings.Replace(sale, `"payments":`, fmt.Sprintf(`"reservationId":"%s","payments":`, target), 1)
	}
	expect(keyedCash(salesPath, bindReservation(ordinarySale, heldID), "reservation-mismatch", manager), 409)
	conversion := strings.Replace(payBody(managerCollect, `[{"method":"qr","amount":"60"}]`), `"quantity":1`, `"quantity":2`, 1)
	conversion = bindReservation(conversion, heldID)
	expect(keyedCash(salesPath, strings.Replace(conversion, `"amount":"60"`, `"amount":"59"`, 1), "reservation-bad-payment", manager), 409)
	cashAtLimit := strings.Replace(conversion, `"payments":[{"method":"qr","amount":"60"}]`, `"cashReceived":"60"`, 1)
	expect(keyedCash(salesPath, cashAtLimit, "reservation-cash-limit", manager), 409)
	checkReserved(2, 2)
	finished := expect(keyedCash(salesPath, conversion, "reservation-fulfill", manager), 201)
	soldReservationID := readID(finished)
	expect(keyedCash(salesPath, conversion, "reservation-fulfill-again", manager), 409)
	if replay := expect(keyedCash(salesPath, conversion, "reservation-fulfill", manager), 201); replay.Body.String() != finished.Body.String() {
		t.Fatal("reintentar la entrega debe devolver la venta original")
	}
	request("POST", reservePath+"/"+heldID+"/cancel", storeInput, origin, cashier, 409)
	if page := request("GET", reservePath+cashQuery+"&status=FULFILLED", "", "", cashier, 200).Body.String(); !strings.Contains(page, heldID) ||
		!strings.Contains(page, soldReservationID) {
		t.Fatalf("reserva entregada sin vínculo de venta: %s", page)
	}
	checkReserved(0, 0)
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 1, "Stock para liberar reservas"), "reservation-release-stock", manager), 201)
	go func() {
		raceSales <- keyedCash(reservePath, reserveBody(customerID, "1"), "reserve-race-a", cashier).Code
	}()
	go func() {
		raceSales <- keyedCash(reservePath, reserveBody(customerID, "1"), "reserve-race-b", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("dos reservas intentaron tomar la última prenda: %d y %d", aSale, bSale)
	}
	var releaseID string
	if err := db.QueryRow(ctx, "SELECT id FROM reservations WHERE tenant_id=$1 AND status='ACTIVE'", created.TenantID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	checkReserved(1, 1)
	request("POST", reservePath+"/"+releaseID+"/cancel", storeInput, origin, cashier, 200)
	request("POST", reservePath+"/"+releaseID+"/cancel", storeInput, origin, cashier, 200)
	checkReserved(1, 0)
	due := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	dueInput := strings.Replace(reserveBody(customerID, "1"), `"items":`, fmt.Sprintf(`"expiresAt":"%s","items":`, due), 1)
	expiryID := readID(expect(keyedCash(reservePath, dueInput, "reserve-expiry", cashier), 201))
	request("POST", reservePath+"/"+expiryID+"/expire", storeInput, origin, cashier, 409)
	execSQL(t, ctx, db, "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", expiryID)
	expect(keyedCash(salesPath, bindReservation(ordinarySale, expiryID), "reservation-expired-sale", manager), 409)
	checkReserved(1, 1)
	request("POST", reservePath+"/"+expiryID+"/expire", storeInput, origin, cashier, 200)
	request("POST", reservePath+"/"+expiryID+"/expire", storeInput, origin, cashier, 200)
	checkReserved(1, 0)

	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 3, "Existencias para señas"), "deposit-stock", manager), 201)
	firstDepositSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, creditRegister, `"0"`),
		"deposit-register-open", cashier), 201))
	cashReserveID := readID(expect(keyedCash(reservePath, reserveBody(customerID, "1"), "deposit-cash-reservation", cashier), 201))
	cashDepositPath := reservePath + "/" + cashReserveID + "/deposit"
	expect(keyedCash(cashDepositPath, collectionBody(cashierCollect, "cash", "8"), "deposit-closed-session", cashier), 409)
	expect(keyedCash(cashDepositPath, collectionBody(firstDepositSession, "cash", "0"), "deposit-zero", cashier), 400)
	expect(keyedCash(cashDepositPath, collectionBody(managerCollect, "cash", "8"), "deposit-cash-limit", manager), 409)
	firstDeposit := expect(keyedCash(cashDepositPath, collectionBody(firstDepositSession, "cash", "8"), "deposit-cash", cashier), 201)
	firstDepositID := readID(firstDeposit)
	expect(keyedCash(cashDepositPath, collectionBody(firstDepositSession, "cash", "8"), "deposit-double", cashier), 409)
	expect(keyedCash(cashDepositPath, collectionBody(firstDepositSession, "cash", "9"), "deposit-cash", cashier), 409)
	firstDepositClose := request("POST", sessionsPath+"/"+firstDepositSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"8"}`, created.StoreID), origin, cashier, 200).Body.String()
	if !strings.Contains(firstDepositClose, `"expectedCash":"8.00"`) {
		t.Fatalf("arqueo inicial de seña en efectivo: %s", firstDepositClose)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE reservation_deposit_id=$1 AND sale_id IS NULL", firstDepositID).
		Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("la seña debe producir un solo movimiento sin venta: %d %v", auditCount, err)
	}
	secondDepositSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, creditRegister, `"10"`),
		"deposit-delivery-register", cashier), 201))
	viewDeposit := func(sale, amount string) string {
		return strings.Replace(sale, `"reservationId":`, fmt.Sprintf(`"expectedDeposit":"%s","reservationId":`, amount), 1)
	}
	staleDepositSale := bindReservation(payBody(secondDepositSession, `[{"method":"qr","amount":"30"}]`), cashReserveID)
	if conflict := expect(keyedCash(salesPath, staleDepositSale, "deposit-stale-sale", cashier), 409); !strings.Contains(conflict.Body.String(), "reservation_deposit_changed") {
		t.Fatalf("una seña agregada desde otra caja debe impedir el cobro anterior: %s", conflict.Body)
	}
	cashRemainder := strings.Replace(staleDepositSale, `"payments":[{"method":"qr","amount":"30"}]`, `"cashReceived":"25"`, 1)
	deliveredDeposit := expect(keyedCash(salesPath, viewDeposit(cashRemainder, "8"), "deliver-with-deposit", cashier), 201)
	checkSalePaymentTotals(deliveredDeposit, cashier, map[string]string{"cash": "30.00"})
	deliveredDepositID := readID(deliveredDeposit)
	if body := deliveredDeposit.Body.String(); !strings.Contains(body, `"deposit":{"method":"cash","amount":"8.00"}`) ||
		!strings.Contains(body, `"payments":[{"method":"cash","amount":"22.00"}]`) || !strings.Contains(body, `"change":"3.00"`) {
		t.Fatalf("entrega con seña en efectivo incorrecta: %s", body)
	}
	if replay := expect(keyedCash(cashDepositPath, collectionBody(firstDepositSession, "cash", "8.00"), "deposit-cash", cashier), 201); replay.Body.String() != firstDeposit.Body.String() {
		t.Fatal("reintentar después del cierre y la entrega debe devolver la seña original")
	}
	if ticket := request("GET", salesPath+"/"+deliveredDepositID+"/ticket"+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(ticket, deliveredDeposit.Body.String()) {
		t.Fatalf("seña aplicada ausente en el ticket: %s", ticket)
	}

	qrReserveID := readID(expect(keyedCash(reservePath, reserveBody(customerID, "1"), "deposit-qr-reservation", cashier), 201))
	qrDepositID := readID(expect(keyedCash(reservePath+"/"+qrReserveID+"/deposit", collectionBody(secondDepositSession, "qr", "30"),
		"deposit-full-qr", cashier), 201))
	prepaidSale := expect(keyedCash(salesPath, viewDeposit(bindReservation(payBody(secondDepositSession, `[]`), qrReserveID), "30"),
		"deposit-full-sale", cashier), 201)
	fullyPrepaid := prepaidSale.Body.String()
	if !strings.Contains(fullyPrepaid, `"payments":[]`) || !strings.Contains(fullyPrepaid, `"deposit":{"method":"qr","amount":"30.00"}`) {
		t.Fatalf("venta cubierta por seña electrónica: %s", fullyPrepaid)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE reservation_deposit_id=$1", qrDepositID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("la seña electrónica aumentó el arqueo: %d %v", auditCount, err)
	}

	cancelledReserve := readID(expect(keyedCash(reservePath, reserveBody(customerID, "1"), "deposit-cancelled-reservation", cashier), 201))
	cancelledDeposit := readID(expect(keyedCash(reservePath+"/"+cancelledReserve+"/deposit",
		collectionBody(secondDepositSession, "transfer", "31"), "deposit-cancelled-payment", cashier), 201))
	overTotal := viewDeposit(bindReservation(payBody(secondDepositSession, `[]`), cancelledReserve), "31")
	if conflict := expect(keyedCash(salesPath, overTotal, "deposit-exceeds-price", cashier), 409); !strings.Contains(conflict.Body.String(), "reservation_deposit_exceeds_total") {
		t.Fatalf("un precio por debajo de la seña requiere resolverla: %s", conflict.Body)
	}
	checkReserved(2, 1)
	request("POST", reservePath+"/"+cancelledReserve+"/cancel", storeInput, origin, cashier, 200)
	if got := request("GET", reservePath+cashQuery+"&status=CANCELLED", "", "", cashier, 200).Body.String(); !strings.Contains(got, cancelledDeposit) {
		t.Fatalf("la cancelación debe conservar el anticipo pendiente: %s", got)
	}
	secondDepositClose := request("POST", sessionsPath+"/"+secondDepositSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"32"}`, created.StoreID), origin, cashier, 200).Body.String()
	if !strings.Contains(secondDepositClose, `"expectedCash":"32.00"`) || !strings.Contains(secondDepositClose, `"difference":"0.00"`) {
		t.Fatalf("la entrega duplicó la seña o contó un cobro electrónico: %s", secondDepositClose)
	}
	checkReserved(2, 0)

	returnPath := func(id string) string { return salesPath + "/" + id + "/returns" }
	quarantineID := readID(request("POST", locationsPath,
		fmt.Sprintf(`{"storeId":"%s","code":"CUARENTENA","name":"Cuarentena","type":"warehouse","isSellable":false}`, created.StoreID), origin, cookie, 201))
	lineReturn := func(location, variant, disposition string, count int) string {
		return fmt.Sprintf(`{"locationId":"%s","variantId":"%s","disposition":"%s","quantity":%d}`, location, variant, disposition, count)
	}
	returnBody := func(reason, lines string) string {
		return fmt.Sprintf(`{"storeId":"%s","reason":%q,"items":[%s]}`, created.StoreID, reason, lines)
	}
	returnedFromReservation := returnPath(soldReservationID)
	sellableLine := lineReturn(sellable, variantID, "sellable", 1)
	nonSellableLine := lineReturn(quarantineID, variantID, "quarantine", 1)
	request("GET", returnedFromReservation+cashQuery, "", "", nil, 401)
	request("GET", returnedFromReservation+cashQuery, "", "", cashier, 404)
	request("GET", returnedFromReservation+cashQuery, "", "", second, 404)
	request("GET", returnedFromReservation+cashQuery, "", "", operator, 403)
	expect(keyedCash(returnedFromReservation, returnBody("", sellableLine), "return-missing-reason", manager), 400)
	expect(keyedCash(returnedFromReservation, returnBody("Duplicado", sellableLine+","+sellableLine), "return-duplicate-line", manager), 400)
	expect(keyedCash(returnedFromReservation, returnBody("Límite", lineReturn(sellable, variantID, "sellable", 3)), "return-too-many", manager), 409)
	expect(keyedCash(returnedFromReservation, returnBody("Variante ajena", lineReturn(sellable, spare, "sellable", 1)), "return-wrong-variant", manager), 409)
	expect(keyedCash(returnedFromReservation, returnBody("Ubicación ajena", lineReturn(northLocation, variantID, "quarantine", 1)), "return-cross-store", manager), 409)
	expect(keyedCash(returnedFromReservation, returnBody("Mercadería observada", lineReturn(sellable, variantID, "quarantine", 1)), "return-wrong-disposition", manager), 409)
	// Arrange the invalid destination second so the receipt fails after posting its first movement.
	firstDestination, wrongDestination := sellableLine, lineReturn(quarantineID, variantID, "sellable", 1)
	if quarantineID < sellable {
		firstDestination, wrongDestination = nonSellableLine, lineReturn(sellable, variantID, "quarantine", 1)
	}
	expect(keyedCash(returnedFromReservation, returnBody("Reversión completa", firstDestination+","+wrongDestination), "return-rollback", manager), 409)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE sale_id=$1 AND return_id IS NOT NULL", soldReservationID).
		Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("una recepción parcial fallida dejó movimientos: %d %v", auditCount, err)
	}

	checkReserved(2, 0)
	if history := request("GET", returnedFromReservation+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history, `"returns":[]`) ||
		!strings.Contains(history, `"sold":2,"returned":0`) {
		t.Fatalf("una devolución rechazada modificó la venta: %s", history)
	}
	var cashBefore int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&cashBefore); err != nil {
		t.Fatal(err)
	}
	request("PUT", itemPath(general, variantID), `{"amount":"80.00"}`, origin, cookie, 200)
	firstReturn := expect(keyedCash(returnedFromReservation, returnBody("  Prenda en buen estado  ", sellableLine), "return-first", manager), 201)
	firstReturnID := readID(firstReturn)
	if body := firstReturn.Body.String(); !strings.Contains(body, `"reason":"Prenda en buen estado"`) ||
		!strings.Contains(body, `"referenceAmount":"30.00"`) || !strings.Contains(body, `"disposition":"sellable"`) {
		t.Fatalf("la devolución debe mantener el precio histórico: %s", body)
	}
	if replay := expect(keyedCash(returnedFromReservation, returnBody("Prenda en buen estado", sellableLine), "return-first", manager), 201); replay.Body.String() != firstReturn.Body.String() {
		t.Fatal("el reintento debe devolver la misma recepción")
	}
	expect(keyedCash(returnedFromReservation, returnBody("Otro motivo", sellableLine), "return-first", manager), 409)
	checkReserved(3, 0)
	go func() {
		raceSales <- keyedCash(returnedFromReservation, returnBody("Última unidad A", nonSellableLine), "return-last-a", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(returnedFromReservation, returnBody("Última unidad B", nonSellableLine), "return-last-b", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("solo una operación debe devolver la última unidad: %d y %d", aSale, bSale)
	}
	if history := request("GET", returnedFromReservation+cashQuery+"&limit=1", "", "", manager, 200).Body.String(); !strings.Contains(history, `"hasMore":true`) ||
		!strings.Contains(history, `"sold":2,"returned":2`) || !strings.Contains(history, `"disposition":"quarantine"`) {
		t.Fatalf("historial de recepción paginado incorrecto: %s", history)
	}
	var onQuarantine int
	if err := db.QueryRow(ctx, "SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, quarantineID, variantID).Scan(&onQuarantine); err != nil || onQuarantine != 1 {
		t.Fatalf("la devolución a cuarentena se registró en otro lugar o se duplicó: %d %v", onQuarantine, err)
	}
	if detail := request("GET", "/api/v1/inventory/existences/"+variantID+"?store_id="+created.StoreID,
		"", "", manager, 200).Body.String(); !strings.Contains(detail, `"onHand":1,"reserved":0,"sellable":0`) || !strings.Contains(detail, quarantineID) {
		t.Fatalf("la devolución a cuarentena debe aportar stock físico sin disponibilidad: %s", detail)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE return_id=$1 AND sale_id=$2 AND movement_type='sale_return'",
		firstReturnID, soldReservationID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("movimiento de stock de devolución inválido: %d %v", auditCount, err)
	}
	if _, err := db.Exec(ctx, "UPDATE sale_returns SET reason='Otro motivo' WHERE id=$1", firstReturnID); err == nil {
		t.Fatal("las recepciones registradas deben ser inmutables")
	}

	expect(keyedCash(salesPath, strings.Replace(payBody(managerCollect, `[{"method":"qr","amount":"80"}]`), sellable, quarantineID, 1),
		"return-quarantine-cannot-sell", manager), 409)
	debtBefore := request("GET", partDebtPath+cashQuery, "", "", cashier, 200).Body.String()
	expect(keyedCash(returnPath(partCreditID), returnBody("Prenda de fiado", nonSellableLine), "return-credit-stock-only", cashier), 201)
	if debtAfter := request("GET", partDebtPath+cashQuery, "", "", cashier, 200).Body.String(); debtAfter != debtBefore {
		t.Fatalf("la recepción física modificó la deuda: %s", debtAfter)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != cashBefore {
		t.Fatalf("la recepción o el nuevo cambio modificaron la caja: %d %v", auditCount, err)
	}
	if ticket := request("GET", salesPath+"/"+soldReservationID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(ticket, finished.Body.String()) {
		t.Fatalf("la devolución modificó el ticket original: %s", ticket)
	}

	registerRefund := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-REINTEGROS"), origin, cookie, 201))
	sessionRefund := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, registerRefund, `"10"`), "refund-session", manager), 201))
	refundBody := func(session string) string {
		return fmt.Sprintf(`{"storeId":"%s","cashSessionId":"%s"}`, created.StoreID, session)
	}
	refundPath := func(sale, received string) string { return returnPath(sale) + "/" + received + "/refund" }
	checkRefundCode := func(res *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		expect(res, status)
		if !strings.Contains(res.Body.String(), code) {
			t.Fatalf("error de reintegro esperado %s: %s", code, res.Body)
		}
	}
	checkRefundCode(keyedCash(refundPath(soldReservationID, firstReturnID), refundBody(sessionRefund), "refund-qr", manager), 409, "return_refund_method_mismatch")
	depositReceipt := readID(expect(keyedCash(returnPath(deliveredDepositID), returnBody("Venta con seña", nonSellableLine), "return-from-deposit", cashier), 201))
	checkRefundCode(keyedCash(refundPath(deliveredDepositID, depositReceipt), refundBody(sessionRefund), "refund-deposit-without-cash", manager), 409, "cash_balance_conflict")
	receivedForRefund := expect(keyedCash(returnPath(saleID), returnBody("Reintegro de venta histórica", sellableLine), "refund-receipt", manager), 201)
	receivedForRefundID := readID(receivedForRefund)
	cashRefundPath := refundPath(saleID, receivedForRefundID)
	request("GET", returnPath(saleID)+cashQuery, "", "", second, 404)
	expect(keyedCash(cashRefundPath, refundBody(cashierCollect), "refund-forbidden", cashier), 404)
	expect(keyedCash(cashRefundPath, refundBody(managerSession), "refund-closed", manager), 409)
	expect(keyedCash(refundPath(saleID, firstReturnID), refundBody(sessionRefund), "refund-wrong-sale", manager), 404)
	checkRefundCode(keyedCash(cashRefundPath, refundBody(sessionRefund), "refund-balance", manager), 409, "cash_balance_conflict")
	if history := request("GET", returnPath(saleID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history, `"canRefundCash":true`) ||
		strings.Contains(history, `"refund":`) {
		t.Fatalf("un egreso fallido modificó el historial: %s", history)
	}
	cashFunding := fmt.Sprintf(`{"storeId":"%s","type":"manual_in","amount":"20","reason":"Fondo para reintegros"}`, created.StoreID)
	expect(keyedCash(sessionsPath+"/"+sessionRefund+"/movements", cashFunding, "refund-funding", manager), 201)
	type refundAttempt struct {
		key      string
		response *httptest.ResponseRecorder
	}
	refundRace := make(chan refundAttempt, 2)
	for _, key := range []string{"refund-race-a", "refund-race-b"} {
		go func() {
			refundRace <- refundAttempt{key, keyedCash(cashRefundPath, refundBody(sessionRefund), key, manager)}
		}()
	}
	winner, loser := <-refundRace, <-refundRace
	if winner.response.Code != 201 {
		winner, loser = loser, winner
	}
	expect(winner.response, 201)
	checkRefundCode(loser.response, 409, "return_already_refunded")
	refundID := readID(winner.response)
	if !strings.Contains(winner.response.Body.String(), `"amount":"25.40"`) {
		t.Fatalf("el reintegro debe corresponder al precio original: %s", winner.response.Body)
	}
	if movements := request("GET", sessionsPath+"/"+sessionRefund+"/movements"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(movements, `"type":"sale_refund_cash","amount":"-25.40"`) {
		t.Fatalf("el reintegro no quedó como egreso: %s", movements)
	}
	closeRefund := request("POST", sessionsPath+"/"+sessionRefund+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"4.60"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(closeRefund, `"expectedCash":"4.60"`) || !strings.Contains(closeRefund, `"difference":"0.00"`) {
		t.Fatalf("arqueo del reintegro incorrecto: %s", closeRefund)
	}
	if replay := expect(keyedCash(cashRefundPath, refundBody(sessionRefund), winner.key, manager), 201); replay.Body.String() != winner.response.Body.String() {
		t.Fatal("reintentar después del cierre debe devolver el reintegro original")
	}
	if replay := expect(keyedCash(returnPath(saleID), returnBody("Reintegro de venta histórica", sellableLine), "refund-receipt", manager), 201); replay.Body.String() != receivedForRefund.Body.String() {
		t.Fatal("resolver el reintegro no debe cambiar el resultado del registro físico")
	}
	if actual := request("GET", returnPath(saleID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(actual, `"refund":{"id":"`+refundID+`"`) {
		t.Fatalf("el reintegro no aparece junto a su devolución: %s", actual)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE return_refund_id=$1 AND amount=-25.40", refundID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("egreso en efectivo ausente o duplicado: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE return_id=$1", receivedForRefundID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("registrar el reintegro modificó el stock: %d %v", auditCount, err)
	}

	electronicRefund := func(session, method string) string {
		return strings.Replace(refundBody(session), "}", fmt.Sprintf(`,"method":"%s"}`, method), 1)
	}
	electronicRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-REINTEGROS-ELEC"), origin, cookie, 201))
	electronicSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, electronicRegister, `"0"`),
		"refund-electronic-session", manager), 201))
	qrRefundPath := refundPath(soldReservationID, firstReturnID)
	checkRefundCode(keyedCash(qrRefundPath, electronicRefund(electronicSession, "transfer"), "refund-wrong-method", manager), 409, "return_refund_method_mismatch")
	qrRefund := expect(keyedCash(qrRefundPath, electronicRefund(electronicSession, "qr"), "refund-qr-confirmed", manager), 201)
	if !strings.Contains(qrRefund.Body.String(), `"method":"qr","amount":"30.00"`) {
		t.Fatalf("el reintegro debe conservar el medio y el importe histórico: %s", qrRefund.Body)
	}
	if page := request("GET", returnedFromReservation+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(page, `"canRefundCash":false,"refundMethod":"qr"`) ||
		!strings.Contains(page, `"method":"qr","amount":"30.00"`) {
		t.Fatalf("el historial no muestra el medio electrónico del reintegro: %s", page)
	}
	mixedReceipt := readID(expect(keyedCash(returnPath(mixedID), returnBody("Pago mixto", nonSellableLine), "return-mixed-refund", cashier), 201))
	checkRefundCode(keyedCash(refundPath(mixedID, mixedReceipt), electronicRefund(electronicSession, "qr"),
		"refund-mixed-needs-allocation", manager), 409, "return_refund_allocation")
	var fiadoReceipt string
	if err := db.QueryRow(ctx, "SELECT id FROM sale_returns WHERE tenant_id=$1 AND sale_id=$2", created.TenantID, partCreditID).Scan(&fiadoReceipt); err != nil {
		t.Fatal(err)
	}
	checkRefundCode(keyedCash(refundPath(partCreditID, fiadoReceipt), electronicRefund(electronicSession, "cash"),
		"refund-credit-needs-explicit-amount", manager), 409, "return_refund_allocation")
	for _, method := range []string{"transfer", "debit", "credit"} {
		payment := fmt.Sprintf(`[{"method":"%s","amount":"80"}]`, method)
		body := strings.Replace(payBody(electronicSession, payment), `"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
		sale := readID(expect(keyedCash(salesPath, body, "refund-sale-"+method, manager), 201))
		receipt := readID(expect(keyedCash(returnPath(sale), returnBody("Entrega de devolución", sellableLine), "refund-return-"+method, manager), 201))
		reimbursed := expect(keyedCash(refundPath(sale, receipt), electronicRefund(electronicSession, method), "refund-electronic-"+method, manager), 201)
		if !strings.Contains(reimbursed.Body.String(), fmt.Sprintf(`"method":"%s","amount":"80.00"`, method)) {
			t.Fatalf("reintegro electrónico incorrecto: %s", reimbursed.Body)
		}
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE cash_session_id=$1", electronicSession).
		Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("las operaciones electrónicas alteraron el efectivo: %d %v", auditCount, err)
	}
	closedElectronic := request("POST", sessionsPath+"/"+electronicSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"0"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(closedElectronic, `"expectedCash":"0.00"`) || !strings.Contains(closedElectronic, `"difference":"0.00"`) {
		t.Fatalf("los reintegros electrónicos alteraron el arqueo: %s", closedElectronic)
	}
	if replay := expect(keyedCash(qrRefundPath, electronicRefund(electronicSession, "qr"), "refund-qr-confirmed", manager), 201); replay.Body.String() != qrRefund.Body.String() {
		t.Fatal("un reintento de reintegro por QR debe devolver el original")
	}
	checkRefundCode(keyedCash(qrRefundPath, electronicRefund(electronicSession, "credit"), "refund-qr-confirmed", manager), 409, "idempotency_conflict")

	mixedRefundBody := func(session, payments string) string {
		return strings.Replace(refundBody(session), "}", `,"payments":`+payments+"}", 1)
	}
	mixedRefundRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-REINTEGRO-MIXTO"), origin, cookie, 201))
	mixedRefundSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, mixedRefundRegister, `"0"`),
		"mixed-refund-open", manager), 201))
	twoPaid := strings.Replace(payBody(mixedRefundSession, `[{"method":"cash","amount":"100","cashReceived":"100"},{"method":"qr","amount":"60"}]`),
		`"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
	twoPaid = strings.Replace(twoPaid, `"quantity":1`, `"quantity":2`, 1)
	mixedRefundSale := readID(expect(keyedCash(salesPath, twoPaid, "mixed-refund-sale", manager), 201))
	mixedReturnOne := readID(expect(keyedCash(returnPath(mixedRefundSale), returnBody("Primer artículo", sellableLine), "mixed-return-one", manager), 201))
	mixedReturnTwo := readID(expect(keyedCash(returnPath(mixedRefundSale), returnBody("Segundo artículo", nonSellableLine), "mixed-return-two", manager), 201))
	mixedRefundOnePath, mixedRefundTwoPath := refundPath(mixedRefundSale, mixedReturnOne), refundPath(mixedRefundSale, mixedReturnTwo)
	checkRefundCode(keyedCash(mixedRefundOnePath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"80"},{"method":"cash","amount":"1"}]`),
		"mixed-duplicate-method", manager), 400, "invalid_request")
	checkRefundCode(keyedCash(mixedRefundOnePath, mixedRefundBody(mixedRefundSession, `[{"method":"transfer","amount":"80"}]`),
		"mixed-unpaid-method", manager), 409, "return_refund_method_mismatch")
	checkRefundCode(keyedCash(mixedRefundOnePath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"10"},{"method":"qr","amount":"60"}]`),
		"mixed-incomplete", manager), 409, "return_refund_allocation")
	checkRefundCode(keyedCash(mixedRefundOnePath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"10"},{"method":"qr","amount":"70"}]`),
		"mixed-cap", manager), 409, "return_refund_allocation")
	firstSplit := mixedRefundBody(mixedRefundSession, `[{"method":"qr","amount":"60"},{"method":"cash","amount":"20"}]`)
	mixedSaved := expect(keyedCash(mixedRefundOnePath, firstSplit, "mixed-refund-one", manager), 201)
	if !strings.Contains(mixedSaved.Body.String(), `"method":null,"amount":"80.00"`) ||
		!strings.Contains(mixedSaved.Body.String(), `"payments":[{"method":"cash","amount":"20.00"},{"method":"qr","amount":"60.00"}]`) {
		t.Fatalf("desglose de reintegro mixto incorrecto: %s", mixedSaved.Body)
	}
	if got := request("GET", returnPath(mixedRefundSale)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"refundOptions":[{"method":"cash","amount":"80.00"},{"method":"qr","amount":"0.00"}]`) {
		t.Fatalf("saldo del reintegro por medio incorrecto: %s", got)
	}
	checkRefundCode(keyedCash(mixedRefundTwoPath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"79"},{"method":"qr","amount":"1"}]`),
		"mixed-cumulative-cap", manager), 409, "return_refund_allocation")
	expect(keyedCash(mixedRefundTwoPath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"80"}]`),
		"mixed-refund-two", manager), 201)
	reorderedSplit := mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"20.00"},{"method":"qr","amount":"60.0"}]`)
	checkRefundCode(keyedCash(mixedRefundOnePath, mixedRefundBody(mixedRefundSession, `[{"method":"cash","amount":"19"},{"method":"qr","amount":"61"}]`),
		"mixed-refund-one", manager), 409, "idempotency_conflict")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE cash_session_id=$1 AND movement_type='sale_refund_cash' AND amount IN (-20,-80)", mixedRefundSession).
		Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("arqueo de reintegro mixto incorrecto: %d %v", auditCount, err)
	}
	mixedClosed := request("POST", sessionsPath+"/"+mixedRefundSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"0"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(mixedClosed, `"expectedCash":"0.00"`) || !strings.Contains(mixedClosed, `"difference":"0.00"`) {
		t.Fatalf("el arqueo registró más que la parte en efectivo: %s", mixedClosed)
	}
	if replay := expect(keyedCash(mixedRefundOnePath, reorderedSplit, "mixed-refund-one", manager), 201); replay.Body.String() != mixedSaved.Body.String() {
		t.Fatal("reordenar o normalizar importes debe conservar el reintegro original")
	}

	depositRefundRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-REINTEGRO-SENA"), origin, cookie, 201))
	depositRefundSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, depositRefundRegister, `"70"`),
		"deposit-refund-open", manager), 201))
	if got := request("GET", returnPath(deliveredDepositID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"refundOptions":[{"method":"cash","amount":"30.00"}],"deposit":{"method":"cash","amount":"8.00"}`) {
		t.Fatalf("la seña y el saldo cobrados con un mismo medio deben sumarse: %s", got)
	}
	sameMethodRefund := expect(keyedCash(refundPath(deliveredDepositID, depositReceipt), refundBody(depositRefundSession), "refund-cash-deposit", manager), 201)
	if !strings.Contains(sameMethodRefund.Body.String(), `"payments":[{"method":"cash","amount":"30.00"}]`) {
		t.Fatalf("el reintegro íntegro debe incluir la seña y el pago de entrega: %s", sameMethodRefund.Body)
	}
	prepaidSaleID := readID(prepaidSale)
	prepaidReceipt := readID(expect(keyedCash(returnPath(prepaidSaleID), returnBody("Cubierta por seña QR", nonSellableLine), "prepaid-return", cashier), 201))
	prepaidRefund := expect(keyedCash(refundPath(prepaidSaleID, prepaidReceipt), electronicRefund(depositRefundSession, "qr"),
		"prepaid-qr-refund", manager), 201)
	if !strings.Contains(prepaidRefund.Body.String(), `"payments":[{"method":"qr","amount":"30.00"}]`) {
		t.Fatalf("la seña debe ser reintegrable aunque no haya pagos al entregar: %s", prepaidRefund.Body)
	}

	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock para reintegro con seña"), "seed-deposit-refund", manager), 201)
	mixedDepositReservation := readID(expect(keyedCash(reservePath, reserveBody(customerID, "2"), "mixed-deposit-reservation", manager), 201))
	expect(keyedCash(reservePath+"/"+mixedDepositReservation+"/deposit", collectionBody(depositRefundSession, "cash", "35"),
		"mixed-deposit-record", manager), 201)
	deliverMixedDeposit := strings.Replace(payBody(depositRefundSession, `[{"method":"qr","amount":"125"}]`),
		`"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
	deliverMixedDeposit = strings.Replace(deliverMixedDeposit, `"quantity":1`, `"quantity":2`, 1)
	mixedDepositSale := readID(expect(keyedCash(salesPath, viewDeposit(bindReservation(deliverMixedDeposit, mixedDepositReservation), "35"),
		"mixed-deposit-sale", manager), 201))
	mixedDepositFirst := readID(expect(keyedCash(returnPath(mixedDepositSale), returnBody("Primera devolución con seña", sellableLine),
		"mixed-deposit-return-first", manager), 201))
	mixedDepositSecond := readID(expect(keyedCash(returnPath(mixedDepositSale), returnBody("Segunda devolución con seña", nonSellableLine),
		"mixed-deposit-return-second", manager), 201))
	mixedDepositRefundPath := refundPath(mixedDepositSale, mixedDepositFirst)
	checkRefundCode(keyedCash(mixedDepositRefundPath, mixedRefundBody(depositRefundSession, `[{"method":"cash","amount":"36"},{"method":"qr","amount":"44"}]`),
		"mixed-deposit-cash-cap", manager), 409, "return_refund_allocation")
	mixedWithDeposit := mixedRefundBody(depositRefundSession, `[{"method":"qr","amount":"45"},{"method":"cash","amount":"35"}]`)
	mixedDepositRefund := expect(keyedCash(mixedDepositRefundPath, mixedWithDeposit, "mixed-deposit-refund", manager), 201)
	if !strings.Contains(mixedDepositRefund.Body.String(), `"payments":[{"method":"cash","amount":"35.00"},{"method":"qr","amount":"45.00"}]`) {
		t.Fatalf("desglose con seña incorrecto: %s", mixedDepositRefund.Body)
	}
	if got := request("GET", returnPath(mixedDepositSale)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"refundOptions":[{"method":"cash","amount":"0.00"},{"method":"qr","amount":"80.00"}]`) {
		t.Fatalf("el saldo debe descontar el reintegro previo de la seña: %s", got)
	}
	mixedDepositSecondPath := refundPath(mixedDepositSale, mixedDepositSecond)
	checkRefundCode(keyedCash(mixedDepositSecondPath, mixedRefundBody(depositRefundSession, `[{"method":"cash","amount":"1"},{"method":"qr","amount":"79"}]`),
		"mixed-deposit-consumed", manager), 409, "return_refund_allocation")
	expect(keyedCash(mixedDepositSecondPath, mixedRefundBody(depositRefundSession, `[{"method":"qr","amount":"80"}]`),
		"mixed-deposit-remainder", manager), 201)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE cash_session_id=$1 AND movement_type='sale_refund_cash'",
		depositRefundSession).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("solo los reintegros en efectivo deben tener movimiento de caja: %d %v", auditCount, err)
	}
	depositRefundClose := request("POST", sessionsPath+"/"+depositRefundSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"40"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(depositRefundClose, `"expectedCash":"40.00"`) || !strings.Contains(depositRefundClose, `"difference":"0.00"`) {
		t.Fatalf("arqueo con seña y reintegros incorrecto: %s", depositRefundClose)
	}
	if replay := expect(keyedCash(mixedDepositRefundPath, mixedWithDeposit, "mixed-deposit-refund", manager), 201); replay.Body.String() != mixedDepositRefund.Body.String() {
		t.Fatal("la devolución con seña debe mantener su reintegro al reintentar después del cierre")
	}

	debtCreditPath := func(sale, receipt string) string {
		return returnPath(sale) + "/" + receipt + "/debt-credits"
	}
	debtCreditBody := func(amount string) string {
		return fmt.Sprintf(`{"storeId":"%s","amount":"%s"}`, created.StoreID, amount)
	}
	partCreditAdjustment := debtCreditPath(partCreditID, fiadoReceipt)
	expect(keyedCash(partCreditAdjustment, debtCreditBody("1"), "debt-credit-forbidden", second), 404)
	expect(keyedCash(partCreditAdjustment, debtCreditBody("1"), "debt-credit-operator", operator), 403)
	expect(keyedCash(debtCreditPath(deliveredDepositID, depositReceipt), debtCreditBody("1"), "debt-credit-cash-sale", manager), 404)
	expect(keyedCash(debtCreditPath(partCreditID, depositReceipt), debtCreditBody("1"), "debt-credit-wrong-receipt", manager), 404)
	expect(keyedCash(partCreditAdjustment, debtCreditBody("0"), "debt-credit-zero", cashier), 400)
	checkRefundCode(keyedCash(partCreditAdjustment, debtCreditBody("4.26"), "debt-credit-exceeds-balance", cashier), 409, "return_debt_credit_amount")
	var cashBeforeCredit, stockBeforeCredit int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM cash_movements WHERE tenant_id=$1),
		(SELECT count(*) FROM stock_movements WHERE tenant_id=$1)`, created.TenantID).Scan(&cashBeforeCredit, &stockBeforeCredit); err != nil {
		t.Fatal(err)
	}
	beforeAdjustment := readReceivablesSummary(cashier, "")
	firstAdjustment := expect(keyedCash(partCreditAdjustment, debtCreditBody("1.25"), "debt-credit-first", cashier), 201)
	if total := readReceivablesSummary(cashier, ""); total.Collected != beforeAdjustment.Collected || total.DebtCredits != "1.25" {
		t.Fatalf("la compensación debe figurar separada de los abonos: %+v", total)
	}
	firstAdjustmentID := readID(firstAdjustment)
	if !strings.Contains(firstAdjustment.Body.String(), `"amount":"1.25","balanceAfter":"3.00"`) {
		t.Fatalf("compensación parcial incorrecta: %s", firstAdjustment.Body)
	}
	if retry := expect(keyedCash(partCreditAdjustment, debtCreditBody("1.25"), "debt-credit-first", cashier), 201); retry.Body.String() != firstAdjustment.Body.String() {
		t.Fatal("el reintento debe conservar el ajuste original")
	}
	checkRefundCode(keyedCash(partCreditAdjustment, debtCreditBody("2"), "debt-credit-first", cashier), 409, "idempotency_conflict")
	// Both operations consume the same remaining balance and must serialize on the same receivable.
	go func() {
		raceSales <- keyedCash(partCreditAdjustment, debtCreditBody("3"), "debt-credit-race", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(partDebtPath, collectionBody(managerCollect, "qr", "3"), "payment-debt-credit-race", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("un abono y una compensación aprobaron el mismo saldo: %d y %d", aSale, bSale)
	}
	expect(keyedCash(partCreditAdjustment, debtCreditBody("0.01"), "debt-credit-no-balance", manager), 409)
	if got := request("GET", partDebtPath+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(got, firstAdjustmentID) ||
		!strings.Contains(got, `"outstandingAmount":"0.00"`) {
		t.Fatalf("el fiado debe mostrar el ajuste y el saldo actual: %s", got)
	}
	if got := request("GET", returnPath(partCreditID)+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(got,
		`"outstandingDebt":"0.00"`) || !strings.Contains(got, firstAdjustmentID) || strings.Contains(got, `"refund":`) {
		t.Fatalf("la recepción debe conservar la compensación y el importe pendiente de reintegro: %s", got)
	}
	var cashAfterCredit, stockAfterCredit int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM cash_movements WHERE tenant_id=$1),
		(SELECT count(*) FROM stock_movements WHERE tenant_id=$1)`, created.TenantID).Scan(&cashAfterCredit, &stockAfterCredit); err != nil ||
		cashAfterCredit != cashBeforeCredit || stockAfterCredit != stockBeforeCredit {
		t.Fatalf("la compensación cambió caja o inventario: %d/%d %v", cashAfterCredit, stockAfterCredit, err)
	}
	if got := request("GET", salesPath+"/"+partCreditID+"/ticket"+cashQuery, "", "", cashier, 200).Body.String(); !strings.Contains(got, partCredit.Body.String()) {
		t.Fatalf("la compensación modificó el ticket original: %s", got)
	}

	debtTestBody := strings.Replace(payBody(managerCollect, `[]`), `"payments":`,
		fmt.Sprintf(`"onCredit":true,"customerId":"%s","payments":`, customerID), 1)
	debtTestBody = strings.Replace(debtTestBody, `"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
	debtTestBody = strings.Replace(debtTestBody, `"quantity":1`, `"quantity":2`, 1)
	debtTestSale := readID(expect(keyedCash(salesPath, debtTestBody, "debt-credit-second-sale", manager), 201))
	debtTestFirstReceipt := readID(expect(keyedCash(returnPath(debtTestSale), returnBody("Devolución parcial fiada", sellableLine),
		"debt-credit-capped-receipt", manager), 201))
	debtTestCreditPath := debtCreditPath(debtTestSale, debtTestFirstReceipt)
	expect(keyedCash(debtTestCreditPath, debtCreditBody("40"), "debt-credit-receipt-part", manager), 201)
	checkRefundCode(keyedCash(debtTestCreditPath, debtCreditBody("40.01"), "debt-credit-exceeds-receipt", manager), 409, "return_debt_credit_amount")
	expect(keyedCash(debtTestCreditPath, debtCreditBody("40"), "debt-credit-receipt-rest", manager), 201)
	checkRefundCode(keyedCash(debtTestCreditPath, debtCreditBody("1"), "debt-credit-receipt-consumed", manager), 409, "return_debt_credit_amount")
	debtTestSecondReceipt := readID(expect(keyedCash(returnPath(debtTestSale), returnBody("Segunda devolución fiada", sellableLine),
		"debt-credit-settled-receipt", manager), 201))
	expect(keyedCash(debtCreditPath(debtTestSale, debtTestSecondReceipt), debtCreditBody("80"), "debt-credit-settled-sale", manager), 201)
	if got := request("GET", receivablesPath+cashQuery+"&status=settled", "", "", manager, 200).Body.String(); !strings.Contains(got, debtTestSale) {
		t.Fatalf("el fiado compensado debe aparecer en saldados: %s", got)
	}
	if _, err := db.Exec(ctx, "UPDATE sale_return_debt_credits SET amount=2 WHERE id=$1", firstAdjustmentID); err == nil {
		t.Fatal("la compensación registrada debe ser inmutable")
	}

	fiadoRefundRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-REINTEGRO-FIADO"), origin, cookie, 201))
	fiadoRefundSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoRefundRegister, `"50"`),
		"fiado-refund-open", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 4, "Stock de prueba para devolución de fiado"),
		"fiado-refund-stock", manager), 201)
	fiadoSaleBody := func(payments string) string {
		body := strings.Replace(payBody(fiadoRefundSession, payments), `"payments":`, fmt.Sprintf(`"onCredit":true,"customerId":"%s","payments":`, customerID), 1)
		body = strings.Replace(body, `"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
		return strings.Replace(body, `"quantity":1`, `"quantity":2`, 1)
	}
	fiadoRefundSale := readID(expect(keyedCash(salesPath, fiadoSaleBody(`[{"method":"cash","amount":"20","cashReceived":"20"},{"method":"qr","amount":"15"}]`),
		"fiado-refund-venta", manager), 201))
	var fiadoRefundDebt string
	if err := db.QueryRow(ctx, "SELECT id FROM receivables WHERE tenant_id=$1 AND sale_id=$2", created.TenantID, fiadoRefundSale).Scan(&fiadoRefundDebt); err != nil {
		t.Fatal(err)
	}
	expect(keyedCash(receivablesPath+"/"+fiadoRefundDebt+"/payments", collectionBody(fiadoRefundSession, "debit", "15"),
		"fiado-refund-abono", manager), 201)
	fiadoFirstReceipt := readID(expect(keyedCash(returnPath(fiadoRefundSale), returnBody("Primera recepción del fiado", sellableLine), "fiado-refund-receive-first", manager), 201))
	fiadoSecondReceipt := readID(expect(keyedCash(returnPath(fiadoRefundSale), returnBody("Segunda recepción del fiado", sellableLine), "fiado-refund-receive-second", manager), 201))
	fiadoFirstRefundPath := refundPath(fiadoRefundSale, fiadoFirstReceipt)
	checkRefundCode(keyedCash(fiadoFirstRefundPath, mixedRefundBody(fiadoRefundSession, `[{"method":"cash","amount":"80"}]`),
		"fiado-refund-uncovered", manager), 409, "return_refund_allocation")
	expect(keyedCash(debtCreditPath(fiadoRefundSale, fiadoFirstReceipt), debtCreditBody("60"), "fiado-refund-credit-first", manager), 201)
	checkRefundCode(keyedCash(fiadoFirstRefundPath, mixedRefundBody(fiadoRefundSession, `[{"method":"cash","amount":"20"},{"method":"qr","amount":"1"}]`),
		"fiado-refund-too-much", manager), 409, "return_refund_allocation")
	fiadoFirstBreakdown := mixedRefundBody(fiadoRefundSession, `[{"method":"cash","amount":"12"},{"method":"qr","amount":"8"}]`)
	fiadoFirstRefund := expect(keyedCash(fiadoFirstRefundPath, fiadoFirstBreakdown, "fiado-refund-applied-first", manager), 201)
	if !strings.Contains(fiadoFirstRefund.Body.String(), `"amount":"20.00"`) {
		t.Fatalf("el reintegro debe descontar la compensación de deuda: %s", fiadoFirstRefund.Body)
	}
	if got := request("GET", returnPath(fiadoRefundSale)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"refundOptions":[{"method":"cash","amount":"8.00"},{"method":"debit","amount":"15.00"},{"method":"qr","amount":"7.00"}]`) ||
		!strings.Contains(got, `"outstandingDebt":"50.00"`) {
		t.Fatalf("saldo reintegrable incorrecto después del ajuste y reintegro: %s", got)
	}
	checkRefundCode(keyedCash(debtCreditPath(fiadoRefundSale, fiadoFirstReceipt), debtCreditBody("0.01"), "fiado-refund-already-resolved", manager),
		409, "return_debt_credit_amount")
	expect(keyedCash(debtCreditPath(fiadoRefundSale, fiadoSecondReceipt), debtCreditBody("50"), "fiado-refund-credit-second", manager), 201)
	fiadoSecondRefund := expect(keyedCash(refundPath(fiadoRefundSale, fiadoSecondReceipt),
		mixedRefundBody(fiadoRefundSession, `[{"method":"cash","amount":"8"},{"method":"debit","amount":"15"},{"method":"qr","amount":"7"}]`),
		"fiado-refund-applied-second", manager), 201)
	if !strings.Contains(fiadoSecondRefund.Body.String(), `{"method":"debit","amount":"15.00"}`) {
		t.Fatalf("el reintegro debe incluir el medio de un abono posterior: %s", fiadoSecondRefund.Body)
	}

	fiadoRaceSale := readID(expect(keyedCash(salesPath, fiadoSaleBody(`[{"method":"qr","amount":"100"}]`), "fiado-refund-race-sale", manager), 201))
	fiadoRaceReceipt := readID(expect(keyedCash(returnPath(fiadoRaceSale), returnBody("Recepción concurrente", sellableLine), "fiado-refund-race-receive", manager), 201))
	go func() {
		raceSales <- keyedCash(refundPath(fiadoRaceSale, fiadoRaceReceipt), mixedRefundBody(fiadoRefundSession, `[{"method":"qr","amount":"80"}]`),
			"fiado-refund-race-a", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(debtCreditPath(fiadoRaceSale, fiadoRaceReceipt), debtCreditBody("20"), "fiado-refund-race-b", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("el reintegro y la compensación no pueden consumir el mismo importe de devolución: %d y %d", aSale, bSale)
	}
	closeFiadoRefund := request("POST", sessionsPath+"/"+fiadoRefundSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"50"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(closeFiadoRefund, `"expectedCash":"50.00"`) || !strings.Contains(closeFiadoRefund, `"difference":"0.00"`) {
		t.Fatalf("los reintegros del fiado deben descontar solo el efectivo devuelto: %s", closeFiadoRefund)
	}
	if replay := expect(keyedCash(fiadoFirstRefundPath, fiadoFirstBreakdown, "fiado-refund-applied-first", manager), 201); replay.Body.String() != fiadoFirstRefund.Body.String() {
		t.Fatal("el reintegro del fiado debe mantener su resultado al reintentar después del cierre")
	}

	exchangePath := func(sale, received string) string { return returnPath(sale) + "/" + received + "/exchange" }
	exchangeInput := func(locationID, variant, expected string, quantity int) string {
		return fmt.Sprintf(`{"storeId":"%s","locationId":"%s","items":[{"variantId":"%s","expectedUnitPrice":"%s","quantity":%d}]}`,
			created.StoreID, locationID, variant, expected, quantity)
	}
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Prendas para probar cambio"), "exchange-original-stock", manager), 201)
	paidForExchange := strings.Replace(payBody(managerCollect, `[{"method":"qr","amount":"160"}]`), `"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
	paidForExchange = strings.Replace(paidForExchange, `"quantity":1`, `"quantity":2`, 1)
	originalExchangeSale := expect(keyedCash(salesPath, paidForExchange, "exchange-sale", manager), 201)
	originalExchangeID := readID(originalExchangeSale)
	exchangeReceipt := readID(expect(keyedCash(returnPath(originalExchangeID), returnBody("Cambio de talle", nonSellableLine), "exchange-receipt-one", manager), 201))
	exchangeReceiptTwo := readID(expect(keyedCash(returnPath(originalExchangeID), returnBody("Segunda prenda", sellableLine), "exchange-receipt-two", manager), 201))
	changePath := exchangePath(originalExchangeID, exchangeReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"80.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 1, "Prenda para entregar en un cambio"), "exchange-replacement-stock", manager), 201)
	heldReplacement := strings.ReplaceAll(reserveBody(customerID, "1"), variantID, spare)
	reserveReplacement := readID(expect(keyedCash(reservePath, heldReplacement, "exchange-replacement-reserved", manager), 201))
	matchingExchange := exchangeInput(sellable, spare, "80", 1)
	expect(keyedCash(changePath, matchingExchange, "exchange-no-key-actor", second), 404)
	expect(keyedCash(changePath, matchingExchange, "exchange-operator", operator), 403)
	expect(keyedCash(exchangePath(originalExchangeID, depositReceipt), matchingExchange, "exchange-wrong-receipt", manager), 404)
	unpricedProduct := readID(request("POST", "/api/v1/products", `{"name":"Prenda de cambio sin precio"}`, origin, cookie, 201))
	unpricedReplacement := readID(request("POST", "/api/v1/products/"+unpricedProduct+"/variants", `{"sku":"CAMBIO-SIN-PRECIO"}`, origin, cookie, 201))
	checkRefundCode(keyedCash(changePath, exchangeInput(sellable, unpricedReplacement, "0", 1), "exchange-missing-price", manager),
		409, "sale_price_missing")

	checkRefundCode(keyedCash(changePath, exchangeInput(sellable, spare, "79", 1), "exchange-stale-price", manager), 409, "sale_price_changed")
	checkRefundCode(keyedCash(changePath, exchangeInput(sellable, spare, "80", 2), "exchange-price-difference", manager), 409, "return_exchange_difference")
	checkRefundCode(keyedCash(changePath, exchangeInput(quarantineID, spare, "80", 1), "exchange-quarantine-output", manager), 409, "sale_location_unavailable")
	checkRefundCode(keyedCash(changePath, matchingExchange, "exchange-stock-reserved", manager), 409, "return_exchange_stock")
	checkRefundCode(keyedCash(exchangePath(deliveredDepositID, depositReceipt), matchingExchange, "exchange-already-refunded", manager),
		409, "return_exchange_unavailable")
	checkRefundCode(keyedCash(exchangePath(partCreditID, fiadoReceipt), matchingExchange, "exchange-fiado", manager),
		409, "return_exchange_unavailable")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_exchanges WHERE sale_id=$1", originalExchangeID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un cambio fallido modificó el historial: %d %v", auditCount, err)
	}
	request("POST", reservePath+"/"+reserveReplacement+"/cancel", storeInput, origin, manager, 200)
	var cashBeforeExchange int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&cashBeforeExchange); err != nil {
		t.Fatal(err)
	}
	savedExchange := expect(keyedCash(changePath, matchingExchange, "exchange-confirmed", manager), 201)
	exchangeID := readID(savedExchange)
	if got := savedExchange.Body.String(); !strings.Contains(got, `"total":"80.00"`) || !strings.Contains(got, spare) {
		t.Fatalf("el cambio debe guardar las prendas entregadas y su importe: %s", got)
	}
	checkRefundCode(keyedCash(refundPath(originalExchangeID, exchangeReceipt), electronicRefund(managerCollect, "qr"),
		"refund-exchanged-return", manager), 409, "return_exchange_unavailable")
	request("PUT", itemPath(general, spare), `{"amount":"81.00"}`, origin, cookie, 200)
	if replay := expect(keyedCash(changePath, matchingExchange, "exchange-confirmed", manager), 201); replay.Body.String() != savedExchange.Body.String() {
		t.Fatal("el reintento debe conservar el cambio original después de modificar el precio")
	}
	checkRefundCode(keyedCash(changePath, exchangeInput(sellable, spare, "81", 1), "exchange-confirmed", manager), 409, "idempotency_conflict")
	if got := request("GET", returnPath(originalExchangeID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"exchange":{"id":"`+exchangeID+`"`) || !strings.Contains(got, `"unitPrice":"80.00"`) {
		t.Fatalf("el historial debe mostrar el cambio con su precio congelado: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE exchange_id=$1 AND sale_id=$2 AND quantity_delta=-1",
		exchangeID, originalExchangeID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("la salida de stock debe quedar registrada una vez: %d %v", auditCount, err)
	}
	request("PUT", itemPath(general, spare), `{"amount":"80.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 1, "Stock para dos operaciones concurrentes"), "exchange-race-stock", manager), 201)
	go func() {
		raceSales <- keyedCash(exchangePath(originalExchangeID, exchangeReceiptTwo), matchingExchange, "exchange-race", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(refundPath(originalExchangeID, exchangeReceiptTwo), electronicRefund(managerCollect, "qr"), "exchange-refund-race", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("el cambio y el reintegro no pueden resolver la misma devolución: %d y %d", aSale, bSale)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != cashBeforeExchange {
		t.Fatalf("el cambio de igual importe modificó el arqueo: %d %v", auditCount, err)
	}
	if got := request("GET", salesPath+"/"+originalExchangeID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, originalExchangeSale.Body.String()) {
		t.Fatalf("el cambio modificó el ticket original: %s", got)
	}

	chargeRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-DIFERENCIA-CAMBIO"), origin, cookie, 201))
	chargeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, chargeRegister, `"50"`), "exchange-charge-open", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock para venta con cambio"), "exchange-charge-original-stock", manager), 201)
	chargeSale := expect(keyedCash(salesPath, paidForExchange, "exchange-charge-sale", manager), 201)
	chargeSaleID := readID(chargeSale)
	chargeReceiptOne := readID(expect(keyedCash(returnPath(chargeSaleID), returnBody("Cambio de mayor importe", sellableLine), "exchange-cash-receipt", manager), 201))
	chargeReceiptTwo := readID(expect(keyedCash(returnPath(chargeSaleID), returnBody("Cambio de mayor importe por QR", sellableLine), "exchange-qr-receipt", manager), 201))
	cashChargePath := exchangePath(chargeSaleID, chargeReceiptOne)
	request("PUT", itemPath(general, spare), `{"amount":"100.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Stock para cambios con diferencia"), "exchange-charge-replacement", manager), 201)
	chargePayload := exchangeInput(sellable, spare, "100", 1)
	chargeBody := func(session, method, amount, tender string) string {
		charge := fmt.Sprintf(`,"charge":{"cashSessionId":"%s","method":"%s","amount":"%s"`, session, method, amount)
		if tender != "" {
			charge += fmt.Sprintf(`,"cashReceived":"%s"`, tender)
		}
		return strings.TrimSuffix(chargePayload, "}") + charge + "}}"
	}
	checkRefundCode(keyedCash(cashChargePath, chargePayload, "exchange-charge-missing", manager), 409, "return_exchange_difference")
	checkRefundCode(keyedCash(cashChargePath, chargeBody(chargeSession, "qr", "19.99", ""), "exchange-charge-wrong-amount", manager), 409, "return_exchange_charge_amount")
	checkRefundCode(keyedCash(cashChargePath, chargeBody(chargeSession, "cash", "20", "19.99"), "exchange-charge-short-tender", manager), 409, "sale_insufficient_cash")
	checkRefundCode(keyedCash(cashChargePath, chargeBody(fiadoRefundSession, "cash", "20", "25"), "exchange-charge-closed", manager), 409, "cash_session_state_conflict")
	checkRefundCode(keyedCash(cashChargePath, chargeBody(chargeSession, "qr", "20", "20"), "exchange-charge-electronic-tender", manager), 400, "invalid_request")
	var spareBeforeCharge int
	if err := db.QueryRow(ctx, "SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, sellable, spare).Scan(&spareBeforeCharge); err != nil {
		t.Fatal(err)
	}
	checkRefundCode(keyedCash(cashChargePath, chargeBody(managerCollect, "cash", "20", "25"), "exchange-charge-cash-limit", manager), 409, "cash_balance_conflict")
	var spareAfterFailure int
	if err := db.QueryRow(ctx, "SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, sellable, spare).Scan(&spareAfterFailure); err != nil || spareAfterFailure != spareBeforeCharge {
		t.Fatalf("un cobro fallido modificó el stock de reemplazo: %d %v", spareAfterFailure, err)
	}

	cashChargeBody := chargeBody(chargeSession, "cash", "20", "25")
	cashCharged := expect(keyedCash(cashChargePath, cashChargeBody, "exchange-cash-charge", manager), 201)
	cashChargedID := readID(cashCharged)
	if got := cashCharged.Body.String(); !strings.Contains(got, `"total":"100.00"`) ||
		!strings.Contains(got, `"method":"cash","amount":"20.00","cashReceived":"25.00","change":"5.00"`) {
		t.Fatalf("el cambio debe conservar el importe de reemplazo y el vuelto: %s", got)
	}
	if got := request("GET", returnPath(chargeSaleID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, cashCharged.Body.String()) {
		t.Fatalf("el historial debe conservar el cobro del cambio: %s", got)
	}
	qrCharged := expect(keyedCash(exchangePath(chargeSaleID, chargeReceiptTwo), chargeBody(chargeSession, "qr", "20", ""),
		"exchange-qr-charge", manager), 201)
	if !strings.Contains(qrCharged.Body.String(), `"method":"qr","amount":"20.00"`) || strings.Contains(qrCharged.Body.String(), `"change":`) {
		t.Fatalf("cobro de diferencia por QR incorrecto: %s", qrCharged.Body)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE cash_session_id=$1 AND exchange_id=$2 AND amount=20",
		chargeSession, cashChargedID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el arqueo debe registrar un ingreso neto de 20 por el cambio: %d %v", auditCount, err)
	}
	chargeClosed := request("POST", sessionsPath+"/"+chargeSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"70"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(chargeClosed, `"expectedCash":"70.00"`) || !strings.Contains(chargeClosed, `"difference":"0.00"`) {
		t.Fatalf("arqueo incorrecto después de cambios: %s", chargeClosed)
	}
	if replay := expect(keyedCash(cashChargePath, chargeBody(chargeSession, "cash", "20.00", "25.0"), "exchange-cash-charge", manager), 201); replay.Body.String() != cashCharged.Body.String() {
		t.Fatal("el reintento posterior al cierre debe devolver el cobro original")
	}
	if got := request("GET", salesPath+"/"+chargeSaleID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, chargeSale.Body.String()) {
		t.Fatalf("cobrar una diferencia modificó el ticket original: %s", got)
	}

	mixedChargeRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-DIF-CAMBIO-MIXTO"), origin, cookie, 201))
	mixedChargeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, mixedChargeRegister, `"10"`),
		"exchange-mixed-charge-register", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock de venta para cambio mixto"), "exchange-mixed-origin-stock", manager), 201)
	mixedChargeSale := readID(expect(keyedCash(salesPath, paidForExchange, "exchange-mixed-charge-sale", manager), 201))
	receiptMixedCash := readID(expect(keyedCash(returnPath(mixedChargeSale), returnBody("Cambio mixto con efectivo", sellableLine),
		"exchange-mixed-charge-receipt", manager), 201))
	receiptMixedElectronic := readID(expect(keyedCash(returnPath(mixedChargeSale), returnBody("Cambio mixto electrónico", sellableLine),
		"exchange-electronic-charge-receipt", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Stock para cambio de diferencia mixta"), "exchange-mixed-charge-stock", manager), 201)
	splitCharge := func(session, payments string) string {
		return strings.TrimSuffix(chargePayload, "}") + fmt.Sprintf(`,"charge":{"cashSessionId":"%s","payments":%s}}`, session, payments)
	}
	mixedChargePath := exchangePath(mixedChargeSale, receiptMixedCash)
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, `[]`), "exchange-empty-split", manager), 400, "invalid_request")
	duplicateCharge := `[{"method":"cash","amount":"7","cashReceived":"10"},{"method":"cash","amount":"13","cashReceived":"15"}]`
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, duplicateCharge), "exchange-duplicate-method", manager), 400, "invalid_request")
	bothFormats := strings.Replace(splitCharge(mixedChargeSession, `[{"method":"qr","amount":"20"}]`),
		`"payments":`, `"method":"cash","amount":"20","cashReceived":"20","payments":`, 1)
	checkRefundCode(keyedCash(mixedChargePath, bothFormats, "exchange-charge-both-formats", manager), 400, "invalid_request")
	wrongSplit := `[{"method":"cash","amount":"7.01","cashReceived":"8"},{"method":"qr","amount":"13"}]`
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, wrongSplit), "exchange-split-sum", manager), 409, "return_exchange_charge_amount")
	shortCash := `[{"method":"cash","amount":"7","cashReceived":"6.99"},{"method":"qr","amount":"13"}]`
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, shortCash), "exchange-mixed-short-tender", manager), 409, "sale_insufficient_cash")
	validSplit := `[{"method":"qr","amount":"13"},{"method":"cash","amount":"7","cashReceived":"10"}]`
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(managerCollect, validSplit), "exchange-mixed-cash-limit", manager), 409, "cash_balance_conflict")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_exchanges WHERE sale_id=$1", mixedChargeSale).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un cobro mixto rechazado dejó un cambio registrado: %d %v", auditCount, err)
	}
	mixedCharge := expect(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, validSplit), "exchange-confirm-mixed-charge", manager), 201)
	mixedChargeID := readID(mixedCharge)
	if !strings.Contains(mixedCharge.Body.String(), `"method":null,"amount":"20.00","payments":[{"method":"cash","amount":"7.00","cashReceived":"10.00","change":"3.00"},{"method":"qr","amount":"13.00"}]`) {
		t.Fatalf("desglose del cambio mixto incorrecto: %s", mixedCharge.Body)
	}
	mixedElectronics := `[{"method":"transfer","amount":"8.25"},{"method":"debit","amount":"11.75"}]`
	electronicMixedCharge := expect(keyedCash(exchangePath(mixedChargeSale, receiptMixedElectronic), splitCharge(mixedChargeSession, mixedElectronics),
		"exchange-confirm-electronics", manager), 201)
	if !strings.Contains(electronicMixedCharge.Body.String(), `"payments":[{"method":"debit","amount":"11.75"},{"method":"transfer","amount":"8.25"}]`) {
		t.Fatalf("desglose electrónico incorrecto: %s", electronicMixedCharge.Body)
	}
	if got := request("GET", returnPath(mixedChargeSale)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, mixedCharge.Body.String()) ||
		!strings.Contains(got, electronicMixedCharge.Body.String()) {
		t.Fatalf("el historial perdió el desglose del cambio: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE cash_session_id=$1 AND exchange_id=$2 AND amount=7",
		mixedChargeSession, mixedChargeID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el cambio mixto debe registrar un solo ingreso neto en efectivo: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE exchange_id=$1",
		readID(electronicMixedCharge)).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("el cambio con dos medios electrónicos no debe alterar el efectivo: %d %v", auditCount, err)
	}
	closedSplitCharge := request("POST", sessionsPath+"/"+mixedChargeSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"17"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(closedSplitCharge, `"expectedCash":"17.00"`) || !strings.Contains(closedSplitCharge, `"difference":"0.00"`) {
		t.Fatalf("arqueo con cambio mixto incorrecto: %s", closedSplitCharge)
	}
	reorderedCharge := `[{"method":"cash","amount":"7.0","cashReceived":"10.00"},{"method":"qr","amount":"13.00"}]`
	if replay := expect(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, reorderedCharge), "exchange-confirm-mixed-charge", manager), 201); replay.Body.String() != mixedCharge.Body.String() {
		t.Fatal("el reintento desordenado tras el cierre debe recuperar el mismo cambio")
	}
	changedCharge := `[{"method":"cash","amount":"8","cashReceived":"10"},{"method":"qr","amount":"12"}]`
	checkRefundCode(keyedCash(mixedChargePath, splitCharge(mixedChargeSession, changedCharge), "exchange-confirm-mixed-charge", manager),
		409, "idempotency_conflict")

	lowerRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-CAMBIO-MENOR-VALOR"), origin, cookie, 201))
	lowerSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, lowerRegister, `"50"`), "exchange-lower-open", manager), 201))
	emptyExchangeRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-CAMBIO-SIN-EFECTIVO"), origin, cookie, 201))
	emptyExchangeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, emptyExchangeRegister, `"0"`),
		"exchange-lower-empty-register", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock de venta para cambio de menor valor"), "exchange-lower-sale-stock", manager), 201)
	lowerSaleBody := strings.Replace(payBody(lowerSession, `[{"method":"cash","amount":"40","cashReceived":"40"},{"method":"qr","amount":"120"}]`),
		`"expectedUnitPrice":"30"`, `"expectedUnitPrice":"80"`, 1)
	lowerSaleBody = strings.Replace(lowerSaleBody, `"quantity":1`, `"quantity":2`, 1)
	lowerSale := expect(keyedCash(salesPath, lowerSaleBody, "exchange-lower-sale", manager), 201)
	lowerSaleID := readID(lowerSale)
	lowerReceiptFirst := readID(expect(keyedCash(returnPath(lowerSaleID), returnBody("Primera recepción de menor valor", sellableLine),
		"exchange-lower-receipt-first", manager), 201))
	lowerReceiptSecond := readID(expect(keyedCash(returnPath(lowerSaleID), returnBody("Segunda recepción de menor valor", sellableLine),
		"exchange-lower-receipt-second", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Prendas de reemplazo de menor importe"),
		"exchange-lower-replacement-stock", manager), 201)
	request("PUT", itemPath(general, spare), `{"amount":"55.00"}`, origin, cookie, 200)
	lowerExchangePayload := func(session, expectedPrice, payments string) string {
		base := exchangeInput(sellable, spare, expectedPrice, 1)
		return strings.TrimSuffix(base, "}") + fmt.Sprintf(`,"refund":{"cashSessionId":"%s","payments":%s}}`, session, payments)
	}
	lowerFirstPath := exchangePath(lowerSaleID, lowerReceiptFirst)
	checkRefundCode(keyedCash(lowerFirstPath, exchangeInput(sellable, spare, "55", 1), "exchange-lower-missing-refund", manager),
		409, "return_exchange_difference")
	checkRefundCode(keyedCash(lowerFirstPath, lowerExchangePayload(chargeSession, "55", `[{"method":"qr","amount":"25"}]`),
		"exchange-lower-closed-register", manager), 409, "cash_session_state_conflict")
	checkRefundCode(keyedCash(lowerFirstPath, lowerExchangePayload(lowerSession, "55", `[{"method":"debit","amount":"25"}]`),
		"exchange-lower-unpaid-method", manager), 409, "return_refund_method_mismatch")
	checkRefundCode(keyedCash(lowerFirstPath, lowerExchangePayload(lowerSession, "55", `[{"method":"qr","amount":"25.01"}]`),
		"exchange-lower-refund-amount", manager), 409, "return_refund_allocation")
	lowerFirstSplit := `[{"method":"qr","amount":"15"},{"method":"cash","amount":"10"}]`
	var stockBeforeLower int
	if err := db.QueryRow(ctx, "SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, sellable, spare).Scan(&stockBeforeLower); err != nil {
		t.Fatal(err)
	}
	checkRefundCode(keyedCash(lowerFirstPath, lowerExchangePayload(emptyExchangeSession, "55", lowerFirstSplit), "exchange-lower-without-cash", manager),
		409, "cash_balance_conflict")
	var stockAfterLowerFailure int
	if err := db.QueryRow(ctx, "SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
		created.TenantID, sellable, spare).Scan(&stockAfterLowerFailure); err != nil || stockAfterLowerFailure != stockBeforeLower {
		t.Fatalf("la falta de efectivo debe revertir la salida de stock: %d %v", stockAfterLowerFailure, err)
	}
	if got := request("GET", returnPath(lowerSaleID)+cashQuery, "", "", manager, 200).Body.String(); strings.Contains(got, `"exchange":`) ||
		strings.Contains(got, `"refund":`) {
		t.Fatalf("un egreso fallido dejó parte del cambio: %s", got)
	}
	lowerExchangeFirst := expect(keyedCash(lowerFirstPath, lowerExchangePayload(lowerSession, "55", lowerFirstSplit), "exchange-lower-first", manager), 201)
	if !strings.Contains(lowerExchangeFirst.Body.String(), `"total":"55.00"`) || strings.Contains(lowerExchangeFirst.Body.String(), `"charge":`) {
		t.Fatalf("el registro del reemplazo debe conservar su precio: %s", lowerExchangeFirst.Body)
	}
	if got := request("GET", returnPath(lowerSaleID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got,
		`"refundOptions":[{"method":"cash","amount":"30.00"},{"method":"qr","amount":"105.00"}]`) ||
		!strings.Contains(got, `"payments":[{"method":"cash","amount":"10.00"},{"method":"qr","amount":"15.00"}]`) {
		t.Fatalf("el reintegro de la diferencia debe descontarse de cada medio original: %s", got)
	}
	checkRefundCode(keyedCash(refundPath(lowerSaleID, lowerReceiptFirst), electronicRefund(lowerSession, "qr"),
		"exchange-lower-after-refund", manager), 409, "return_exchange_unavailable")

	request("PUT", itemPath(general, spare), `{"amount":"30.00"}`, origin, cookie, 200)
	lowerSecondPath := exchangePath(lowerSaleID, lowerReceiptSecond)
	checkRefundCode(keyedCash(lowerSecondPath, lowerExchangePayload(lowerSession, "30", `[{"method":"cash","amount":"31"},{"method":"qr","amount":"19"}]`),
		"exchange-lower-cumulative-limit", manager), 409, "return_refund_allocation")
	expect(keyedCash(lowerSecondPath, lowerExchangePayload(lowerSession, "30", `[{"method":"cash","amount":"30"},{"method":"qr","amount":"20"}]`),
		"exchange-lower-second", manager), 201)
	lowerClosed := request("POST", sessionsPath+"/"+lowerSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"50"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(lowerClosed, `"expectedCash":"50.00"`) || !strings.Contains(lowerClosed, `"difference":"0.00"`) {
		t.Fatalf("el arqueo debe descontar solo el efectivo reintegrado: %s", lowerClosed)
	}
	reorderedLowerSplit := `[{"method":"cash","amount":"10.0"},{"method":"qr","amount":"15.00"}]`
	if replay := expect(keyedCash(lowerFirstPath, lowerExchangePayload(lowerSession, "55.00", reorderedLowerSplit), "exchange-lower-first", manager), 201); replay.Body.String() != lowerExchangeFirst.Body.String() {
		t.Fatal("reintentar un cambio de menor valor tras el cierre debe recuperar el cambio sin repetir el reintegro")
	}
	if got := request("GET", salesPath+"/"+lowerSaleID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, lowerSale.Body.String()) {
		t.Fatalf("el cambio de menor importe modificó el ticket histórico: %s", got)
	}

	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock para cambio de venta fiada"), "fiado-exchange-original-stock", manager), 201)
	creditExchangeBody := strings.Replace(fiadoSaleBody(`[{"method":"qr","amount":"30"}]`), fiadoRefundSession, managerCollect, 1)
	originalCreditExchange := expect(keyedCash(salesPath, creditExchangeBody, "fiado-exchange-sale", manager), 201)
	creditExchangeSale := readID(originalCreditExchange)
	var creditExchangeDebt string
	if err := db.QueryRow(ctx, "SELECT id FROM receivables WHERE tenant_id=$1 AND sale_id=$2", created.TenantID, creditExchangeSale).Scan(&creditExchangeDebt); err != nil {
		t.Fatal(err)
	}
	creditExchangeDebtPath := receivablesPath + "/" + creditExchangeDebt + "/payments"
	expect(keyedCash(creditExchangeDebtPath, collectionBody(managerCollect, "transfer", "5"), "fiado-exchange-installment", manager), 201)
	creditExchangeFirst := readID(expect(keyedCash(returnPath(creditExchangeSale), returnBody("Cambio de talle fiado", nonSellableLine),
		"fiado-exchange-receipt-first", manager), 201))
	creditExchangeSecond := readID(expect(keyedCash(returnPath(creditExchangeSale), returnBody("Segundo cambio fiado", sellableLine),
		"fiado-exchange-receipt-second", manager), 201))
	creditExchangeFirstPath := exchangePath(creditExchangeSale, creditExchangeFirst)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Reemplazos para venta fiada"), "fiado-exchange-stock", manager), 201)
	checkRefundCode(keyedCash(creditExchangeFirstPath, exchangeInput(sellable, spare, "30", 1), "fiado-exchange-lower", manager),
		409, "return_exchange_difference")
	request("PUT", itemPath(general, spare), `{"amount":"81.00"}`, origin, cookie, 200)
	checkRefundCode(keyedCash(creditExchangeFirstPath, exchangeInput(sellable, spare, "81", 1), "fiado-exchange-higher-without-charge", manager),
		409, "return_exchange_difference")
	request("PUT", itemPath(general, spare), `{"amount":"80.00"}`, origin, cookie, 200)
	debtBeforeExchange := request("GET", creditExchangeDebtPath+cashQuery, "", "", manager, 200).Body.String()
	var cashBeforeFiadoExchange int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&cashBeforeFiadoExchange); err != nil {
		t.Fatal(err)
	}
	savedCreditExchange := expect(keyedCash(creditExchangeFirstPath, matchingExchange, "fiado-exchange-confirmed", manager), 201)
	savedCreditExchangeID := readID(savedCreditExchange)
	if got := request("GET", creditExchangeDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != debtBeforeExchange ||
		!strings.Contains(got, `"outstandingAmount":"125.00"`) {
		t.Fatalf("el cambio de igual importe modificó el historial o saldo del fiado: %s", got)
	}
	if history := request("GET", returnPath(creditExchangeSale)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history,
		`"exchange":{"id":"`+savedCreditExchangeID+`"`) || !strings.Contains(history, `"outstandingDebt":"125.00"`) {
		t.Fatalf("la devolución fiada debe mostrar el cambio y el saldo vigente: %s", history)
	}
	checkRefundCode(keyedCash(debtCreditPath(creditExchangeSale, creditExchangeFirst), debtCreditBody("1"), "fiado-exchange-no-compensation", manager),
		409, "return_exchange_unavailable")
	checkRefundCode(keyedCash(refundPath(creditExchangeSale, creditExchangeFirst), mixedRefundBody(managerCollect, `[{"method":"qr","amount":"30"}]`),
		"fiado-exchange-no-refund", manager), 409, "return_exchange_unavailable")
	if replay := expect(keyedCash(creditExchangeFirstPath, matchingExchange, "fiado-exchange-confirmed", manager), 201); replay.Body.String() != savedCreditExchange.Body.String() {
		t.Fatal("el reintento debe conservar el cambio de la venta fiada")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE exchange_id=$1 AND quantity_delta=-1", savedCreditExchangeID).
		Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el reintento modificó el stock de reemplazo: %d %v", auditCount, err)
	}
	go func() {
		raceSales <- keyedCash(exchangePath(creditExchangeSale, creditExchangeSecond), matchingExchange, "fiado-exchange-race", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(debtCreditPath(creditExchangeSale, creditExchangeSecond), debtCreditBody("10"), "fiado-exchange-compensation-race", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("el cambio y la compensación simultáneos no pueden resolver la misma recepción: %d y %d", aSale, bSale)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != cashBeforeFiadoExchange {
		t.Fatalf("el cambio de fiado de igual importe alteró la caja: %d %v", auditCount, err)
	}
	if ticket := request("GET", salesPath+"/"+creditExchangeSale+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(ticket, originalCreditExchange.Body.String()) {
		t.Fatalf("el cambio de fiado modificó el ticket original: %s", ticket)
	}

	fiadoChargeRegister := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-DIFERENCIA-FIADO"), origin, cookie, 201))
	fiadoChargeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoChargeRegister, `"20"`),
		"fiado-exchange-charge-open", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock de venta fiada con diferencia de cambio"),
		"fiado-exchange-charge-origin-stock", manager), 201)
	chargedCreditSale := expect(keyedCash(salesPath, creditExchangeBody, "fiado-exchange-charge-sale", manager), 201)
	chargedCreditSaleID := readID(chargedCreditSale)
	var chargedCreditDebtID string
	if err := db.QueryRow(ctx, "SELECT id FROM receivables WHERE tenant_id=$1 AND sale_id=$2", created.TenantID, chargedCreditSaleID).Scan(&chargedCreditDebtID); err != nil {
		t.Fatal(err)
	}
	chargedCreditDebtPath := receivablesPath + "/" + chargedCreditDebtID + "/payments"
	expect(keyedCash(chargedCreditDebtPath, collectionBody(managerCollect, "transfer", "5"), "fiado-exchange-charge-abono", manager), 201)
	chargedCreditReceipt := readID(expect(keyedCash(returnPath(chargedCreditSaleID), returnBody("Cambio de fiado de mayor valor", sellableLine),
		"fiado-exchange-charge-receipt", manager), 201))
	chargedCreditPath := exchangePath(chargedCreditSaleID, chargedCreditReceipt)
	beforeCreditCharge := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String()
	request("PUT", itemPath(general, spare), `{"amount":"95.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 1, "Reemplazo para cambio fiado con diferencia"),
		"fiado-exchange-charge-replacement-stock", manager), 201)
	creditExchangeDifference := func(sessionID, payments string) string {
		return strings.TrimSuffix(exchangeInput(sellable, spare, "95", 1), "}") +
			fmt.Sprintf(`,"charge":{"cashSessionId":"%s","payments":%s}}`, sessionID, payments)
	}
	validCreditExchangeCharge := `[{"method":"qr","amount":"10"},{"method":"cash","amount":"5","cashReceived":"10"}]`
	checkRefundCode(keyedCash(chargedCreditPath, exchangeInput(sellable, spare, "95", 1), "fiado-charge-required", manager),
		409, "return_exchange_difference")
	checkRefundCode(keyedCash(chargedCreditPath, creditExchangeDifference(fiadoChargeSession, `[{"method":"qr","amount":"14.99"}]`),
		"fiado-charge-wrong-amount", manager), 409, "return_exchange_charge_amount")
	checkRefundCode(keyedCash(chargedCreditPath, creditExchangeDifference(chargeSession, validCreditExchangeCharge), "fiado-charge-closed-register", manager),
		409, "cash_session_state_conflict")
	checkRefundCode(keyedCash(chargedCreditPath, creditExchangeDifference(managerCollect, validCreditExchangeCharge), "fiado-charge-cash-limit", manager),
		409, "cash_balance_conflict")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_exchanges WHERE sale_id=$1", chargedCreditSaleID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un cobro no confirmado modificó el historial del cambio fiado: %d %v", auditCount, err)
	}
	chargedCreditInput := creditExchangeDifference(fiadoChargeSession, validCreditExchangeCharge)
	chargedCreditExchange := expect(keyedCash(chargedCreditPath, chargedCreditInput, "fiado-cambio-con-diferencia", manager), 201)
	chargedCreditExchangeID := readID(chargedCreditExchange)
	if !strings.Contains(chargedCreditExchange.Body.String(), `"total":"95.00"`) ||
		!strings.Contains(chargedCreditExchange.Body.String(), `"payments":[{"method":"cash","amount":"5.00","cashReceived":"10.00","change":"5.00"},{"method":"qr","amount":"10.00"}]`) {
		t.Fatalf("cambio fiado con diferencia mixta incorrecto: %s", chargedCreditExchange.Body)
	}
	if got := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != beforeCreditCharge ||
		!strings.Contains(got, `"outstandingAmount":"125.00"`) {
		t.Fatalf("la diferencia se registró como abono o cambió el saldo original: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE exchange_id=$1 AND cash_session_id=$2 AND amount=5",
		chargedCreditExchangeID, fiadoChargeSession).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el arqueo debe registrar solo la diferencia neta en efectivo: %d %v", auditCount, err)
	}
	fiadoChargeClose := request("POST", sessionsPath+"/"+fiadoChargeSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"25"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(fiadoChargeClose, `"expectedCash":"25.00"`) || !strings.Contains(fiadoChargeClose, `"difference":"0.00"`) {
		t.Fatalf("el arqueo del cambio fiado no coincide con el efectivo neto: %s", fiadoChargeClose)
	}
	if replay := expect(keyedCash(chargedCreditPath, chargedCreditInput, "fiado-cambio-con-diferencia", manager), 201); replay.Body.String() != chargedCreditExchange.Body.String() {
		t.Fatal("reintentar después del cierre debe recuperar el mismo cobro")
	}
	if ticket := request("GET", salesPath+"/"+chargedCreditSaleID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(ticket, chargedCreditSale.Body.String()) {
		t.Fatalf("el cobro de la diferencia cambió el ticket del fiado: %s", ticket)
	}

	lowerCreditSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoChargeRegister, `"20"`),
		"fiado-lower-open", manager), 201))
	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock de venta fiada para cambios de menor valor"),
		"fiado-lower-original-stock", manager), 201)
	lowerCreditSale := expect(keyedCash(salesPath, creditExchangeBody, "fiado-lower-sale", manager), 201)
	lowerCreditSaleID := readID(lowerCreditSale)
	var lowerCreditDebtID string
	if err := db.QueryRow(ctx, "SELECT id FROM receivables WHERE sale_id=$1", lowerCreditSaleID).Scan(&lowerCreditDebtID); err != nil {
		t.Fatal(err)
	}
	lowerCreditDebtPath := receivablesPath + "/" + lowerCreditDebtID + "/payments"
	expect(keyedCash(lowerCreditDebtPath, collectionBody(lowerCreditSession, "cash", "10"), "fiado-lower-cash-installment", manager), 201)
	lowerCreditFirstReceipt := readID(expect(keyedCash(returnPath(lowerCreditSaleID), returnBody("Cambio fiado compensado", sellableLine),
		"fiado-lower-first-receipt", manager), 201))
	lowerCreditSecondReceipt := readID(expect(keyedCash(returnPath(lowerCreditSaleID), returnBody("Cambio fiado con reintegro", sellableLine),
		"fiado-lower-second-receipt", manager), 201))
	lowerCreditFirstPath := exchangePath(lowerCreditSaleID, lowerCreditFirstReceipt)
	lowerCreditSecondPath := exchangePath(lowerCreditSaleID, lowerCreditSecondReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"30.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Reemplazos para cambios fiados de menor valor"),
		"fiado-lower-replacement-stock", manager), 201)
	lowerCreditPayload := func(amount, session, payments string) string {
		base := strings.TrimSuffix(exchangeInput(sellable, spare, "30", 1), "}")
		if amount != "" {
			base += fmt.Sprintf(`,"debtCreditAmount":"%s"`, amount)
		}
		if session != "" {
			base += fmt.Sprintf(`,"refund":{"cashSessionId":"%s","payments":%s}`, session, payments)
		}
		return base + "}"
	}
	checkRefundCode(keyedCash(lowerCreditFirstPath, lowerCreditPayload("51", "", ""), "fiado-lower-over-difference", manager),
		409, "return_debt_credit_amount")
	checkRefundCode(keyedCash(lowerCreditFirstPath, lowerCreditPayload("49", "", ""), "fiado-lower-missing-refund", manager),
		409, "return_exchange_difference")
	lowerCreditFirst := expect(keyedCash(lowerCreditFirstPath, lowerCreditPayload("50", "", ""), "fiado-lower-credit-only", manager), 201)
	if replay := expect(keyedCash(lowerCreditFirstPath, lowerCreditPayload("50.00", "", ""), "fiado-lower-credit-only", manager), 201); replay.Body.String() != lowerCreditFirst.Body.String() {
		t.Fatal("el reintento sin caja debe recuperar el cambio con la compensación original")
	}
	mixedCreditRefund := `[{"method":"cash","amount":"4"},{"method":"qr","amount":"6"}]`
	beforeLowerCreditSplit := request("GET", lowerCreditDebtPath+cashQuery, "", "", manager, 200).Body.String()
	if !strings.Contains(beforeLowerCreditSplit, `"outstandingAmount":"70.00"`) || !strings.Contains(beforeLowerCreditSplit, `"amount":"50.00","balanceAfter":"70.00"`) {
		t.Fatalf("la compensación sin caja debe reducir el saldo una sola vez: %s", beforeLowerCreditSplit)
	}
	checkRefundCode(keyedCash(lowerCreditSecondPath, lowerCreditPayload("40", lowerCreditSession, `[{"method":"qr","amount":"9"}]`),
		"fiado-lower-wrong-refund-sum", manager), 409, "return_refund_allocation")
	checkRefundCode(keyedCash(lowerCreditSecondPath, lowerCreditPayload("40", emptyExchangeSession, mixedCreditRefund),
		"fiado-lower-insufficient-cash", manager), 409, "cash_balance_conflict")
	if got := request("GET", lowerCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != beforeLowerCreditSplit {
		t.Fatalf("un egreso fallido cambió el saldo o sus movimientos: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_exchanges WHERE return_id=$1", lowerCreditSecondReceipt).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un egreso fallido registró un cambio: %d %v", auditCount, err)
	}
	lowerCreditSplit := expect(keyedCash(lowerCreditSecondPath, lowerCreditPayload("40", lowerCreditSession, mixedCreditRefund),
		"fiado-lower-mixed-resolution", manager), 201)
	if history := request("GET", returnPath(lowerCreditSaleID)+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history, `"outstandingDebt":"30.00"`) ||
		!strings.Contains(history, `"amount":"40.00","balanceAfter":"30.00"`) ||
		!strings.Contains(history, `"payments":[{"method":"cash","amount":"4.00"},{"method":"qr","amount":"6.00"}]`) ||
		!strings.Contains(history, `"refundOptions":[{"method":"cash","amount":"6.00"},{"method":"qr","amount":"24.00"}]`) {
		t.Fatalf("la compensación y el reintegro mixto deben quedar en el historial de la recepción: %s", history)
	}
	checkRefundCode(keyedCash(debtCreditPath(lowerCreditSaleID, lowerCreditSecondReceipt), debtCreditBody("1"), "fiado-lower-already-resolved", manager),
		409, "return_exchange_unavailable")
	lowerCreditClose := request("POST", sessionsPath+"/"+lowerCreditSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"26"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(lowerCreditClose, `"expectedCash":"26.00"`) || !strings.Contains(lowerCreditClose, `"difference":"0.00"`) {
		t.Fatalf("solo el reintegro efectivo debe reducir el arqueo: %s", lowerCreditClose)
	}
	if replay := expect(keyedCash(lowerCreditSecondPath, lowerCreditPayload("40.0", lowerCreditSession,
		`[{"method":"qr","amount":"6.00"},{"method":"cash","amount":"4.00"}]`), "fiado-lower-mixed-resolution", manager), 201); replay.Body.String() != lowerCreditSplit.Body.String() {
		t.Fatal("reintentar el cambio después del cierre debe recuperar la operación completa")
	}
	if ticket := request("GET", salesPath+"/"+lowerCreditSaleID+"/ticket"+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(ticket, lowerCreditSale.Body.String()) {
		t.Fatalf("el cambio de menor valor modificó el ticket del fiado: %s", ticket)
	}

	replacementReturnBody := func(exchangeID, variant, location, disposition string, quantity int) string {
		return fmt.Sprintf(`{"storeId":"%s","sourceExchangeId":"%s","reason":"Recepción de reemplazo","items":[
			{"variantId":"%s","locationId":"%s","disposition":"%s","quantity":%d}]}`,
			created.StoreID, exchangeID, variant, location, disposition, quantity)
	}
	replacementReturnPath := returnPath(chargedCreditSaleID)
	replacementInput := replacementReturnBody(chargedCreditExchangeID, spare, quarantineID, "quarantine", 1)
	expect(keyedCash(returnPath(lowerCreditSaleID), replacementInput, "replacement-return-wrong-sale", manager), 404)
	expect(keyedCash(replacementReturnPath, replacementInput, "replacement-return-wrong-tenant", second), 404)
	expect(keyedCash(replacementReturnPath, replacementInput, "replacement-return-wrong-role", operator), 403)
	checkRefundCode(keyedCash(replacementReturnPath, replacementReturnBody(chargedCreditExchangeID, variantID, sellable, "sellable", 1),
		"replacement-return-original-variant", manager), 409, "return_quantity_exceeded")
	checkRefundCode(keyedCash(replacementReturnPath, replacementReturnBody(chargedCreditExchangeID, spare, quarantineID, "sellable", 1),
		"replacement-return-wrong-location", manager), 409, "return_location_unavailable")
	creditBeforeReplacementReturn := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String()
	var cashBeforeReplacementReturn int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&cashBeforeReplacementReturn); err != nil {
		t.Fatal(err)
	}
	replacementReturned := expect(keyedCash(replacementReturnPath, replacementInput, "replacement-return-receipt", manager), 201)
	replacementReceipt := readID(replacementReturned)
	if body := replacementReturned.Body.String(); !strings.Contains(body, `"sourceExchangeId":"`+chargedCreditExchangeID+`"`) ||
		!strings.Contains(body, `"referenceAmount":"95.00"`) || !strings.Contains(body, `"disposition":"quarantine"`) {
		t.Fatalf("la recepción debe usar el cambio y su precio histórico: %s", body)
	}
	if replay := expect(keyedCash(replacementReturnPath, replacementInput, "replacement-return-receipt", manager), 201); replay.Body.String() != replacementReturned.Body.String() {
		t.Fatal("el reintento debe recuperar la recepción original")
	}
	checkRefundCode(keyedCash(replacementReturnPath, replacementInput, "replacement-return-over-quantity", manager), 409, "return_quantity_exceeded")
	if history := request("GET", replacementReturnPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history,
		`"returnedQuantities":{"`+spare+`":1}`) || !strings.Contains(history, `"sold":2,"returned":1`) {
		t.Fatalf("el historial debe separar cantidades de la venta y del cambio: %s", history)
	}
	if replay := expect(keyedCash(chargedCreditPath, chargedCreditInput, "fiado-cambio-con-diferencia", manager), 201); replay.Body.String() != chargedCreditExchange.Body.String() {
		t.Fatal("la nueva recepción modificó la respuesta original del cambio")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE return_id=$1 AND sale_id=$2 AND variant_id=$3 AND location_id=$4 AND quantity_delta=1`,
		replacementReceipt, chargedCreditSaleID, spare, quarantineID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("la recepción debe ingresar una sola unidad al destino elegido: %d %v", auditCount, err)
	}
	checkRefundCode(keyedCash(refundPath(chargedCreditSaleID, replacementReceipt), mixedRefundBody(managerCollect, `[{"method":"qr","amount":"30"}]`),
		"replacement-return-refund-insufficient", manager), 409, "return_refund_allocation")

	secondExchangePath := exchangePath(chargedCreditSaleID, replacementReceipt)
	cheaperReplacement := exchangeInput(sellable, spare, "30", 1)
	checkRefundCode(keyedCash(secondExchangePath, cheaperReplacement, "replacement-exchange-cheaper", manager),
		409, "return_exchange_difference")
	cheaperWithCredit := strings.TrimSuffix(cheaperReplacement, "}") + `,"debtCreditAmount":"66"}`
	checkRefundCode(keyedCash(secondExchangePath, cheaperWithCredit, "replacement-exchange-with-credit", manager),
		409, "return_debt_credit_amount")
	cheaperWithRefund := strings.TrimSuffix(cheaperReplacement, "}") +
		fmt.Sprintf(`,"debtCreditAmount":"60","refund":{"cashSessionId":"%s","payments":[{"method":"qr","amount":"4"}]}}`, managerCollect)
	checkRefundCode(keyedCash(secondExchangePath, cheaperWithRefund, "replacement-exchange-with-refund", manager),
		409, "return_refund_allocation")
	request("PUT", itemPath(general, spare), `{"amount":"100.00"}`, origin, cookie, 200)
	moreExpensive := exchangeInput(sellable, spare, "100", 1)
	checkRefundCode(keyedCash(secondExchangePath, moreExpensive, "replacement-exchange-charge-required", manager),
		409, "return_exchange_difference")

	request("PUT", itemPath(general, spare), `{"amount":"95.00"}`, origin, cookie, 200)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 1, "Stock para segundo cambio"), "replacement-exchange-stock", manager), 201)
	sameValue := exchangeInput(sellable, spare, "95", 1)
	equalReplacement := expect(keyedCash(secondExchangePath, sameValue, "replacement-second-exchange", manager), 201)
	secondExchangeID := readID(equalReplacement)
	if got := equalReplacement.Body.String(); !strings.Contains(got, `"returnId":"`+replacementReceipt+`"`) ||
		!strings.Contains(got, `"total":"95.00"`) || strings.Contains(got, `"charge":`) {
		t.Fatalf("el segundo cambio debe resolver la recepción por su importe histórico: %s", got)
	}
	if replay := expect(keyedCash(secondExchangePath, sameValue, "replacement-second-exchange", manager), 201); replay.Body.String() != equalReplacement.Body.String() {
		t.Fatal("el reintento debe conservar el segundo cambio")
	}
	checkRefundCode(keyedCash(secondExchangePath, sameValue, "replacement-second-exchange-duplicate", manager),
		409, "return_exchange_unavailable")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE exchange_id=$1 AND quantity_delta=-1", secondExchangeID).
		Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el segundo cambio debe descontar una sola unidad: %d %v", auditCount, err)
	}
	secondReplacementReceipt := readID(expect(keyedCash(replacementReturnPath, replacementReturnBody(secondExchangeID, spare, sellable, "sellable", 1),
		"replacement-receipt-second-source", manager), 201))
	var replacementHistory sales.ReturnHistory
	reexchangeHistoryJSON := request("GET", replacementReturnPath+cashQuery, "", "", manager, 200).Body.Bytes()
	if err := json.Unmarshal(reexchangeHistoryJSON, &replacementHistory); err != nil {
		t.Fatal(err)
	}
	receipts := replacementHistory.Returns
	if len(receipts) < 2 || receipts[0].SourceExchangeID == nil || *receipts[0].SourceExchangeID != secondExchangeID ||
		receipts[1].ID != replacementReceipt || receipts[1].Exchange == nil || receipts[1].Exchange.ID != secondExchangeID {
		t.Fatalf("el historial debe conservar la cadena de recepciones y cambios: %s", reexchangeHistoryJSON)
	}
	if got := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != creditBeforeReplacementReturn {
		t.Fatalf("la recepción o el nuevo cambio modificaron el saldo o historial del fiado: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != cashBeforeReplacementReturn {
		t.Fatalf("la recepción o el nuevo cambio modificaron la caja: %d %v", auditCount, err)
	}

	reexchangeChargeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoChargeRegister, `"20"`),
		"replacement-exchange-open", manager), 201))
	reexchangeChargePath := exchangePath(chargedCreditSaleID, secondReplacementReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"110.00"}`, origin, cookie, 200)
	reexchangeChargeBody := func(session, payments string) string {
		return strings.TrimSuffix(exchangeInput(sellable, spare, "110", 1), "}") +
			fmt.Sprintf(`,"charge":{"cashSessionId":"%s","payments":%s}}`, session, payments)
	}
	checkRefundCode(keyedCash(reexchangeChargePath, reexchangeChargeBody(reexchangeChargeSession, `[{"method":"qr","amount":"14.99"}]`),
		"replacement-exchange-short-charge", manager), 409, "return_exchange_charge_amount")
	checkRefundCode(keyedCash(reexchangeChargePath, reexchangeChargeBody(fiadoChargeSession, validCreditExchangeCharge),
		"replacement-exchange-closed-cash", manager), 409, "cash_session_state_conflict")
	checkRefundCode(keyedCash(reexchangeChargePath, reexchangeChargeBody(managerCollect, validCreditExchangeCharge),
		"replacement-exchange-overflow", manager), 409, "cash_balance_conflict")
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_exchanges WHERE return_id=$1", secondReplacementReceipt).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("un cobro fallido resolvió una recepción de reemplazo: %d %v", auditCount, err)
	}
	chargedReexchangeInput := reexchangeChargeBody(reexchangeChargeSession, validCreditExchangeCharge)
	chargedReexchange := expect(keyedCash(reexchangeChargePath, chargedReexchangeInput, "replacement-exchange-charge", manager), 201)
	chargedReexchangeID := readID(chargedReexchange)
	if history := request("GET", replacementReturnPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(history, chargedReexchange.Body.String()) ||
		!strings.Contains(chargedReexchange.Body.String(), `"amount":"15.00","payments":[{"method":"cash","amount":"5.00","cashReceived":"10.00","change":"5.00"},{"method":"qr","amount":"10.00"}]`) {
		t.Fatalf("el historial debe conservar el cobro mixto del nuevo cambio: %s", history)
	}
	if got := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != creditBeforeReplacementReturn {
		t.Fatalf("la diferencia del nuevo cambio modificó el saldo o el historial del fiado: %s", got)
	}
	cashSummary := request("POST", sessionsPath+"/"+reexchangeChargeSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"25"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(cashSummary, `"expectedCash":"25.00"`) || !strings.Contains(cashSummary, `"difference":"0.00"`) {
		t.Fatalf("el arqueo debe incluir solo el ingreso neto de la diferencia: %s", cashSummary)
	}
	if replay := expect(keyedCash(reexchangeChargePath, chargedReexchangeInput, "replacement-exchange-charge", manager), 201); replay.Body.String() != chargedReexchange.Body.String() {
		t.Fatal("el reintento tras cerrar la caja debe recuperar el nuevo cambio y su cobro")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE exchange_id=$1 AND cash_session_id=$2 AND amount=5",
		chargedReexchangeID, reexchangeChargeSession).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el reintento debe conservar un solo ingreso: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE exchange_id=$1 AND quantity_delta=-1",
		chargedReexchangeID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("el reintento debe conservar una sola salida de reemplazo: %d %v", auditCount, err)
	}

	for _, parent := range []string{readID(lowerCreditFirst), readID(lowerCreditSplit)} {
		expect(keyedCash(returnPath(lowerCreditSaleID), replacementReturnBody(parent, spare, sellable, "sellable", 1),
			"replacement-independent-"+parent, manager), 201)
	}
	raceReplacementInput := replacementReturnBody(mixedChargeID, spare, sellable, "sellable", 1)
	go func() {
		raceSales <- keyedCash(returnPath(mixedChargeSale), raceReplacementInput, "replacement-race-one", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(returnPath(mixedChargeSale), raceReplacementInput, "replacement-race-two", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("dos recepciones simultáneas no pueden recibir la misma unidad: %d y %d", aSale, bSale)
	}

	var paidReplacementReceipt string
	if err := db.QueryRow(ctx, "SELECT id FROM sale_returns WHERE tenant_id=$1 AND source_exchange_id=$2",
		created.TenantID, mixedChargeID).Scan(&paidReplacementReceipt); err != nil {
		t.Fatal(err)
	}
	paidReplacementHistory := func() sales.ReturnHistory {
		t.Helper()
		response := request("GET", returnPath(mixedChargeSale)+cashQuery, "", "", manager, 200)
		var result sales.ReturnHistory
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	findReplacementOptions := func(history sales.ReturnHistory, receiptID string) []sales.Payment {
		t.Helper()
		for _, receipt := range history.Returns {
			if receipt.ID == receiptID {
				return receipt.ExchangeRefundOptions
			}
		}
		t.Fatalf("falta la recepción %s en el historial", receiptID)
		return nil
	}
	if got := findReplacementOptions(paidReplacementHistory(), paidReplacementReceipt); fmt.Sprint(got) != "[{cash 7.00} {qr 173.00}]" {
		t.Fatalf("solo deben estar disponibles los cobros de la venta y de la cadena: %v", got)
	}
	paidReexchangeSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, mixedChargeRegister, `"10"`),
		"replacement-refund-open", manager), 201))
	paidReexchangePath := exchangePath(mixedChargeSale, paidReplacementReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"75.00"}`, origin, cookie, 200)
	refundReexchange := func(session, price, payments string) string {
		return strings.TrimSuffix(exchangeInput(sellable, spare, price, 1), "}") +
			fmt.Sprintf(`,"refund":{"cashSessionId":"%s","payments":%s}}`, session, payments)
	}
	checkRefundCode(keyedCash(paidReexchangePath, refundReexchange(paidReexchangeSession, "75", `[{"method":"debit","amount":"1"},{"method":"qr","amount":"24"}]`),
		"replacement-refund-other-chain", manager), 409, "return_refund_method_mismatch")
	checkRefundCode(keyedCash(paidReexchangePath, refundReexchange(paidReexchangeSession, "75", `[{"method":"cash","amount":"8"},{"method":"qr","amount":"17"}]`),
		"replacement-refund-over-method", manager), 409, "return_refund_allocation")
	reexchangeRefundParts := `[{"method":"cash","amount":"7"},{"method":"qr","amount":"18"}]`
	checkRefundCode(keyedCash(paidReexchangePath, refundReexchange(emptyExchangeSession, "75", reexchangeRefundParts),
		"replacement-refund-empty-cash", manager), 409, "cash_balance_conflict")
	if got := findReplacementOptions(paidReplacementHistory(), paidReplacementReceipt); fmt.Sprint(got) != "[{cash 7.00} {qr 173.00}]" {
		t.Fatalf("un egreso fallido consumió saldo reintegrable: %v", got)
	}
	reexchangeRefundInput := refundReexchange(paidReexchangeSession, "75", reexchangeRefundParts)
	refundedReexchange := expect(keyedCash(paidReexchangePath, reexchangeRefundInput, "replacement-refund-complete", manager), 201)
	refundedReexchangeID := readID(refundedReexchange)
	if got := paidReplacementHistory(); fmt.Sprint(got.RefundOptions) != "[{qr 155.00}]" {
		t.Fatalf("solo el remanente del cobro QR tiene que salir de la venta original: %v", got.RefundOptions)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sale_return_refund_charge_allocations a
		JOIN sale_return_refunds f ON (f.tenant_id,f.id)=(a.tenant_id,a.refund_id)
		WHERE f.return_id=$1 AND a.exchange_id=$2 AND ((a.method='cash' AND a.amount=7) OR (a.method='qr' AND a.amount=13))`,
		paidReplacementReceipt, mixedChargeID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("deben persistirse dos importes atribuidos al cambio de origen: %d %v", auditCount, err)
	}
	replacementRefundClose := request("POST", sessionsPath+"/"+paidReexchangeSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"3"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(replacementRefundClose, `"expectedCash":"3.00"`) || !strings.Contains(replacementRefundClose, `"difference":"0.00"`) {
		t.Fatalf("el arqueo debe descontar solo el efectivo reintegrado: %s", replacementRefundClose)
	}
	if replay := expect(keyedCash(paidReexchangePath, reexchangeRefundInput, "replacement-refund-complete", manager), 201); replay.Body.String() != refundedReexchange.Body.String() {
		t.Fatal("el reintento después del cierre debe recuperar el cambio con reintegro")
	}
	nextPaidReceipt := readID(expect(keyedCash(returnPath(mixedChargeSale), replacementReturnBody(refundedReexchangeID, spare, sellable, "sellable", 1),
		"replacement-refund-next-receipt", manager), 201))
	if got := findReplacementOptions(paidReplacementHistory(), nextPaidReceipt); fmt.Sprint(got) != "[{qr 155.00}]" {
		t.Fatalf("el siguiente cambio no puede volver a usar los cobros reintegrados del ancestro: %v", got)
	}

	fullCreditReceipt := readID(expect(keyedCash(replacementReturnPath, replacementReturnBody(chargedReexchangeID, spare, sellable, "sellable", 1),
		"replacement-receipt-for-full-credit", manager), 201))
	fullCreditPath := exchangePath(chargedCreditSaleID, fullCreditReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"100.00"}`, origin, cookie, 200)
	fullCreditInput := strings.TrimSuffix(exchangeInput(sellable, spare, "100", 1), "}") + `,"debtCreditAmount":"10"}`
	var beforeFullCreditCash int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&beforeFullCreditCash); err != nil {
		t.Fatal(err)
	}
	fullCreditReexchange := expect(keyedCash(fullCreditPath, fullCreditInput, "replacement-exchange-full-credit", manager), 201)
	if replay := expect(keyedCash(fullCreditPath, fullCreditInput, "replacement-exchange-full-credit", manager), 201); replay.Body.String() != fullCreditReexchange.Body.String() {
		t.Fatal("el reintento debe conservar el cambio con compensación sin caja")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM cash_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != beforeFullCreditCash {
		t.Fatalf("la compensación completa no debe afectar la caja: %d %v", auditCount, err)
	}
	creditBeforePartial := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String()
	if !strings.Contains(creditBeforePartial, `"amount":"10.00","balanceAfter":"115.00"`) || !strings.Contains(creditBeforePartial, `"outstandingAmount":"115.00"`) {
		t.Fatalf("la primera compensación debe reducir el saldo en diez pesos: %s", creditBeforePartial)
	}
	partialCreditReceipt := readID(expect(keyedCash(replacementReturnPath, replacementReturnBody(readID(fullCreditReexchange), spare, sellable, "sellable", 1),
		"replacement-receipt-for-partial-credit", manager), 201))
	partialCreditPath := exchangePath(chargedCreditSaleID, partialCreditReceipt)
	request("PUT", itemPath(general, spare), `{"amount":"80.00"}`, origin, cookie, 200)
	partialCreditInput := func(session, payments string) string {
		return strings.TrimSuffix(refundReexchange(session, "80", payments), "}") + `,"debtCreditAmount":"11.00"}`
	}
	partialCreditSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoChargeRegister, `"10"`),
		"replacement-partial-credit-open", manager), 201))
	partialCreditHistory := request("GET", replacementReturnPath+cashQuery, "", "", manager, 200).Body.String()
	if !strings.Contains(partialCreditHistory, `"exchangeRefundOptions":[{"method":"cash","amount":"10.00"},{"method":"qr","amount":"50.00"},{"method":"transfer","amount":"5.00"}]`) {
		t.Fatalf("deben ofrecerse los cobros de la venta y de los cambios previos: %s", partialCreditHistory)
	}
	partialCreditPayments := `[{"method":"cash","amount":"6"},{"method":"qr","amount":"3"}]`
	checkRefundCode(keyedCash(partialCreditPath, partialCreditInput(emptyExchangeSession, partialCreditPayments),
		"replacement-partial-credit-cash-short", manager), 409, "cash_balance_conflict")
	if got := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); got != creditBeforePartial {
		t.Fatalf("el fallo de caja debe revertir la compensación de deuda: %s", got)
	}
	partialCreditBody := partialCreditInput(partialCreditSession, partialCreditPayments)
	partialCreditReexchange := expect(keyedCash(partialCreditPath, partialCreditBody, "replacement-partial-credit-confirm", manager), 201)
	if got := request("GET", replacementReturnPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, `"amount":"11.00","balanceAfter":"104.00"`) ||
		!strings.Contains(got, `"refundOptions":[{"method":"qr","amount":"30.00"},{"method":"transfer","amount":"5.00"}]`) ||
		!strings.Contains(got, `"payments":[{"method":"cash","amount":"6.00"},{"method":"qr","amount":"3.00"}]`) {
		t.Fatalf("deben registrarse la compensación y el reintegro, usando los cobros de la cadena: %s", got)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sale_return_refund_charge_allocations a
		JOIN sale_return_refunds f ON (f.tenant_id,f.id)=(a.tenant_id,a.refund_id)
		WHERE f.return_id=$1 AND ((a.exchange_id=$2 AND a.method='cash' AND a.amount=5) OR
			(a.exchange_id=$3 AND a.method='cash' AND a.amount=1) OR (a.exchange_id=$2 AND a.method='qr' AND a.amount=3))`,
		partialCreditReceipt, chargedReexchangeID, chargedCreditExchangeID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("el reintegro debe usar primero el cobro más cercano: %d %v", auditCount, err)
	}
	partialCreditClose := request("POST", sessionsPath+"/"+partialCreditSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"4"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(partialCreditClose, `"expectedCash":"4.00"`) || !strings.Contains(partialCreditClose, `"difference":"0.00"`) {
		t.Fatalf("la caja solo registra el dinero reintegrado en efectivo: %s", partialCreditClose)
	}
	if replay := expect(keyedCash(partialCreditPath, partialCreditBody, "replacement-partial-credit-confirm", manager), 201); replay.Body.String() != partialCreditReexchange.Body.String() {
		t.Fatal("el reintento tras el cierre debe conservar el cambio, el reintegro y la compensación")
	}

	directFiadoReceipt := readID(expect(keyedCash(replacementReturnPath,
		replacementReturnBody(readID(partialCreditReexchange), spare, quarantineID, "quarantine", 1), "replacement-direct-fiado-receipt", manager), 201))
	directFiadoCreditPath := debtCreditPath(chargedCreditSaleID, directFiadoReceipt)
	checkRefundCode(keyedCash(directFiadoCreditPath, debtCreditBody("81"), "replacement-direct-credit-over-reference", manager),
		409, "return_debt_credit_amount")
	directFiadoCredit := expect(keyedCash(directFiadoCreditPath, debtCreditBody("67"), "replacement-direct-credit", manager), 201)
	if !strings.Contains(directFiadoCredit.Body.String(), `"amount":"67.00","balanceAfter":"37.00"`) {
		t.Fatalf("la compensación debe descontar el importe elegido sin caja: %s", directFiadoCredit.Body)
	}
	if replay := expect(keyedCash(directFiadoCreditPath, debtCreditBody("67.00"), "replacement-direct-credit", manager), 201); replay.Body.String() != directFiadoCredit.Body.String() {
		t.Fatal("el reintento debe conservar la compensación directa")
	}
	checkRefundCode(keyedCash(exchangePath(chargedCreditSaleID, directFiadoReceipt), exchangeInput(sellable, spare, "80", 1),
		"replacement-direct-credit-blocks-exchange", manager), 409, "return_exchange_unavailable")

	directRefundSession := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, fiadoChargeRegister, `"10"`),
		"replacement-direct-refund-open", manager), 201))
	directFiadoRefundPath := refundPath(chargedCreditSaleID, directFiadoReceipt)
	checkRefundCode(keyedCash(directFiadoRefundPath, mixedRefundBody(directRefundSession, `[{"method":"cash","amount":"5"},{"method":"qr","amount":"8"}]`),
		"replacement-direct-refund-exceeds-chain", manager), 409, "return_refund_allocation")
	directFiadoRefundBody := mixedRefundBody(directRefundSession, `[{"method":"cash","amount":"4"},{"method":"qr","amount":"9"}]`)
	directFiadoRefund := expect(keyedCash(directFiadoRefundPath, directFiadoRefundBody, "replacement-direct-fiado-refund", manager), 201)
	if !strings.Contains(directFiadoRefund.Body.String(), `"amount":"13.00"`) {
		t.Fatalf("el reintegro debe cubrir el importe histórico menos la compensación: %s", directFiadoRefund.Body)
	}
	checkRefundCode(keyedCash(directFiadoCreditPath, debtCreditBody("1"), "replacement-direct-credit-after-refund", manager),
		409, "return_debt_credit_amount")
	if result := request("GET", chargedCreditDebtPath+cashQuery, "", "", manager, 200).Body.String(); !strings.Contains(result, `"outstandingAmount":"37.00"`) {
		t.Fatalf("el reintegro no debe modificar nuevamente el saldo de la cuenta: %s", result)
	}

	expect(adjust(adjustBody(created.StoreID, sellable, variantID, 2, "Stock para reintegro de reemplazo más caro"),
		"replacement-direct-paid-stock", manager), 201)
	expect(adjust(adjustBody(created.StoreID, sellable, spare, 2, "Stock para reemplazo de mayor valor"),
		"replacement-direct-paid-spare", manager), 201)
	directPaidSale := readID(expect(keyedCash(salesPath, paidForExchange, "replacement-direct-paid-sale", manager), 201))
	originalPaidReceipt := readID(expect(keyedCash(returnPath(directPaidSale),
		returnBody("Devolución de la venta", lineReturn(sellable, variantID, "sellable", 2)), "replacement-direct-paid-original-return", manager), 201))
	request("PUT", itemPath(general, spare), `{"amount":"100.00"}`, origin, cookie, 200)
	paidUpgradeBody := strings.TrimSuffix(exchangeInput(sellable, spare, "100", 2), "}") +
		fmt.Sprintf(`,"charge":{"cashSessionId":"%s","payments":[{"method":"cash","amount":"20","cashReceived":"20"},{"method":"qr","amount":"20"}]}}`, directRefundSession)
	paidUpgrade := readID(expect(keyedCash(exchangePath(directPaidSale, originalPaidReceipt), paidUpgradeBody, "replacement-direct-paid-charge", manager), 201))
	directPaidReceipt := readID(expect(keyedCash(returnPath(directPaidSale), replacementReturnBody(paidUpgrade, spare, sellable, "sellable", 2),
		"replacement-direct-paid-receipt", manager), 201))
	directPaidRefundPath := refundPath(directPaidSale, directPaidReceipt)
	directPaidRefundBody := mixedRefundBody(directRefundSession, `[{"method":"cash","amount":"20"},{"method":"qr","amount":"180"}]`)
	directPaidRefund := expect(keyedCash(directPaidRefundPath, directPaidRefundBody, "replacement-direct-paid-refund", manager), 201)
	if !strings.Contains(directPaidRefund.Body.String(), `"amount":"200.00"`) {
		t.Fatalf("el reemplazo puede reintegrarse por encima del total original con sus cobros históricos: %s", directPaidRefund.Body)
	}
	directRefundClose := request("POST", sessionsPath+"/"+directRefundSession+"/close",
		fmt.Sprintf(`{"storeId":"%s","declaredCash":"6"}`, created.StoreID), origin, manager, 200).Body.String()
	if !strings.Contains(directRefundClose, `"expectedCash":"6.00"`) || !strings.Contains(directRefundClose, `"difference":"0.00"`) {
		t.Fatalf("solo los movimientos en efectivo deben figurar en el arqueo: %s", directRefundClose)
	}
	if replay := expect(keyedCash(directPaidRefundPath, directPaidRefundBody, "replacement-direct-paid-refund", manager), 201); replay.Body.String() != directPaidRefund.Body.String() {
		t.Fatal("el reintento tras cerrar caja debe recuperar el reintegro directo")
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_return_refund_charge_allocations WHERE tenant_id=$1 AND refund_id=$2 AND exchange_id=$3 AND amount=20",
		created.TenantID, readID(directPaidRefund), paidUpgrade).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("el reintegro debe consumir una sola vez ambos cobros del cambio: %d %v", auditCount, err)
	}

	const suppliersPath = "/api/v1/suppliers"
	request("GET", suppliersPath, "", "", nil, 401)
	request("GET", suppliersPath, "", "", cashier, 403)
	request("GET", suppliersPath, "", "", operator, 403)
	request("GET", suppliersPath+"?limit=0", "", "", cookie, 400)
	request("GET", suppliersPath+"?offset=100001", "", "", cookie, 400)
	request("POST", suppliersPath, `{"name":" "}`, origin, cookie, 400)
	request("POST", suppliersPath, `{"name":"Proveedor","email":"correo inválido"}`, origin, cookie, 400)
	request("POST", suppliersPath, `{"name":"Proveedor","tenantId":"otro"}`, origin, cookie, 400)
	request("POST", suppliersPath, `{"name":"Proveedor","status":"inactive"}`, origin, cookie, 400)
	request("POST", suppliersPath, `{"name":"Proveedor"}`, origin, manager, 403)
	supplierResponse := request("POST", suppliersPath, `{"name":"  Textiles Norte  ","email":" PEDIDOS@NORTE.EXAMPLE ","phone":" 381 555 0100 "}`, origin, cookie, 201)
	supplierID := readID(supplierResponse)
	supplierPath := suppliersPath + "/" + supplierID
	if got := supplierResponse.Body.String(); !strings.Contains(got, `"name":"Textiles Norte","email":"pedidos@norte.example","phone":"381 555 0100","status":"active"`) {
		t.Fatalf("el alta debe normalizar los datos de contacto: %s", got)
	}
	request("POST", suppliersPath, `{"name":"Avíos"}`, origin, cookie, 201)
	if got := request("GET", suppliersPath+"?limit=1", "", "", cookie, 200).Body.String(); !strings.Contains(got, `"name":"Avíos"`) ||
		!strings.Contains(got, `"hasMore":true,"canManage":true`) {
		t.Fatalf("la lista debe ordenar y paginar los proveedores: %s", got)
	}
	if got := request("GET", suppliersPath+"?limit=1&offset=1", "", "", cookie, 200).Body.String(); !strings.Contains(got, supplierID) || !strings.Contains(got, `"hasMore":false`) {
		t.Fatalf("la segunda página debe traer el proveedor restante: %s", got)
	}
	for _, filter := range []string{"TEXTILES", "PEDIDOS%40NORTE", "555"} {
		if got := request("GET", suppliersPath+"?q="+filter, "", "", manager, 200).Body.String(); !strings.Contains(got, supplierID) ||
			!strings.Contains(got, `"canManage":false`) {
			t.Fatalf("el encargado debe consultar por nombre o contacto: %s", got)
		}
	}
	if got := request("GET", suppliersPath+"?q=Textiles", "", "", second, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Fatalf("un comercio consultó proveedores de otro: %s", got)
	}
	request("PATCH", supplierPath, `{"name":"Otro comercio"}`, origin, second, 404)
	request("PATCH", supplierPath, `{"status":"inactive"}`, origin, manager, 403)
	request("PATCH", supplierPath, `{"status":"desconocido"}`, origin, cookie, 400)
	inactiveSupplier := request("PATCH", supplierPath, `{"email":"","status":"inactive"}`, origin, cookie, 200)
	if got := inactiveSupplier.Body.String(); !strings.Contains(got, `"name":"Textiles Norte","email":"","phone":"381 555 0100","status":"inactive"`) {
		t.Fatalf("editar el estado y limpiar un contacto debe conservar los demás datos: %s", got)
	}
	if got := request("GET", suppliersPath+"?q=Textiles", "", "", manager, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Fatalf("el encargado solo debe consultar proveedores activos: %s", got)
	}
	if got := request("GET", suppliersPath+"?q=Textiles", "", "", cookie, 200).Body.String(); !strings.Contains(got, `"status":"inactive"`) {
		t.Fatalf("el titular debe poder localizar al proveedor inactivo: %s", got)
	}
	request("PATCH", supplierPath, `{"status":"active","name":"Textiles Norte Mayorista"}`, origin, cookie, 200)
	if got := request("GET", suppliersPath+"?q=Mayorista", "", "", manager, 200).Body.String(); !strings.Contains(got, supplierID) {
		t.Fatalf("el proveedor reactivado debe volver a la consulta del encargado: %s", got)
	}
	const purchasesPath = "/api/v1/purchase-orders"
	purchaseQuery := "?store_id=" + created.StoreID
	purchaseInput := func(storeID, supplier, lines string) string {
		return fmt.Sprintf(`{"storeId":"%s","supplierId":"%s","items":%s}`, storeID, supplier, lines)
	}
	purchaseLine := func(variant string, quantity int, cost string) string {
		return fmt.Sprintf(`{"variantId":"%s","quantity":%d,"unitCost":%s}`, variant, quantity, cost)
	}
	lineWithoutCost := purchaseLine(variantID, 100, "null")
	lineWithCost := purchaseLine(spare, 2, `"0"`)
	lineList := "[" + lineWithoutCost + "," + lineWithCost + "]"
	validPurchase := purchaseInput(created.StoreID, supplierID, lineList)
	request("GET", purchasesPath+purchaseQuery, "", "", nil, 401)
	request("GET", purchasesPath+purchaseQuery, "", "", cashier, 403)
	request("GET", purchasesPath+purchaseQuery, "", "", operator, 403)
	request("GET", purchasesPath+purchaseQuery, "", "", second, 404)
	request("GET", purchasesPath+"?store_id="+unassigned, "", "", manager, 404)
	request("GET", purchasesPath+purchaseQuery+"&limit=51", "", "", manager, 400)
	request("POST", purchasesPath, validPurchase, origin, cookie, 400)
	expect(keyedCash(purchasesPath, purchaseInput(otherStore, supplierID, lineList), "purchase-wrong-store", manager), 404)
	expect(keyedCash(purchasesPath, validPurchase, "purchase-cashier", cashier), 403)
	expect(keyedCash(purchasesPath, purchaseInput(created.StoreID, supplierID, "["+lineWithCost+","+lineWithCost+"]"),
		"purchase-duplicate-item", manager), 400)
	expect(keyedCash(purchasesPath, purchaseInput(created.StoreID, supplierID, "["+purchaseLine(variantID, 1, `"1.005"`)+"]"),
		"purchase-invalid-cost", manager), 400)
	foreignSupplierID := readID(request("POST", suppliersPath, `{"name":"Proveedor de otro comercio"}`, origin, second, 201))
	checkRefundCode(keyedCash(purchasesPath, purchaseInput(created.StoreID, foreignSupplierID, lineList),
		"purchase-external-supplier", manager), 409, "purchase_supplier_unavailable")
	checkRefundCode(keyedCash(purchasesPath, purchaseInput(created.StoreID, supplierID,
		"["+lineWithoutCost+","+purchaseLine(foreignSupplierID, 1, "null")+"]"), "purchase-missing-variant", manager),
		409, "purchase_variant_unavailable")
	if got := request("GET", purchasesPath+purchaseQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Fatalf("una orden con una variante inválida no debe dejar una cabecera ni líneas: %s", got)
	}

	var stockBeforeOrder int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE tenant_id=$1", created.TenantID).Scan(&stockBeforeOrder); err != nil {
		t.Fatal(err)
	}
	ordered := expect(keyedCash(purchasesPath, validPurchase, "purchase-confirm-order", manager), 201)
	orderID := readID(ordered)
	if got := ordered.Body.String(); !strings.Contains(got, `"supplierName":"Textiles Norte Mayorista","reference":"","currency":"ARS","status":"OPEN"`) ||
		!strings.Contains(got, `"quantity":100,"unitCost":null`) || !strings.Contains(got, `"quantity":2,"unitCost":"0.00"`) {
		t.Fatalf("la orden debe distinguir el costo faltante del cero explícito: %s", got)
	}
	retryOrder := purchaseInput(created.StoreID, supplierID, "["+purchaseLine(spare, 2, `"0.00"`)+","+lineWithoutCost+"]")
	if replay := expect(keyedCash(purchasesPath, retryOrder, "purchase-confirm-order", manager), 201); replay.Body.String() != ordered.Body.String() {
		t.Fatal("el reintento debe recuperar la misma orden con líneas reordenadas e importes equivalentes")
	}
	checkRefundCode(keyedCash(purchasesPath, strings.Replace(validPurchase, `"quantity":100`, `"quantity":99`, 1),
		"purchase-confirm-order", manager), 409, "idempotency_conflict")
	checkRefundCode(keyedCash(purchasesPath, validPurchase, "purchase-confirm-order", cookie), 409, "idempotency_conflict")

	decimalOrder := purchaseInput(created.StoreID, supplierID, "["+purchaseLine(spare, 5, `"125.6"`)+"]")
	go func() { raceSales <- keyedCash(purchasesPath, decimalOrder, "purchase-concurrent-retry", manager).Code }()
	go func() { raceSales <- keyedCash(purchasesPath, decimalOrder, "purchase-concurrent-retry", manager).Code }()
	aSale, bSale = <-raceSales, <-raceSales
	if aSale != 201 || bSale != 201 {
		t.Fatalf("dos reintentos simultáneos deben recuperar la misma orden: %d y %d", aSale, bSale)
	}
	if got := request("GET", purchasesPath+purchaseQuery+"&limit=1", "", "", manager, 200).Body.String(); !strings.Contains(got, `"unitCost":"125.60"`) ||
		!strings.Contains(got, `"hasMore":true,"canManage":true`) {
		t.Fatalf("la lista debe paginar y conservar el costo decimal exacto: %s", got)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM purchase_orders WHERE tenant_id=$1 AND store_id=$2",
		created.TenantID, created.StoreID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("los reintentos no deben duplicar órdenes: %d %v", auditCount, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE tenant_id=$1", created.TenantID).Scan(&auditCount); err != nil || auditCount != stockBeforeOrder {
		t.Fatalf("registrar una orden no debe mover stock: %d %v", auditCount, err)
	}

	request("PATCH", supplierPath, `{"status":"inactive","name":"Textiles Norte renombrada"}`, origin, cookie, 200)
	checkRefundCode(keyedCash(purchasesPath, validPurchase, "purchase-inactive-supplier", manager), 409, "purchase_supplier_unavailable")
	if replay := expect(keyedCash(purchasesPath, validPurchase, "purchase-confirm-order", manager), 201); replay.Body.String() != ordered.Body.String() {
		t.Fatal("la desactivación y el cambio de nombre del proveedor no deben alterar una orden registrada")
	}
	if got := request("GET", purchasesPath+purchaseQuery+"&offset=1&limit=1", "", "", manager, 200).Body.String(); !strings.Contains(got, orderID) ||
		!strings.Contains(got, `"supplierName":"Textiles Norte Mayorista"`) {
		t.Fatalf("la orden histórica debe conservar la ficha original del proveedor: %s", got)
	}

	var receiptOrder purchases.Order
	if err := json.Unmarshal(ordered.Body.Bytes(), &receiptOrder); err != nil {
		t.Fatal(err)
	}
	var orderLine, secondOrderLine string
	for _, item := range receiptOrder.Items {
		if item.VariantID == variantID {
			orderLine = item.ID
		} else if item.VariantID == spare {
			secondOrderLine = item.ID
		}
	}
	receiptsPath := purchasesPath + "/" + orderID + "/receipts"
	readReceiptSummary := func(filter string) purchases.ReceiptSummary {
		t.Helper()
		response := request("GET", receiptsPath+purchaseQuery+filter, "", "", manager, 200)
		var page purchases.ReceiptPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Summary
	}
	if summary := readReceiptSummary(""); summary.ReceiptCount != 0 || summary.KnownValue != nil ||
		summary.TotalValue == nil || *summary.TotalValue != "0.00" {
		t.Fatalf("un historial vacío debe tener valor total cero: %+v", summary)
	}
	receiptsCSVPath := receiptsPath + "/csv" + purchaseQuery
	readReceiptCSV := func(filter string) [][]string {
		t.Helper()
		response := request("GET", receiptsCSVPath+filter, "", "", manager, 200)
		if response.Header().Get("Content-Disposition") != `attachment; filename="recepciones-de-compra.csv"` ||
			response.Header().Get("Content-Type") != csvContentType {
			t.Fatalf("encabezados inesperados del CSV de recepciones: %v", response.Header())
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := readReceiptCSV(""); len(rows) != 1 {
		t.Fatalf("una orden sin recepciones debe exportar solo el encabezado: %v", rows)
	}
	request("GET", receiptsCSVPath, "", "", cashier, 403)
	request("GET", receiptsCSVPath, "", "", second, 404)
	request("GET", purchasesPath+"/"+otherStore+"/receipts/csv"+purchaseQuery, "", "", manager, 404)
	receiptInput := func(destination, note, items string) string {
		return fmt.Sprintf(`{"storeId":"%s","locationId":"%s","note":%q,"items":[%s]}`, created.StoreID, destination, note, items)
	}
	receiptLine := func(line string, accepted, rejected int) string {
		if rejected > 0 {
			return fmt.Sprintf(`{"orderItemId":"%s","accepted":%d,"rejected":%d,"rejectionReason":"damaged"}`, line, accepted, rejected)
		}
		return fmt.Sprintf(`{"orderItemId":"%s","accepted":%d,"rejected":%d}`, line, accepted, rejected)
	}
	readPurchasePage := func(filter string) purchases.Page {
		t.Helper()
		var page purchases.Page
		result := request("GET", purchasesPath+purchaseQuery+filter, "", "", manager, 200)
		if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	purchaseCSVPath := purchasesPath + "/csv" + purchaseQuery
	readPurchaseCSV := func(filter string) [][]string {
		t.Helper()
		response := request("GET", purchaseCSVPath+filter, "", "", manager, 200)
		if response.Header().Get("Content-Disposition") != `attachment; filename="ordenes-de-compra.csv"` ||
			response.Header().Get("Content-Type") != csvContentType {
			t.Fatalf("encabezados inesperados del CSV de órdenes: %v", response.Header())
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := readPurchaseCSV("&status=pending&q=mayorista&limit=1&offset=1"); len(rows) != 4 {
		t.Fatalf("el CSV debe incluir las tres líneas filtradas aunque el listado esté paginado: %v", rows)
	}
	if rows := readPurchaseCSV("&q=sin-coincidencias"); len(rows) != 1 {
		t.Fatalf("una búsqueda sin coincidencias debe exportar el encabezado: %v", rows)
	}
	orderTimezone, err := time.LoadLocation("America/Argentina/Tucuman")
	if err != nil {
		t.Fatal(err)
	}
	orderDate := receiptOrder.OrderedAt.In(orderTimezone)
	orderDay, nextOrderDay := orderDate.Format(time.DateOnly), orderDate.AddDate(0, 0, 1).Format(time.DateOnly)
	var orderSKU string
	for _, item := range receiptOrder.Items {
		if item.VariantID == variantID {
			orderSKU = item.SKU
		}
	}
	dateSearch := "&q=" + url.QueryEscape(orderSKU)
	for _, dateFilter := range []string{"&from_date=" + orderDay, "&to_date=" + orderDay, "&from_date=" + orderDay + "&to_date=" + orderDay} {
		page := readPurchasePage(dateSearch + dateFilter)
		if len(page.Items) != 1 || page.Items[0].ID != orderID || page.Summary.OrderedUnits != 102 ||
			len(readPurchaseCSV(dateSearch+dateFilter)) != 3 {
			t.Fatalf("el día de emisión debe incluir la orden completa en listado, resumen y CSV: %+v", page)
		}
	}
	if page := readPurchasePage(dateSearch + "&from_date=" + nextOrderDay); len(page.Items) != 0 || page.Summary.OrderCount != 0 ||
		len(readPurchaseCSV(dateSearch+"&from_date="+nextOrderDay)) != 1 {
		t.Fatalf("el día siguiente debe excluir la orden del listado, resumen y CSV: %+v", page)
	}
	request("GET", purchasesPath+purchaseQuery+"&to_date=2026-02-29", "", "", manager, 400)
	request("GET", purchaseCSVPath+"&from_date="+nextOrderDay+"&to_date="+orderDay, "", "", manager, 400)
	request("GET", purchaseCSVPath, "", "", nil, 401)
	request("GET", purchaseCSVPath, "", "", second, 404)
	request("GET", purchaseCSVPath, "", "", cashier, 403)
	request("GET", purchaseCSVPath+"&status=invalid", "", "", manager, 400)
	readPurchaseStatus := func() purchases.Order {
		t.Helper()
		page := readPurchasePage("")
		for _, item := range page.Items {
			if item.ID == orderID {
				return item
			}
		}
		t.Fatal("la orden debe seguir visible en su sucursal")
		return purchases.Order{}
	}
	if page := readPurchasePage("&status=pending&q=+TEXTILES+norte+mayorista+&limit=1"); len(page.Items) != 1 || !page.HasMore {
		t.Fatalf("los filtros deben usar el proveedor histórico antes de paginar: %+v", page)
	} else if page.Summary != (purchases.OrderSummary{OrderCount: 2, OrderedUnits: 107, PendingUnits: 107, PendingOrders: 2}) {
		t.Fatalf("el resumen debe sumar ambas órdenes antes de paginar: %+v", page.Summary)
	}
	for _, query := range []string{"Textiles Norte renombrada", "%"} {
		if page := readPurchasePage("&q=" + url.QueryEscape(query)); len(page.Items) != 0 {
			t.Fatalf("la búsqueda literal no debe cambiar el proveedor guardado: %+v", page)
		} else if page.Summary != (purchases.OrderSummary{}) {
			t.Fatalf("sin coincidencias los totales deben quedar en cero: %+v", page.Summary)
		}
	}
	for _, item := range receiptOrder.Items {
		for _, query := range []string{item.SKU, item.Product} {
			page := readPurchasePage("&status=OPEN&q=" + url.QueryEscape(query))
			found := false
			for _, order := range page.Items {
				found = found || (order.ID == orderID && len(order.Items) == len(receiptOrder.Items))
			}
			if !found {
				t.Fatalf("la búsqueda por variante debe devolver la orden completa: %+v", page)
			}
		}
	}
	request("GET", purchasesPath+purchaseQuery+"&status=invalid", "", "", manager, 400)
	request("GET", receiptsPath+purchaseQuery, "", "", cashier, 403)
	request("GET", receiptsPath+purchaseQuery, "", "", second, 404)
	request("GET", purchasesPath+"/"+otherStore+"/receipts"+purchaseQuery, "", "", manager, 404)
	expect(keyedCash(receiptsPath, receiptInput(sellable, "", receiptLine(orderLine, 1, 0)), "purchase-receipt-cashier", cashier), 403)
	checkRefundCode(keyedCash(receiptsPath, receiptInput(northLocation, "", receiptLine(orderLine, 1, 0)), "purchase-receipt-other-location", manager),
		409, "purchase_receipt_location")
	checkRefundCode(keyedCash(receiptsPath, receiptInput(sellable, "", receiptLine(otherStore, 1, 0)), "purchase-receipt-other-line", manager),
		409, "purchase_receipt_quantity")
	checkRefundCode(keyedCash(receiptsPath, receiptInput(sellable, "", receiptLine(orderLine, 101, 0)), "purchase-receipt-over-limit", manager),
		409, "purchase_receipt_quantity")

	overflowLocation := readID(request("POST", locationsPath, locBody(created.StoreID, "RECEPCION-LIMITE"), origin, cookie, 201))
	expect(adjust(adjustBody(created.StoreID, overflowLocation, spare, 2147483647, "Tope para probar la recepción"),
		"purchase-receipt-stock-limit", manager), 201)
	checkRefundCode(keyedCash(receiptsPath, receiptInput(overflowLocation, "", receiptLine(orderLine, 1, 0)+","+receiptLine(secondOrderLine, 1, 0)),
		"purchase-receipt-rollback", manager), 409, "inventory_balance_conflict")
	if state := readPurchaseStatus(); state.Status != "OPEN" || state.Items[0].Received != 0 || state.Items[1].Received != 0 {
		t.Fatalf("un error de stock debe revertir toda la recepción: %+v", state)
	}
	if got := request("GET", receiptsPath+purchaseQuery, "", "", manager, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Fatalf("una recepción fallida no debe dejar historial: %s", got)
	}

	withReceiptCost := func(line, amount string) string {
		return strings.TrimSuffix(line, "}") + `,"unitCost":` + amount + "}"
	}
	for _, invalidCost := range []string{`"-0.01"`, `"1.005"`, `3.50`} {
		expect(keyedCash(receiptsPath, receiptInput(sellable, "", withReceiptCost(receiptLine(orderLine, 1, 0), invalidCost)),
			"purchase-receipt-invalid-cost", manager), 400)
	}
	expect(keyedCash(receiptsPath, receiptInput(sellable, "", withReceiptCost(receiptLine(orderLine, 0, 1), `"0"`)),
		"purchase-receipt-rejection-with-cost", manager), 400)
	var priorCostQuantity int64
	var costStatus string
	if err := db.QueryRow(ctx, `SELECT quantity_on_hand FROM inventory_cost_balances
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID).Scan(&priorCostQuantity); err != nil {
		t.Fatal(err)
	}
	execSQL(t, ctx, db, `UPDATE inventory_cost_balances SET status='known',average_unit_cost=12.34
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID)
	partialReceiptInput := receiptInput(sellable, "Dos unidades rechazadas", withReceiptCost(receiptLine(orderLine, 60, 2), `"10.1"`))
	purchaseExistence := "/api/v1/inventory/existences/" + variantID + "?store_id=" + created.StoreID
	readPurchaseLocation := func() int {
		t.Helper()
		var detail struct {
			Locations []struct {
				ID     string
				OnHand *int
			}
		}
		res := request("GET", purchaseExistence, "", "", manager, 200)
		if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		for _, row := range detail.Locations {
			if row.ID == sellable && row.OnHand != nil {
				return *row.OnHand
			}
		}
		return 0
	}
	unitsBeforePurchase := readPurchaseLocation()
	partialPurchaseReceipt := expect(keyedCash(receiptsPath, partialReceiptInput, "purchase-receipt-partial", manager), 201)
	partialPurchaseReceiptID := readID(partialPurchaseReceipt)
	if current := readPurchaseLocation(); current-unitsBeforePurchase != 60 {
		t.Fatalf("existencias tras compra: antes %d, después %d; deben ingresar solo las 60 unidades aceptadas", unitsBeforePurchase, current)
	}
	if summary := readReceiptSummary(""); summary.ReceiptCount != 1 || summary.AcceptedWithCost != 60 ||
		summary.AcceptedWithoutCost != 0 || summary.RejectedUnits != 2 ||
		summary.TotalValue == nil || *summary.TotalValue != "606.00" {
		t.Fatalf("el resumen debe valuar las unidades aceptadas con el costo de recepción: %+v", summary)
	}
	if page := readPurchasePage("&status=PARTIALLY_RECEIVED"); len(page.Items) != 1 || page.Items[0].ID != orderID {
		t.Fatalf("la orden debe figurar como parcialmente recibida: %+v", page)
	}
	partialCSV := readPurchaseCSV("&status=PARTIALLY_RECEIVED")
	if len(partialCSV) != 3 {
		t.Fatalf("la orden parcial debe conservar las dos líneas en el CSV: %v", partialCSV)
	}
	var foundUnknownCost, foundZeroCost bool
	for _, row := range partialCSV[1:] {
		if row[0] != orderID || row[3] != "Textiles Norte Mayorista" || row[4] != "PARTIALLY_RECEIVED" {
			t.Fatalf("el CSV debe mantener los datos históricos y el estado de la orden: %v", row)
		}
		foundUnknownCost = foundUnknownCost || (row[10] == "100" && row[11] == "60" && row[12] == "40" && row[13] == "")
		foundZeroCost = foundZeroCost || (row[10] == "2" && row[11] == "0" && row[12] == "2" && row[13] == "0.00")
	}
	if !foundUnknownCost || !foundZeroCost {
		t.Fatalf("el CSV debe distinguir costo cero y faltante, y conservar las cantidades: %v", partialCSV)
	}
	if page := readPurchasePage("&status=pending"); len(page.Items) != 2 {
		t.Fatalf("pendientes debe incluir órdenes abiertas y parcialmente recibidas: %+v", page)
	} else if page.Summary != (purchases.OrderSummary{OrderCount: 2, OrderedUnits: 107, ReceivedUnits: 60, PendingUnits: 47, PendingOrders: 2}) {
		t.Fatalf("el resumen debe contar solo unidades aceptadas como recibidas: %+v", page.Summary)
	}
	if !strings.Contains(partialPurchaseReceipt.Body.String(), `"accepted":60,"rejected":2,"rejectionReason":"damaged","unitCost":"10.10"`) {
		t.Fatalf("la recepción debe conservar las cantidades informadas: %s", partialPurchaseReceipt.Body)
	}
	if state := readPurchaseStatus(); state.Status != "PARTIALLY_RECEIVED" {
		t.Fatalf("aceptar 60 de 100 debe dejar la orden parcialmente recibida: %+v", state)
	} else {
		for _, line := range state.Items {
			if line.ID == orderLine && (line.Received != 60 || line.Quantity-line.Received != 40) {
				t.Fatalf("los rechazos no se descuentan del pendiente: %+v", line)
			}
		}
	}
	var expectedWeightedCost, receivedWeightedCost string
	if err := db.QueryRow(ctx, `SELECT round(($1::numeric*12.34+60*10.10)/($1+60),2)::text`, priorCostQuantity).Scan(&expectedWeightedCost); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT status,average_unit_cost::text FROM inventory_cost_balances
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID).
		Scan(&costStatus, &receivedWeightedCost); err != nil || costStatus != "known" || receivedWeightedCost != expectedWeightedCost {
		t.Fatalf("la recepción debe ponderar solo las unidades aceptadas: status=%s costo=%s esperado=%s err=%v",
			costStatus, receivedWeightedCost, expectedWeightedCost, err)
	}
	rejectedOnly := expect(keyedCash(receiptsPath, receiptInput(sellable, "Variante rechazada", receiptLine(secondOrderLine, 0, 1)),
		"purchase-receipt-rejected-only", manager), 201)
	if err := db.QueryRow(ctx, "SELECT count(*) FROM stock_movements WHERE purchase_receipt_id=$1", readID(rejectedOnly)).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("una recepción de rechazos no debe mover stock: %d %v", auditCount, err)
	}
	checkRefundCode(keyedCash(receiptsPath, receiptInput(sellable, "Otro contenido", receiptLine(orderLine, 60, 2)),
		"purchase-receipt-partial", manager), 409, "idempotency_conflict")
	completion := receiptInput(sellable, "", receiptLine(orderLine, 40, 0)+","+withReceiptCost(receiptLine(secondOrderLine, 2, 0), `"0"`))
	go func() {
		raceSales <- keyedCash(receiptsPath, completion, "purchase-receipt-concurrent-a", manager).Code
	}()
	go func() {
		raceSales <- keyedCash(receiptsPath, completion, "purchase-receipt-concurrent-b", manager).Code
	}()
	aSale, bSale = <-raceSales, <-raceSales
	if !((aSale == 201 && bSale == 409) || (aSale == 409 && bSale == 201)) {
		t.Fatalf("solo una recepción debe consumir el saldo pendiente: %d y %d", aSale, bSale)
	}
	if state := readPurchaseStatus(); state.Status != "RECEIVED" || state.Items[0].Quantity != state.Items[0].Received ||
		state.Items[1].Quantity != state.Items[1].Received {
		t.Fatalf("la orden debe quedar recibida solo al completar todas sus líneas: %+v", state)
	}
	if page := readPurchasePage("&status=pending"); len(page.Items) != 1 || page.Items[0].ID == orderID {
		t.Fatalf("una orden completa debe salir de pendientes: %+v", page)
	}
	if page := readPurchasePage("&status=RECEIVED"); len(page.Items) != 1 || page.Items[0].ID != orderID {
		t.Fatalf("el filtro de recibidas debe encontrar la orden completa: %+v", page)
	} else if page.Summary != (purchases.OrderSummary{OrderCount: 1, OrderedUnits: 102, ReceivedUnits: 102, ReceivedOrders: 1}) {
		t.Fatalf("la orden completada debe quedar sin unidades pendientes: %+v", page.Summary)
	}
	var costKnown bool
	if err := db.QueryRow(ctx, `SELECT status,average_unit_cost IS NOT NULL FROM inventory_cost_balances
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID).Scan(&costStatus, &costKnown); err != nil {
		t.Fatal(err)
	}
	if costStatus != "unknown" || costKnown {
		t.Fatalf("la recepción sin costo debe dejar el promedio desconocido: status=%s known=%v", costStatus, costKnown)
	}
	var branchStock, costStock int64
	if err := db.QueryRow(ctx, `SELECT coalesce(sum(b.quantity_on_hand),0) FROM stock_balances b
		JOIN stock_locations l ON (l.tenant_id,l.id)=(b.tenant_id,b.location_id)
		WHERE b.tenant_id=$1 AND l.store_id=$2 AND b.variant_id=$3`, created.TenantID, created.StoreID, variantID).Scan(&branchStock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT quantity_on_hand FROM inventory_cost_balances
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, created.TenantID, created.StoreID, variantID).Scan(&costStock); err != nil || costStock != branchStock {
		t.Fatalf("el saldo de costeo debe seguir el stock agregado de la sucursal: costs=%d stock=%d err=%v", costStock, branchStock, err)
	}
	normalizedPartial := strings.Replace(partialReceiptInput, `"10.1"`, `"10.10"`, 1)
	if retry := expect(keyedCash(receiptsPath, normalizedPartial, "purchase-receipt-partial", manager), 201); retry.Body.String() != partialPurchaseReceipt.Body.String() {
		t.Fatal("el reintento debe devolver la recepción original aun con la orden completa")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements
		WHERE purchase_receipt_id=$1 AND movement_type='purchase_receipt' AND location_id=$2 AND quantity_delta=60`,
		partialPurchaseReceiptID, sellable).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("la primera recepción debe quedar vinculada a un solo ingreso de 60: %d %v", auditCount, err)
	}
	if got := request("GET", receiptsPath+purchaseQuery+"&limit=1&offset=2", "", "", manager, 200).Body.String(); !strings.Contains(got, partialPurchaseReceiptID) ||
		!strings.Contains(got, `"hasMore":false`) {
		t.Fatalf("el historial paginado debe conservar las tres recepciones: %s", got)
	}

	if got := request("GET", receiptsPath+purchaseQuery+"&limit=1", "", "", manager, 200).Body.String(); !strings.Contains(got, `"accepted":40,"rejected":0,"rejectionReason":null,"unitCost":null`) ||
		!strings.Contains(got, `"accepted":2,"rejected":0,"rejectionReason":null,"unitCost":"0.00"`) {
		t.Fatalf("el historial debe distinguir costo desconocido, cero explícito y costos por recepción: %s", got)
	}
	receiptCSV := readReceiptCSV("&limit=1&offset=1")
	if summary := readReceiptSummary("&limit=1&offset=1"); summary.ReceiptCount != 3 || summary.AcceptedWithCost != 62 ||
		summary.AcceptedWithoutCost != 40 || summary.RejectedUnits != 3 ||
		summary.KnownValue == nil || *summary.KnownValue != "606.00" || summary.TotalValue != nil {
		t.Fatalf("el resumen debe incluir todo el historial y distinguir cero de costo faltante: %+v", summary)
	}
	if len(receiptCSV) != 5 {
		t.Fatalf("el CSV debe conservar las cuatro líneas de las tres recepciones: %v", receiptCSV)
	}
	costsByAccepted := make(map[string]string)
	for _, row := range receiptCSV[1:] {
		if row[0] != orderID || row[3] == "" || row[9] != "ARS" {
			t.Fatalf("el CSV debe conservar la orden, la ubicación y la moneda: %v", row)
		}
		costsByAccepted[row[7]] = row[10]
		if row[7] == "60" && (row[1] != partialPurchaseReceiptID || row[4] != "Dos unidades rechazadas" || row[8] != "2") {
			t.Fatalf("el CSV debe conservar las diferencias y la observación de la recepción: %v", row)
		}
	}
	if len(costsByAccepted) != 4 || costsByAccepted["60"] != "10.10" ||
		costsByAccepted["40"] != "" || costsByAccepted["2"] != "0.00" || costsByAccepted["0"] != "" {
		t.Fatalf("el CSV debe distinguir los costos por recepción y el cero explícito: %v", receiptCSV)
	}
	var quotedOrderID, quotedOrderLine string
	if err := db.QueryRow(ctx, `SELECT o.id,i.id FROM purchase_orders o JOIN purchase_order_items i
		ON (i.tenant_id,i.purchase_order_id)=(o.tenant_id,o.id) WHERE o.tenant_id=$1 AND o.idempotency_key='purchase-concurrent-retry'`,
		created.TenantID).Scan(&quotedOrderID, &quotedOrderLine); err != nil {
		t.Fatal(err)
	}
	quotedOrderReceiptPath := purchasesPath + "/" + quotedOrderID + "/receipts"
	unpricedReceiptBody := receiptInput(sellable, "", receiptLine(quotedOrderLine, 1, 0))
	unpricedReceipt := expect(keyedCash(quotedOrderReceiptPath, unpricedReceiptBody, "purchase-receipt-cost-unknown", manager), 201)
	if !strings.Contains(unpricedReceipt.Body.String(), `"unitCost":null`) {
		t.Fatalf("el costo de la orden no debe copiarse al recibir sin elección explícita: %s", unpricedReceipt.Body)
	}
	nullCost := receiptInput(sellable, "", withReceiptCost(receiptLine(quotedOrderLine, 1, 0), "null"))
	if replay := expect(keyedCash(quotedOrderReceiptPath, nullCost, "purchase-receipt-cost-unknown", manager), 201); replay.Body.String() != unpricedReceipt.Body.String() {
		t.Fatal("el costo omitido y el costo nulo deben representar la misma solicitud")
	}
	checkRefundCode(keyedCash(quotedOrderReceiptPath, receiptInput(sellable, "", withReceiptCost(receiptLine(quotedOrderLine, 1, 0), `"0"`)),
		"purchase-receipt-cost-unknown", manager), 409, "idempotency_conflict")

	// Exact supplier history is independent of the historical display name.
	if got := request("POST", suppliersPath, `{"name":" textiles norte RENOMBRADA "}`, origin, cookie, 409).Body.String(); !strings.Contains(got, `"code":"supplier_duplicate"`) ||
		!strings.Contains(got, `"field":"name"`) || !strings.Contains(got, supplierID) {
		t.Fatalf("un homónimo sin confirmar debe informar el proveedor existente: %s", got)
	}
	homonymID := readID(request("POST", suppliersPath, `{"name":"Textiles Norte Mayorista","allowDuplicateName":true}`, origin, cookie, 201))
	emptySupplierID := readID(request("POST", suppliersPath, `{"name":"Sin compras"}`, origin, cookie, 201))
	homonymOrderID := readID(expect(keyedCash(purchasesPath, purchaseInput(created.StoreID, homonymID, "["+lineWithoutCost+"]"),
		"purchase-homonym", cookie), 201))
	branchOrderID := readID(expect(keyedCash(purchasesPath, purchaseInput(branch, homonymID, "["+lineWithoutCost+"]"),
		"purchase-homonym-branch", cookie), 201))
	exact := "&supplier_id=" + supplierID
	firstPage := readPurchasePage(exact + "&limit=1")
	secondPage := readPurchasePage(exact + "&limit=1&offset=1")
	if len(firstPage.Items) != 1 || len(secondPage.Items) != 1 || !firstPage.HasMore || secondPage.HasMore ||
		firstPage.Summary.OrderCount != 2 || secondPage.Summary != firstPage.Summary ||
		firstPage.Items[0].ID == secondPage.Items[0].ID {
		t.Fatalf("la paginación y los conteos deben usar la identidad del proveedor: %+v %+v", firstPage, secondPage)
	}
	for _, order := range append(firstPage.Items, secondPage.Items...) {
		if order.SupplierID != supplierID || order.SupplierName != "Textiles Norte Mayorista" {
			t.Fatalf("la orden de proveedor renombrado e inactivo debe conservar su nombre histórico: %+v", order)
		}
	}
	supplierCombined := exact + "&status=RECEIVED&from_date=" + orderDay + "&to_date=" + orderDay
	if page := readPurchasePage(supplierCombined); len(page.Items) != 1 || page.Items[0].ID != orderID || page.Summary.OrderCount != 1 {
		t.Fatalf("el proveedor debe combinarse con fechas y estado: %+v", page)
	}
	if rows := readPurchaseCSV(supplierCombined + "&offset=25"); len(rows) != 3 || rows[1][0] != orderID || rows[2][0] != orderID {
		t.Fatalf("el CSV debe respetar los mismos filtros sin paginar: %v", rows)
	}
	homonym := "&supplier_id=" + homonymID
	if page := readPurchasePage(homonym); len(page.Items) != 1 || page.Items[0].ID != homonymOrderID || page.Summary.OrderCount != 1 {
		t.Fatalf("el proveedor homónimo debe tener historial independiente: %+v", page)
	}
	for _, empty := range []string{emptySupplierID, foreignSupplierID, "00000000-0000-4000-8000-000000000001"} {
		filter := "&supplier_id=" + empty
		if page := readPurchasePage(filter); len(page.Items) != 0 || page.Summary.OrderCount != 0 || len(readPurchaseCSV(filter)) != 1 {
			t.Fatalf("un UUID sin compras visibles debe devolver listado y CSV vacíos: %+v", page)
		}
	}
	for _, endpoint := range []string{purchasesPath, purchasesPath + "/csv"} {
		request("GET", endpoint+purchaseQuery+"&supplier_id=invalido", "", "", manager, 400)
		request("GET", endpoint+purchaseQuery+homonym, "", "", cashier, 403)
		request("GET", endpoint+purchaseQuery+homonym, "", "", second, 404)
		request("GET", endpoint+"?store_id="+branch+homonym, "", "", manager, 404)
		result := request("GET", endpoint+"?store_id="+branch+homonym, "", "", cookie, 200).Body.String()
		if !strings.Contains(result, branchOrderID) || strings.Contains(result, homonymOrderID) {
			t.Fatalf("el titular debe ver únicamente las órdenes de la sucursal seleccionada: %s", result)
		}
	}
	if got := request("GET", purchasesPath+"?store_id="+otherStore+exact, "", "", second, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Fatalf("un proveedor ajeno debe ser indistinguible de uno inexistente: %s", got)
	}

	t.Run("compras con referencia, diferencias y cancelación", func(t *testing.T) {
		// Proveedor con identificación fiscal y contacto: la identificación no se repite.
		taxSupplier := request("POST", suppliersPath, `{"name":"Distribuidora Fiscal","taxId":" 30-71234567-8 ","contactName":" Laura Paz "}`, origin, cookie, 201)
		taxSupplierID := readID(taxSupplier)
		if got := taxSupplier.Body.String(); !strings.Contains(got, `"taxId":"30-71234567-8","contactName":"Laura Paz"`) {
			t.Fatalf("el alta debe normalizar identificación y contacto: %s", got)
		}
		if got := request("POST", suppliersPath, `{"name":"Otra razón social","taxId":"30-71234567-8"}`, origin, cookie, 409).Body.String(); !strings.Contains(got, `"field":"taxId"`) {
			t.Fatalf("la identificación fiscal repetida debe rechazarse: %s", got)
		}
		request("POST", suppliersPath, `{"name":"Distribuidora Fiscal","taxId":"30-71234567-8","allowDuplicateName":true}`, origin, cookie, 409)
		request("PATCH", suppliersPath+"/"+taxSupplierID, `{"name":"Textiles Norte Mayorista"}`, origin, cookie, 409)
		for _, filter := range []string{"71234567", "laura"} {
			if got := request("GET", suppliersPath+"?q="+filter, "", "", manager, 200).Body.String(); !strings.Contains(got, taxSupplierID) {
				t.Fatalf("la búsqueda debe incluir identificación y contacto: %s", got)
			}
		}

		stockAt := func(variant string) int {
			t.Helper()
			var quantity int
			if err := db.QueryRow(ctx, "SELECT coalesce(sum(quantity_on_hand),0) FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
				created.TenantID, sellable, variant).Scan(&quantity); err != nil {
				t.Fatal(err)
			}
			return quantity
		}
		orderBody := func(reference string, lines ...string) string {
			return fmt.Sprintf(`{"storeId":"%s","supplierId":"%s","reference":%q,"items":[%s]}`, created.StoreID, taxSupplierID, reference, strings.Join(lines, ","))
		}
		readOrder := func(res *httptest.ResponseRecorder) purchases.Order {
			t.Helper()
			var order purchases.Order
			if err := json.Unmarshal(res.Body.Bytes(), &order); err != nil {
				t.Fatal(err)
			}
			return order
		}
		before, spareBefore := stockAt(variantID), stockAt(spare)
		expect(keyedCash(purchasesPath, orderBody(strings.Repeat("x", 61), purchaseLine(variantID, 5, "null")), "purchase-long-reference", manager), 400)
		complete := readOrder(expect(keyedCash(purchasesPath, orderBody(" FC A 0001-00001234 ", purchaseLine(variantID, 5, `"1500"`)), "purchase-reference", manager), 201))
		partial := readOrder(expect(keyedCash(purchasesPath, orderBody("", purchaseLine(spare, 4, "null")), "purchase-to-cancel", manager), 201))
		if complete.Reference != "FC A 0001-00001234" || complete.Number < 1 || partial.Number != complete.Number+1 || complete.Items[0].ProductID != product {
			t.Fatalf("el pedido debe numerarse correlativamente y guardar la referencia: %+v %+v", complete, partial)
		}
		if stockAt(variantID) != before || stockAt(spare) != spareBefore {
			t.Fatal("registrar un pedido no debe modificar el stock")
		}
		orderPath := purchasesPath + "/" + complete.ID
		if got := readOrder(request("GET", orderPath+purchaseQuery, "", "", manager, 200)); got.ID != complete.ID || got.Number != complete.Number {
			t.Fatalf("la consulta individual debe devolver el pedido: %+v", got)
		}
		request("GET", orderPath+purchaseQuery, "", "", cashier, 403)
		request("GET", orderPath+purchaseQuery, "", "", second, 404)
		request("GET", purchasesPath+"/"+partial.ID+"?store_id="+branch, "", "", cookie, 404)
		for _, query := range []string{fmt.Sprintf("OC-%04d", complete.Number), "0001-00001234"} {
			if page := readPurchasePage("&q=" + url.QueryEscape(query)); len(page.Items) != 1 || page.Items[0].ID != complete.ID {
				t.Fatalf("la búsqueda debe encontrar el pedido por número o referencia %q: %+v", query, page)
			}
		}

		completeReceipts := orderPath + "/receipts"
		line := complete.Items[0].ID
		reasonLine := func(accepted, rejected int, reason string) string {
			return fmt.Sprintf(`{"orderItemId":"%s","accepted":%d,"rejected":%d,"rejectionReason":%q}`, line, accepted, rejected, reason)
		}
		expect(keyedCash(completeReceipts, receiptInput(sellable, "", fmt.Sprintf(`{"orderItemId":"%s","accepted":1,"rejected":1}`, line)), "purchase-reason-missing", manager), 400)
		expect(keyedCash(completeReceipts, receiptInput(sellable, "", reasonLine(1, 1, "perdida")), "purchase-reason-invalid", manager), 400)
		expect(keyedCash(completeReceipts, receiptInput(sellable, "", reasonLine(1, 0, "damaged")), "purchase-reason-without-units", manager), 400)
		// Llegaron 7: se aceptan las 5 pedidas y las 2 de más quedan registradas como excedente.
		excess := expect(keyedCash(completeReceipts, receiptInput(sellable, "Remito 77", reasonLine(5, 2, "excess")), "purchase-excess", manager), 201)
		if got := excess.Body.String(); !strings.Contains(got, `"accepted":5,"rejected":2,"rejectionReason":"excess"`) ||
			!strings.Contains(got, fmt.Sprintf(`"stockBefore":%d,"stockAfter":%d`, before, before+5)) {
			t.Fatalf("la recepción debe registrar el excedente y el movimiento de stock: %s", got)
		}
		if stockAt(variantID) != before+5 {
			t.Fatalf("solo las unidades aceptadas deben ingresar al stock: %d", stockAt(variantID)-before)
		}
		if got := readOrder(request("GET", orderPath+purchaseQuery, "", "", manager, 200)); got.Status != "RECEIVED" || got.Items[0].Received != 5 || got.Items[0].Rejected != 2 {
			t.Fatalf("el pedido debe quedar recibido con el excedente informado: %+v", got)
		}
		checkRefundCode(keyedCash(completeReceipts, receiptInput(sellable, "", reasonLine(0, 1, "damaged")), "purchase-after-received", manager), 409, "purchase_order_closed")
		checkRefundCode(keyedCash(orderPath+"/cancel", fmt.Sprintf(`{"storeId":"%s","reason":""}`, created.StoreID), "", manager), 409, "purchase_order_closed")

		partialPath := purchasesPath + "/" + partial.ID
		damaged := fmt.Sprintf(`{"orderItemId":"%s","accepted":1,"rejected":1,"rejectionReason":"damaged"}`, partial.Items[0].ID)
		expect(keyedCash(partialPath+"/receipts", receiptInput(sellable, "", damaged), "purchase-damaged", manager), 201)
		if stockAt(spare) != spareBefore+1 {
			t.Fatal("la unidad dañada no debe ingresar al stock")
		}
		if got := readOrder(request("GET", partialPath+purchaseQuery, "", "", manager, 200)); got.Status != "PARTIALLY_RECEIVED" || got.Items[0].Received != 1 || got.Items[0].Rejected != 1 {
			t.Fatalf("la unidad dañada debe seguir pendiente: %+v", got)
		}
		cancelBody := fmt.Sprintf(`{"storeId":"%s","reason":" El proveedor no tiene más unidades "}`, created.StoreID)
		expect(keyedCash(partialPath+"/cancel", cancelBody, "", cashier), 403)
		expect(keyedCash(partialPath+"/cancel", cancelBody, "", second), 404)
		expect(keyedCash(partialPath+"/cancel", fmt.Sprintf(`{"storeId":"%s","reason":%q}`, created.StoreID, strings.Repeat("x", 301)), "", manager), 400)
		cancelled := readOrder(expect(keyedCash(partialPath+"/cancel", cancelBody, "", manager), 200))
		if cancelled.Status != "CANCELLED" || cancelled.CancelledAt == nil || cancelled.CancelReason != "El proveedor no tiene más unidades" || cancelled.Items[0].Received != 1 {
			t.Fatalf("la cancelación debe conservar lo recibido y registrar el motivo: %+v", cancelled)
		}
		if again := readOrder(expect(keyedCash(partialPath+"/cancel", cancelBody, "", manager), 200)); again.CancelledAt == nil || !again.CancelledAt.Equal(*cancelled.CancelledAt) {
			t.Fatalf("cancelar dos veces no debe cambiar la cancelación original: %+v", again)
		}
		checkRefundCode(keyedCash(partialPath+"/receipts", receiptInput(sellable, "", fmt.Sprintf(`{"orderItemId":"%s","accepted":1,"rejected":0}`, partial.Items[0].ID)),
			"purchase-after-cancel", manager), 409, "purchase_order_closed")
		if stockAt(spare) != spareBefore+1 {
			t.Fatal("un pedido cancelado no debe recibir stock")
		}
		if page := readPurchasePage("&status=CANCELLED"); len(page.Items) != 1 || page.Items[0].ID != partial.ID ||
			page.Summary != (purchases.OrderSummary{OrderCount: 1, OrderedUnits: 4, ReceivedUnits: 1}) {
			t.Fatalf("un pedido cancelado no debe contar unidades pendientes: %+v", page)
		}
		if page := readPurchasePage("&status=pending"); slices.ContainsFunc(page.Items, func(order purchases.Order) bool { return order.ID == partial.ID }) {
			t.Fatal("un pedido cancelado no debe figurar como pendiente")
		}
	})

	t.Run("operación única de cambio o devolución", func(t *testing.T) {
		register := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-CAMBIOS"), origin, cookie, 201))
		session := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, register, `"0"`), "cambios-session", manager), 201))
		expect(adjust(adjustBody(created.StoreID, sellable, variantID, 5, "Stock para cambios"), "cambios-stock", manager), 201)
		var price struct {
			Amount string `json:"amount"`
		}
		if err := json.Unmarshal(request("GET", "/api/v1/stores/"+created.StoreID+"/variants/"+variantID+"/price", "", "", manager, 200).Body.Bytes(), &price); err != nil || price.Amount == "" {
			t.Fatalf("precio vigente: %+v %v", price, err)
		}
		saleID := readID(expect(keyedCash(salesPath, saleBody(session, sellable, "100000", line(variantID, 2, price.Amount)), "cambios-sale", manager), 201))
		opPath := salesPath + "/" + saleID + "/return-operations"
		type activity struct {
			SaleCount, ReturnCount, ReturnedUnits int64
			ReturnedAmount, NetSales, GrossSales  string
		}
		readActivity := func() activity {
			t.Helper()
			var page struct{ Activity []activity }
			if err := json.Unmarshal(request("GET", "/api/v1/reports/sales?store_id="+created.StoreID, "", "", manager, 200).Body.Bytes(), &page); err != nil || len(page.Activity) != 1 {
				t.Fatalf("actividad del reporte: %+v %v", page, err)
			}
			return page.Activity[0]
		}
		cents := func(value string) int64 {
			t.Helper()
			whole, fraction, _ := strings.Cut(value, ".")
			units, err := strconv.ParseInt(whole+(fraction + "00")[:2], 10, 64)
			if err != nil {
				t.Fatalf("importe %q: %v", value, err)
			}
			return units
		}
		reportBefore := readActivity()
		item := fmt.Sprintf(`{"variantId":"%s","quantity":1,"locationId":"%s","disposition":"sellable"}`, variantID, sellable)
		body := func(extra string) string {
			return fmt.Sprintf(`{"storeId":"%s","reason":"Prueba","items":[%s]%s}`, created.StoreID, item, extra)
		}
		refund := fmt.Sprintf(`,"refund":{"cashSessionId":"%s","payments":[{"method":"cash","amount":"%s"}]}`, session, price.Amount)
		if got := request("POST", opPath+"/preview", body(""), origin, manager, 200).Body.String(); !strings.Contains(got, `"returnAmount":"`+price.Amount+`"`) ||
			!strings.Contains(got, `"refund":"`+price.Amount+`"`) || !strings.Contains(got, `"charge":"0.00"`) {
			t.Fatalf("la vista previa debe usar el precio histórico: %s", got)
		}
		request("POST", opPath+"/preview", body(""), origin, second, 404)
		stockBefore := readBalances(sellableBalances, manager)
		checkRefundCode(keyedCash(opPath, body(""), "cambios-sin-resolver", manager), 409, "return_operation_unsettled")
		checkRefundCode(keyedCash(opPath, body(`,"charge":{"cashSessionId":"`+session+`","method":"cash","amount":"1.00","cashReceived":"1.00"}`), "cambios-cobro", manager), 409, "return_exchange_difference")
		expect(keyedCash(opPath, body(refund), "cambios-operador", operator), 403)
		var count int64
		if err := db.QueryRow(ctx, "SELECT count(*) FROM sale_returns WHERE tenant_id=$1 AND sale_id=$2", created.TenantID, saleID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("una operación rechazada no debe dejar devolución parcial: %d %v", count, err)
		}
		if after := readBalances(sellableBalances, manager); fmt.Sprint(after) != fmt.Sprint(stockBefore) {
			t.Fatalf("una operación rechazada no debe mover stock: %+v %+v", stockBefore, after)
		}
		done := expect(keyedCash(opPath, body(refund), "cambios-ok", manager), 201)
		if got := done.Body.String(); !strings.Contains(got, `"refund":{`) || !strings.Contains(got, `"amount":"`+price.Amount+`"`) {
			t.Fatalf("la operación debe devolver el reintegro registrado: %s", got)
		}
		if again := expect(keyedCash(opPath, body(refund), "cambios-ok", manager), 201); readID(again) != readID(done) {
			t.Fatal("el reintento con la misma clave debe devolver la misma operación")
		}
		checkRefundCode(keyedCash(opPath, body(refund+`,"debtCreditAmount":"1.00"`), "cambios-ok", manager), 409, "idempotency_conflict")
		if err := db.QueryRow(ctx, `SELECT count(*) FROM cash_movements WHERE tenant_id=$1 AND sale_id=$2 AND movement_type='sale_refund_cash'`,
			created.TenantID, saleID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("un solo reintegro en efectivo: %d %v", count, err)
		}
		expect(keyedCash(opPath, body(refund), "cambios-segunda", manager), 201)
		checkRefundCode(keyedCash(opPath, body(refund), "cambios-tercera", manager), 409, "return_quantity_exceeded")
		if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE tenant_id=$1 AND sale_id=$2 AND movement_type='sale_return'`,
			created.TenantID, saleID).Scan(&count); err != nil || count != 2 {
			t.Fatalf("dos devoluciones, dos movimientos: %d %v", count, err)
		}
		if got := request("GET", salesPath+"?store_id="+created.StoreID+"&q="+url.QueryEscape("no-existe-xyz"), "", "", manager, 200).Body.String(); !strings.Contains(got, `"items":[]`) {
			t.Fatalf("la búsqueda de ventas debe filtrar por texto: %s", got)
		}
		request("GET", salesPath+"?store_id="+created.StoreID+"&from_date=2026-12-02&to_date=2026-12-01", "", "", manager, 400)

		// Reportes: las dos devoluciones descuentan del neto al precio cobrado, sin alterar las ventas.
		reportAfter := readActivity()
		if reportAfter.SaleCount != reportBefore.SaleCount || reportAfter.GrossSales != reportBefore.GrossSales ||
			reportAfter.ReturnCount != reportBefore.ReturnCount+2 || reportAfter.ReturnedUnits != reportBefore.ReturnedUnits+2 ||
			cents(reportAfter.ReturnedAmount)-cents(reportBefore.ReturnedAmount) != 2*cents(price.Amount) ||
			cents(reportBefore.NetSales)-cents(reportAfter.NetSales) != 2*cents(price.Amount) {
			t.Fatalf("el reporte debe neto de devoluciones: antes %+v, después %+v, precio %s", reportBefore, reportAfter, price.Amount)
		}
		if got := request("GET", "/api/v1/reports/sales?store_id="+created.StoreID+"&from_date=2000-01-01&to_date=2000-01-31", "", "", manager, 200).Body.String(); !strings.Contains(got, `"activity":[]`) || !strings.Contains(got, `"topProducts":[]`) {
			t.Fatalf("período sin operaciones: %s", got)
		}
		request("GET", "/api/v1/reports/sales?store_id="+created.StoreID, "", "", second, 404)
		var listed struct {
			Items []struct {
				ID       string
				Payments []string
			}
		}
		if err := json.Unmarshal(request("GET", salesPath+"?store_id="+created.StoreID+"&limit=100", "", "", manager, 200).Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		for _, sale := range listed.Items {
			if sale.ID == saleID && (len(sale.Payments) != 1 || sale.Payments[0] != "cash") {
				t.Fatalf("medios de la venta en el historial: %+v", sale)
			}
		}
	})

	t.Run("existencias por sucursal", func(t *testing.T) {
		const endpoint = "/api/v1/inventory/existences"
		base := endpoint + "?store_id=" + created.StoreID
		makeProduct := func(name string) string {
			return readID(request("POST", productsPath, fmt.Sprintf(`{"name":%q}`, name), origin, cookie, 201))
		}
		item := makeProduct("EX1 Campera")
		catalogPatch(productsPath+"/"+item, `{"categoryId":"`+category+`","brandId":"`+brand+`"}`, origin, cookie, 200)
		empty := makeProduct("EX1 Sin variantes")
		makeVariant := func(sku, size string) string {
			return readID(request("POST", productsPath+"/"+item+"/variants",
				fmt.Sprintf(`{"sku":%q,"size":%q}`, sku, size), origin, cookie, 201))
		}
		positive := makeVariant("EX1-0001", "S")
		zero := makeVariant("EX1-0002", "M")
		unrecorded := makeVariant("EX1-0003", "L")
		inactive := makeVariant("EX1-0004", "XL")
		add := func(store, where, what string, units int, key string) {
			t.Helper()
			expect(adjust(adjustBody(store, where, what, units, "Fixture Existencias"), key, cookie), 201)
		}
		add(created.StoreID, sellable, positive, 7, "ex1-positive")
		add(created.StoreID, quarantineID, positive, 5, "ex1-nonsellable")
		add(branch, northLocation, positive, 90, "ex1-other-store")
		add(created.StoreID, sellable, zero, 3, "ex1-zero-in")
		add(created.StoreID, sellable, zero, -3, "ex1-zero-out")
		add(created.StoreID, sellable, inactive, 4, "ex1-inactive")
		catalogPatch(productsPath+"/"+item+"/variants/"+inactive, `{"status":"inactive"}`, origin, cookie, 200)
		request("PUT", minimumPath, minimumBody(created.StoreID, sellable, zero, "2"), origin, manager, 204)

		var page struct {
			Items []struct {
				ProductID, SKU             string
				VariantID, VariantStatus   *string
				OnHand, Reserved, Sellable *int
				LowStock                   bool
				RecordedLocations          int
			}
			Summary struct{ ProductsWithStock, VariantsSellable, VariantsZero, VariantsUnrecorded, VariantsLow int }
			Access  struct{ CanAdjust, CanManageMinimums, CanManageLocations, CanTransfer, CanCatalog bool }
			HasMore bool
		}
		decode := func(path string) {
			t.Helper()
			if err := json.Unmarshal(request("GET", path, "", "", manager, 200).Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		decode(base + "&q=EX1")
		if len(page.Items) != 5 || page.Summary.ProductsWithStock != 1 || page.Summary.VariantsSellable != 1 ||
			page.Summary.VariantsZero != 2 || page.Summary.VariantsUnrecorded != 1 || page.Summary.VariantsLow != 1 {
			t.Fatalf("resumen Existencias: %+v", page)
		}
		for _, row := range page.Items {
			switch row.SKU {
			case "EX1-0001":
				if row.Sellable == nil || *row.Sellable != 7 || row.OnHand == nil || *row.OnHand != 12 || row.RecordedLocations != 2 {
					t.Fatalf("stock positivo o alcance incorrecto: %+v", row)
				}
			case "EX1-0002":
				if row.Sellable == nil || *row.Sellable != 0 || !row.LowStock {
					t.Fatalf("cero registrado incorrecto: %+v", row)
				}
			case "EX1-0003":
				if row.Sellable != nil || row.OnHand != nil || row.RecordedLocations != 0 {
					t.Fatalf("saldo inexistente confundido con cero: %+v", row)
				}
			case "":
				if row.ProductID != empty || row.VariantID != nil {
					t.Fatalf("producto sin variantes incorrecto: %+v", row)
				}
			}
		}
		decode(base + "&categoryId=" + category + "&brandId=" + brand + "&q=EX1")
		if len(page.Items) != 4 {
			t.Fatalf("clasificaciones combinadas: %+v", page.Items)
		}
		decode(base + "&categoryId=" + otherCategory + "&q=EX1")
		if len(page.Items) != 0 {
			t.Fatalf("categoría de otro tenant: %+v", page.Items)
		}
		catalogPatch(productsPath+"/"+empty, `{"status":"inactive"}`, origin, cookie, 200)
		decode(base + "&q=EX1&producto=inactive")
		if len(page.Items) != 1 || page.Items[0].ProductID != empty || page.Items[0].VariantID != nil {
			t.Fatalf("producto inactivo sin variantes: %+v", page.Items)
		}
		for _, filter := range []string{"q=EX1-0003&saldo=unrecorded", "q=EX1&bajo=1", "q=EX1&variante=inactive",
			"q=EX1-0001&saldo=zero&ubicacion=" + quarantineID, "q=EX1&saldo=positive"} {
			decode(base + "&" + filter)
			if len(page.Items) != 1 {
				t.Fatalf("filtro %s: %+v", filter, page.Items)
			}
		}
		decode(base + "&q=EX1&page=2")
		if len(page.Items) != 0 || page.HasMore || page.Summary.VariantsSellable != 1 {
			t.Fatalf("página agotada: %+v", page)
		}
		detail := endpoint + "/" + positive + "?store_id="
		got := request("GET", detail+created.StoreID, "", "", manager, 200).Body.String()
		if !strings.Contains(got, quarantineID) || strings.Contains(got, northLocation) || !strings.Contains(got, `"sellable":7`) ||
			!strings.Contains(got, `"onHand":5,"reserved":0,"sellable":0`) {
			t.Fatalf("desglose por sucursal: %s", got)
		}
		request("GET", endpoint+"/"+unrecorded+"?store_id="+created.StoreID, "", "", cashier, 200)
		request("GET", base, "", "", nil, 401)
		request("GET", base, "", "", second, 404)
		request("GET", detail+otherStore, "", "", second, 404)
		request("GET", detail+branch, "", "", manager, 404)
		request("GET", base+"&ubicacion="+northLocation, "", "", manager, 404)
		for _, filter := range []string{"page=4002", "page=0", "q=%FF", "q=%00", "page=1&page=1", "store_id=" + branch, "unknown=1"} {
			request("GET", base+"&"+filter, "", "", manager, 400)
		}
		historic := readID(request("POST", locationsPath,
			fmt.Sprintf(`{"storeId":"%s","code":"EX1-HIST","name":"Ubicación anterior","type":"warehouse","isSellable":true}`, created.StoreID), origin, cookie, 201))
		add(created.StoreID, historic, positive, 2, "ex1-historic")
		execSQL(t, ctx, db, "UPDATE stock_locations SET status='inactive' WHERE id=$1", historic)
		got = request("GET", detail+created.StoreID, "", "", operator, 200).Body.String()
		if !strings.Contains(got, `"code":"EX1-HIST","active":false,"isSellable":true,"onHand":2,"reserved":0,"sellable":0`) ||
			!strings.Contains(got, `"canAdjust":true`) || !strings.Contains(got, `"canTransfer":true`) {
			t.Fatalf("ubicación inactiva o permisos de operador: %s", got)
		}
		request("GET", base+"&ubicacion="+historic, "", "", manager, 404)
		payload := request("GET", base+"&q=EX1", "", "", cashier, 200).Body.String()
		if !strings.Contains(payload, `"canAdjust":false`) || !strings.Contains(payload, `"canTransfer":false`) {
			t.Fatalf("accesos de caja: %s", payload)
		}
		execSQL(t, ctx, db, "DELETE FROM role_permissions WHERE role_code='CASHIER' AND permission_code='inventory:read'")
		t.Cleanup(func() {
			execSQL(t, ctx, db, "INSERT INTO role_permissions(role_code,permission_code) VALUES ('CASHIER','inventory:read') ON CONFLICT DO NOTHING")
		})
		request("GET", base, "", "", cashier, 403)
		request("GET", detail+created.StoreID, "", "", cashier, 403)

		volume := makeProduct("EX1 Rendimiento")
		execSQL(t, ctx, db, `INSERT INTO product_variants(tenant_id,product_id,sku)
			SELECT $1,$2,'EXPERF-'||lpad(i::text,4,'0') FROM generate_series(1,400) AS i`, created.TenantID, volume)
		execSQL(t, ctx, db, `INSERT INTO stock_balances(tenant_id,location_id,variant_id,quantity_on_hand)
			SELECT tenant_id,$3,id,5 FROM product_variants WHERE tenant_id=$1 AND product_id=$2 AND right(sku,1) IN ('0','5')`,
			created.TenantID, volume, sellable)
		start := time.Now()
		decode(base + "&q=EXPERF")
		if len(page.Items) != 25 || !page.HasMore || page.Summary.VariantsUnrecorded != 320 || page.Summary.VariantsSellable != 80 {
			t.Fatalf("paginación inicial: %d, %t, %+v", len(page.Items), page.HasMore, page.Summary)
		}
		sku := page.Items[0].SKU
		decode(base + "&q=EXPERF&page=16")
		if len(page.Items) != 25 || page.HasMore || page.Items[0].SKU == sku {
			t.Fatalf("paginación final: %d, %t, %s", len(page.Items), page.HasMore, page.Items[0].SKU)
		}
		t.Logf("dos páginas de 400 variantes en PostgreSQL local: %s", time.Since(start))
	})

	t.Run("historial de movimientos", func(t *testing.T) {
		const endpoint = "/api/v1/inventory/movements/history"
		base := endpoint + "?store_id=" + created.StoreID
		var page struct {
			Items []struct {
				ID, SKU, Product, Type, VariantID string
				Delta                             int
				CreatedAt                         time.Time
				ProductSnapshot, OtherEndHidden   bool
				Origin, Destination               *struct{ ID string }
			}
			Older, Newer string
		}
		read := func(query string, who *http.Cookie) {
			t.Helper()
			page.Items, page.Older, page.Newer = nil, "", ""
			if err := json.Unmarshal(request("GET", base+query, "", "", who, 200).Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		item := readID(request("POST", productsPath, `{"name":"Campera historial EX2"}`, origin, cookie, 201))
		itemVariant := readID(request("POST", productsPath+"/"+item+"/variants",
			`{"sku":"EX2-001","size":"L","color":"Gris"}`, origin, cookie, 201))
		add := func(key string) string {
			return readID(expect(adjust(adjustBody(created.StoreID, sellable, itemVariant, 1, "Ingreso histórico EX2"), key, cookie), 201))
		}
		for i := 0; i < 29; i++ {
			add(fmt.Sprintf("ex2-history-%d", i))
		}
		filter := "&variante=" + itemVariant + "&tipo=adjustment&ubicacion=" + sellable
		read(filter+"&q=historial", manager)
		if len(page.Items) != 25 || page.Older == "" || page.Newer != "" {
			t.Fatalf("primera página incorrecta: %+v", page)
		}
		seen := make(map[string]bool)
		for i, row := range page.Items {
			if i > 0 && (row.CreatedAt.After(page.Items[i-1].CreatedAt) ||
				row.CreatedAt.Equal(page.Items[i-1].CreatedAt) && row.ID >= page.Items[i-1].ID) {
				t.Fatalf("orden incorrecto: %+v", row)
			}
			seen[row.ID] = true
		}
		older := page.Older
		last := add("ex2-history-after-pagination")
		read(filter+"&siguiente="+older, manager)
		if len(page.Items) != 4 || page.Older != "" || page.Newer == "" {
			t.Fatalf("segunda página después de un ingreso concurrente: %+v", page)
		}
		for _, row := range page.Items {
			if seen[row.ID] || row.ID == last {
				t.Fatalf("fila repetida %s", row.ID)
			}
		}
		read(filter+"&anterior="+page.Newer, manager)
		if len(page.Items) != 25 || page.Newer == "" {
			t.Fatalf("retroceso paginado incorrecto: %+v", page)
		}

		term := url.QueryEscape("Campera historial")
		var actorName string
		if err := db.QueryRow(ctx, "SELECT name FROM users WHERE id=$1", created.OwnerID).Scan(&actorName); err != nil {
			t.Fatal(err)
		}
		zone, err := time.LoadLocation("America/Argentina/Tucuman")
		if err != nil {
			t.Fatal(err)
		}
		day := page.Items[0].CreatedAt.In(zone).Format(time.DateOnly)
		read(filter+"&q="+term+"&responsable="+url.QueryEscape(actorName)+"&desde="+day+"&hasta="+day+"&referencia="+last, operator)
		if len(page.Items) != 1 || page.Items[0].ID != last || page.Items[0].Origin != nil || page.Items[0].Destination != nil {
			t.Fatalf("filtros combinados o ajuste sin origen/destino: %+v", page)
		}
		for _, criteria := range []string{"&responsable=no-existe", "&q=no-existe", "&desde=2000-01-01&hasta=2000-01-01", "&tipo=transfer_receipt"} {
			read("&variante="+itemVariant+criteria, manager)
			if len(page.Items) != 0 {
				t.Fatalf("filtro %s: %+v", criteria, page)
			}
		}

		// Force identical timestamps without changing any real balance. This schema is disposable.
		execSQL(t, ctx, db, `INSERT INTO stock_movements(tenant_id,location_id,variant_id,actor_id,sku_snapshot,movement_type,
			quantity_delta,quantity_before,quantity_after,reason,created_at)
			SELECT $1,$2,$3,$4,'EX2-001','adjustment',1,0,1,'Desempate controlado','2001-01-01T12:00:00Z' FROM generate_series(1,2)`,
			created.TenantID, sellable, itemVariant, created.OwnerID)
		read("&variante="+itemVariant+"&desde=2001-01-01&hasta=2001-01-01", manager)
		if len(page.Items) != 2 || !page.Items[0].CreatedAt.Equal(page.Items[1].CreatedAt) || page.Items[0].ID <= page.Items[1].ID {
			t.Fatalf("desempate por UUID en movimientos simultáneos: %+v", page.Items)
		}
		read("&variante="+itemVariant+"&desde=2001-01-01&hasta=2001-01-01&siguiente="+page.Items[0].ID, manager)
		if len(page.Items) != 1 {
			t.Fatalf("cursor dentro de un empate: %+v", page.Items)
		}

		// A historical movement remains readable after the catalog and location are deactivated.
		oldLocation := readID(request("POST", locationsPath, locBody(created.StoreID, "EX2-HIST"), origin, cookie, 201))
		oldMovement := readID(expect(adjust(adjustBody(created.StoreID, oldLocation, itemVariant, 2, "Ubicación histórica"), "ex2-location", cookie), 201))
		execSQL(t, ctx, db, "UPDATE stock_locations SET status='inactive' WHERE id=$1", oldLocation)
		catalogPatch(productsPath+"/"+item, `{"name":"Nombre actual EX2","status":"inactive"}`, origin, cookie, 200)
		catalogPatch(productsPath+"/"+item+"/variants/"+itemVariant, `{"status":"inactive"}`, origin, cookie, 200)
		read("&ubicacion="+oldLocation+"&referencia="+oldMovement+"&q=EX2-001", cashier)
		if len(page.Items) != 1 || page.Items[0].SKU != "EX2-001" || page.Items[0].Product != "Nombre actual EX2" || page.Items[0].ProductSnapshot {
			t.Fatalf("el movimiento histórico debe conservar su SKU y distinguir las etiquetas actuales: %+v", page)
		}
		detail := endpoint + "/" + oldMovement + "?store_id="
		request("GET", detail+created.StoreID, "", "", manager, 200)
		request("GET", detail+otherStore, "", "", second, 404)
		request("GET", detail+branch, "", "", cookie, 404)
		request("GET", endpoint+"?store_id="+branch+"&siguiente="+oldMovement, "", "", cookie, 404)
		request("GET", base, "", "", nil, 401)
		request("GET", base, "", "", second, 404)
		request("GET", endpoint+"?store_id="+branch, "", "", manager, 404)
		request("GET", base+"&ubicacion="+northLocation, "", "", manager, 404)
		for _, criteria := range []string{"q=%FF", "q=%00", "q=EX2&q=EX2", "desde=2026-02-29", "desde=2026-01-02&hasta=2026-01-01", "tipo=inexistente", "variante=no", "destino=oculto", "anterior=" + last + "&siguiente=" + last} {
			request("GET", base+"&"+criteria, "", "", manager, 400)
		}

		read("&referencia="+transferID+"&tipo=transfer_dispatch", manager)
		if len(page.Items) != 1 || !page.Items[0].OtherEndHidden || page.Items[0].Origin == nil || page.Items[0].Destination != nil {
			t.Fatalf("la sucursal no asignada debe ocultarse: %+v", page)
		}
		read("&referencia="+transferID+"&tipo=transfer_dispatch", cookie)
		if len(page.Items) != 1 || page.Items[0].OtherEndHidden || page.Items[0].Destination == nil || page.Items[0].Destination.ID != northLocation {
			t.Fatalf("el titular puede ver los dos extremos autorizados: %+v", page)
		}
		read("&referencia="+partialPurchaseReceiptID+"&tipo=purchase_receipt", manager)
		if len(page.Items) != 1 || !page.Items[0].ProductSnapshot || page.Items[0].Destination == nil {
			t.Fatalf("la recepción debe mostrar el snapshot de compra y su ubicación: %+v", page)
		}
		read("&referencia="+soldReservationID+"&tipo=sale", manager)
		if len(page.Items) != 1 || !page.Items[0].ProductSnapshot || page.Items[0].Origin == nil {
			t.Fatalf("la venta debe conservar los atributos vendidos: %+v", page)
		}
		read("&referencia="+firstReturnID+"&tipo=sale_return", manager)
		if len(page.Items) != 1 || !page.Items[0].ProductSnapshot || page.Items[0].Destination == nil {
			t.Fatalf("devolución histórica: %+v", page)
		}

		execSQL(t, ctx, db, "DELETE FROM role_permissions WHERE role_code='CASHIER' AND permission_code='inventory:read'")
		t.Cleanup(func() {
			execSQL(t, ctx, db, "INSERT INTO role_permissions(role_code,permission_code) VALUES ('CASHIER','inventory:read') ON CONFLICT DO NOTHING")
		})
		request("GET", base, "", "", cashier, 403)
		request("GET", detail+created.StoreID, "", "", cashier, 403)
	})

	t.Run("ajustes y minimos con precondiciones", func(t *testing.T) {
		item := readID(request("POST", productsPath, `{"name":"Prenda de ajuste controlado"}`, origin, cookie, 201))
		v := readID(request("POST", productsPath+"/"+item+"/variants", `{"sku":"E3-AJUSTE"}`, origin, cookie, 201))
		pair := "/api/v1/inventory/existences/" + v + "?store_id=" + created.StoreID
		const unset = "unrecorded"
		type position struct {
			ID                       string
			Revision, MinimumVersion *string
			Minimum, OnHand          *int
		}
		readPair := func() position {
			t.Helper()
			var doc struct{ Locations []position }
			if err := json.Unmarshal(request("GET", pair, "", "", manager, 200).Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			for _, where := range doc.Locations {
				if where.ID == sellable {
					return where
				}
			}
			t.Fatal("ubicación vendible ausente")
			return position{}
		}
		body := func(store, location, variant string, delta int, reason, revision string) string {
			return strings.TrimSuffix(adjustBody(store, location, variant, delta, reason), "}") + fmt.Sprintf(`,"expectedRevision":%q}`, revision)
		}
		run := func(delta int, version, key string, want int) *httptest.ResponseRecorder {
			t.Helper()
			return expect(adjust(body(created.StoreID, sellable, v, delta, "Corrección aprobada", version), key, manager), want)
		}
		before := readPair()
		if before.OnHand != nil || before.Revision != nil || before.Minimum != nil {
			t.Fatalf("sin registros: %+v", before)
		}
		first := run(7, unset, "e3-create-stock", 201)
		firstID := readID(first)
		replay := run(7, unset, "e3-create-stock", 201)
		if first.Body.String() != replay.Body.String() {
			t.Fatal("la clave debe recuperar el movimiento original")
		}
		run(8, unset, "e3-create-stock", 409)
		version := *readPair().Revision
		run(7, version, "e3-create-stock", 409)
		run(-8, version, "e3-insufficient", 409)
		expect(adjust(body(created.StoreID, sellable, v, 2, " ", version), "e3-no-reason", manager), 400)
		expect(adjust(body(created.StoreID, sellable, v, 2, "No autorizado", version), "e3-cashier", cashier), 403)
		expect(adjust(body(created.StoreID, sellable, v, 2, "Otro tenant", version), "e3-tenant", second), 404)
		expect(adjust(body(branch, northLocation, v, 2, "Otra sucursal", version), "e3-branch", manager), 404)
		expect(adjust(body(created.StoreID, northLocation, v, 2, "Ubicación de otra sucursal", version), "e3-location", manager), 404)
		expect(adjust(body(created.StoreID, sellable, otherVariantID, 2, "Variante de otro tenant", version), "e3-variant", manager), 404)
		missing := "00000000-0000-4000-8000-000000000011"
		expect(adjust(body(created.StoreID, missing, v, 2, "Ubicación inexistente", version), "e3-missing-location", manager), 404)
		expect(adjust(body(created.StoreID, sellable, missing, 2, "Variante inexistente", version), "e3-missing-variant", manager), 404)

		const recovery = "/api/v1/inventory/adjustments/result"
		url := recovery + "?store_id=" + created.StoreID + "&key=e3-create-stock"
		if got := request("GET", url, "", "", manager, 200).Body.String(); got != first.Body.String() {
			t.Fatalf("recuperación: %s", got)
		}
		request("GET", url, "", "", cookie, 404)
		request("GET", url, "", "", cashier, 403)
		request("GET", url, "", "", second, 404)
		request("GET", recovery+"?store_id="+created.StoreID+"&key=e3-insufficient", "", "", manager, 404)

		results := make(chan *httptest.ResponseRecorder, 2)
		start := make(chan struct{})
		for _, key := range []string{"e3-race-a", "e3-race-b"} {
			go func() {
				<-start
				results <- adjust(body(created.StoreID, sellable, v, 1, "Carrera de saldo", version), key, manager)
			}()
		}
		close(start)
		a, b := <-results, <-results
		if a.Code == 201 {
			a, b = b, a
		}
		if a.Code != 409 || b.Code != 201 || !strings.Contains(a.Body.String(), "adjustment_stale") {
			t.Fatalf("una sola confirmación sobre la revisión: %d %s y %d %s", a.Code, a.Body, b.Code, b.Body)
		}
		state := readPair()
		if *state.OnHand != 8 {
			t.Fatalf("saldo después de carrera: %+v", state)
		}
		run(-8, *state.Revision, "e3-adjust-zero", 201)
		state = readPair()
		if *state.OnHand != 0 {
			t.Fatal("cero debe quedar registrado")
		}
		execSQL(t, ctx, db, `CREATE FUNCTION test_e3_reject_movement() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN IF NEW.reason='E3 Rechazo transaccional' THEN RAISE EXCEPTION 'fixture rollback E3'; END IF;
			RETURN NEW; END $$`)
		execSQL(t, ctx, db, `CREATE TRIGGER test_e3_reject BEFORE INSERT ON stock_movements
			FOR EACH ROW EXECUTE FUNCTION test_e3_reject_movement()`)
		expect(adjust(body(created.StoreID, sellable, v, 2, "E3 Rechazo transaccional", *state.Revision), "e3-rollback", manager), 500)
		afterFailure := readPair()
		if *afterFailure.OnHand != 0 || *afterFailure.Revision != *state.Revision {
			t.Fatalf("debe revertir la escritura del saldo y su revisión: %+v", afterFailure)
		}
		request("GET", recovery+"?store_id="+created.StoreID+"&key=e3-rollback", "", "", manager, 404)

		var movementCount int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE tenant_id=$1 AND variant_id=$2`, created.TenantID, v).Scan(&movementCount); err != nil || movementCount != 3 {
			t.Fatalf("movimientos parciales o duplicados: %d %v", movementCount, err)
		}
		historical := request("GET", "/api/v1/inventory/movements/history/"+firstID+"?store_id="+created.StoreID, "", "", manager, 200).Body.String()
		if !strings.Contains(historical, `"delta":7`) || !strings.Contains(historical, "Corrección aprobada") {
			t.Fatal(historical)
		}

		minimum := func(value, expected, store, location string, who *http.Cookie, status int) string {
			t.Helper()
			payload := strings.TrimSuffix(minimumBody(store, location, v, value), "}") + fmt.Sprintf(`,"expectedVersion":%q}`, expected)
			return request("PUT", minimumPath, payload, origin, who, status).Body.String()
		}
		minimum("0", unset, created.StoreID, sellable, manager, 204)
		zero := readPair()
		if zero.Minimum == nil || *zero.Minimum != 0 || zero.MinimumVersion == nil {
			t.Fatalf("mínimo cero: %+v", zero)
		}
		if got := minimum("2", unset, created.StoreID, sellable, manager, 409); !strings.Contains(got, "minimum_stale") {
			t.Fatal(got)
		}
		minimum("2", *zero.MinimumVersion, created.StoreID, sellable, manager, 204)
		minimum("null", *zero.MinimumVersion, created.StoreID, sellable, manager, 409)
		minimum("8", unset, branch, northLocation, cookie, 204)
		minimum("1", unset, branch, northLocation, manager, 404)
		minimum("1", unset, created.StoreID, sellable, cashier, 403)
		minimum("1", unset, created.StoreID, sellable, second, 404)
		minimum("1", unset, created.StoreID, northLocation, manager, 404)
		current := readPair()
		if *current.Minimum != 2 || *current.OnHand != 0 {
			t.Fatalf("el mínimo no debe afectar saldo ni otras sucursales: %+v", current)
		}
		minimum("null", *current.MinimumVersion, created.StoreID, sellable, manager, 204)
		if readPair().Minimum != nil {
			t.Fatal("el mínimo debió quedar ausente")
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE tenant_id=$1 AND variant_id=$2`, created.TenantID, v).Scan(&movementCount); err != nil || movementCount != 3 {
			t.Fatalf("cambiar mínimos no debe crear movimientos: %d %v", movementCount, err)
		}
		closed := readID(request("POST", locationsPath, locBody(created.StoreID, "E3-INOP"), origin, cookie, 201))
		execSQL(t, ctx, db, "UPDATE stock_locations SET status='inactive' WHERE id=$1", closed)
		expect(adjust(body(created.StoreID, closed, v, 1, "Ubicación inactiva", unset), "e3-inactive-location", manager), 404)
		catalogPatch(productsPath+"/"+item+"/variants/"+v, `{"status":"inactive"}`, origin, cookie, 200)
		if got := run(1, *state.Revision, "e3-inactive-variant", 409).Body.String(); !strings.Contains(got, "adjustment_inactive") {
			t.Fatal(got)
		}
	})

	t.Run("conteos fisicos con borrador y cierre", func(t *testing.T) {
		const endpoint = "/api/v1/inventory/counts"
		const startKey = "count-e4-main"
		productID := readID(request("POST", productsPath, `{"name":"Campera E4 histórica"}`, origin, cookie, 201))
		variant := func(sku, size string) string {
			return readID(request("POST", productsPath+"/"+productID+"/variants", fmt.Sprintf(`{"sku":%q,"size":%q,"color":"Negro"}`, sku, size), origin, cookie, 201))
		}
		up, down, omitted := variant("E4-S", "S"), variant("E4-M", "M"), variant("E4-L", "L")
		add := func(id string, delta int, key string) {
			expect(adjust(adjustBody(created.StoreID, sellable, id, delta, "Fixture conteo"), key, manager), 201)
		}
		add(up, 4, "e4-count-up")
		add(down, 7, "e4-count-down")
		add(omitted, 12, "e4-count-omitted")
		makeDraft := func(key, observation string) *httptest.ResponseRecorder {
			return expect(keyedCash(endpoint, fmt.Sprintf(`{"storeId":%q,"locationId":%q,"draft":true,"note":%q}`,
				created.StoreID, sellable, observation), key, manager), 201)
		}
		original := makeDraft(startKey, "Inventario E4")
		id := readID(original)
		path := endpoint + "/" + id
		query := "?store_id=" + created.StoreID
		line := func(v, quantity, note string) string {
			return fmt.Sprintf(`{"variantId":%q,"counted":%s,"note":%q}`, v, quantity, note)
		}
		save := func(route string, rev int64, lines string, who *http.Cookie, want int) *httptest.ResponseRecorder {
			return request("PUT", route+"/draft", fmt.Sprintf(`{"storeId":%q,"expectedRevision":%d,"items":[%s]}`, created.StoreID, rev, lines), origin, who, want)
		}
		read := func(route string) *httptest.ResponseRecorder {
			return request("GET", route+query, "", "", manager, 200)
		}
		review := func(route string) (int64, string) {
			var result struct {
				Revision int64
				Items    []struct{ VariantID, StockRevision string }
			}
			if err := json.Unmarshal(read(route+"/review").Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			refs := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				refs = append(refs, fmt.Sprintf(`{"variantId":%q,"stockRevision":%q}`, item.VariantID, item.StockRevision))
			}
			return result.Revision, strings.Join(refs, ",")
		}
		closeBody := func(rev int64, refs string) string {
			return fmt.Sprintf(`{"storeId":%q,"expectedRevision":%d,"references":[%s]}`, created.StoreID, rev, refs)
		}
		physical := func(v string) int {
			t.Helper()
			var units int
			if err := db.QueryRow(ctx, `SELECT quantity_on_hand FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`, created.TenantID, sellable, v).Scan(&units); err != nil {
				t.Fatal(err)
			}
			return units
		}
		moveCount := func(count string) int {
			t.Helper()
			var n int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE tenant_id=$1 AND count_id=$2`, created.TenantID, count).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		save(path, 0, line(up, "-1", ""), manager, 400)
		save(path, 0, line(up, `"0"`, ""), manager, 400)
		save(path, 0, line(up, "2147483648", ""), manager, 400)
		save(path, 0, line(up, "0", "")+","+line(up, "1", ""), manager, 400)
		save(path, 0, line(otherVariantID, "0", ""), manager, 404)
		save(path, 0, line(up, "1", ""), cashier, 403)
		save(path, 0, line(up, "1", ""), second, 404)
		request("GET", path+query, "", "", nil, 401)
		request("GET", path+"?store_id="+otherStore, "", "", second, 404)
		request("GET", path+"?store_id="+branch, "", "", cookie, 404)

		save(path, 0, line(up, "8", "Encontradas")+","+line(down, "2", "")+","+line(omitted, "null", "Sin contar"), manager, 200)
		items := line(up, "9", "Encontradas") + "," + line(down, "0", "Sin unidades") + "," + line(omitted, "null", "Sin contar")
		save(path, 1, items, manager, 200)
		if physical(up) != 4 || physical(down) != 7 || physical(omitted) != 12 || moveCount(id) != 0 {
			t.Fatal("el borrador alteró existencias")
		}
		if got := makeDraft(startKey, "Inventario E4"); readID(got) != id {
			t.Fatal("el reintento de alta debe recuperar la misma sesión")
		}
		if got := save(path, 1, items, manager, 409).Body.String(); !strings.Contains(got, "count_draft_stale") {
			t.Fatal(got)
		}
		history := request("GET", endpoint+"/history"+query+"&status=DRAFT&q=E4-S&location_id="+sellable, "", "", manager, 200).Body.String()
		if !strings.Contains(history, id) || !strings.Contains(history, `"countedCount":2`) {
			t.Fatal(history)
		}
		request("GET", endpoint+"/history"+query+"&location_id="+northLocation, "", "", manager, 404)
		request("GET", endpoint+"/history?store_id="+branch, "", "", manager, 404)

		rev, refs := review(path)
		add(up, 1, "e4-count-external")
		got := expect(keyedCash(path+"/close", closeBody(rev, refs), "count-e4-stale", manager), 409).Body.String()
		if !strings.Contains(got, "count_stale") || !strings.Contains(got, up) {
			t.Fatal(got)
		}
		if moveCount(id) != 0 || !strings.Contains(read(path).Body.String(), `"counted":9`) {
			t.Fatal("el conflicto perdió las cantidades o aplicó un movimiento")
		}
		execSQL(t, ctx, db, "UPDATE stock_balances SET quantity_reserved=1 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
			created.TenantID, sellable, down)
		rev, refs = review(path)
		if reading := read(path + "/review").Body.String(); !strings.Contains(reading, `"blocked":true`) {
			t.Fatal(reading)
		}
		if blocked := expect(keyedCash(path+"/close", closeBody(rev, refs), "count-e4-reserved", manager), 409).Body.String(); !strings.Contains(blocked, "count_reserved") || !strings.Contains(blocked, down) {
			t.Fatal(blocked)
		}
		if moveCount(id) != 0 {
			t.Fatal("el cierre bloqueado por reservas aplicó movimientos")
		}
		execSQL(t, ctx, db, "UPDATE stock_balances SET quantity_reserved=0 WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
			created.TenantID, sellable, down)

		rev, refs = review(path)
		request("GET", path+"/review"+query, "", "", cashier, 403)
		expect(keyedCash(path+"/close", closeBody(rev, refs), "count-e4-denied", cashier), 403)
		expect(keyedCash(path+"/close", closeBody(rev, refs), "count-e4-tenant", second), 404)
		request("POST", path+"/complete", fmt.Sprintf(`{"storeId":%q,"items":[{"variantId":%q,"quantity":9},{"variantId":%q,"quantity":0},{"variantId":%q,"quantity":1}]}`,
			created.StoreID, up, down, omitted), origin, manager, 409)
		const closeKey = "count-e4-close"
		body := closeBody(rev, refs)
		results := make(chan *httptest.ResponseRecorder, 2)
		barrier := make(chan struct{})
		for _, key := range []string{closeKey, "count-e4-double"} {
			go func() { <-barrier; results <- keyedCash(path+"/close", body, key, manager) }()
		}
		close(barrier)
		a, b := <-results, <-results
		if a.Code == 409 {
			a, b = b, a
		}
		if a.Code != 200 || b.Code != 409 {
			t.Fatalf("cierre concurrente: %d %d", a.Code, b.Code)
		}
		winningKey := closeKey
		lookup := httptest.NewRequest("GET", path+"/close-result"+query+"&key="+closeKey, nil)
		lookup.AddCookie(manager)
		check := httptest.NewRecorder()
		router.ServeHTTP(check, lookup)
		if check.Code == 404 {
			winningKey = "count-e4-double"
		} else if check.Code != 200 {
			t.Fatal(check.Body)
		}
		recovered := request("GET", path+"/close-result"+query+"&key="+winningKey, "", "", manager, 200)
		if a.Body.String() != recovered.Body.String() || a.Body.String() != expect(keyedCash(path+"/close", body, winningKey, manager), 200).Body.String() {
			t.Fatal("la recuperación o repetición no devolvió el cierre original")
		}
		request("GET", path+"/close-result"+query+"&key="+winningKey, "", "", cookie, 404)
		expect(keyedCash(path+"/close", closeBody(rev+1, refs), winningKey, manager), 409)
		if physical(up) != 9 || physical(down) != 0 || physical(omitted) != 12 || moveCount(id) != 2 {
			t.Fatal("cierre parcial incorrecto o duplicado")
		}
		log := request("GET", "/api/v1/inventory/movements/history"+query+"&tipo=count_adjustment&referencia="+id, "", "", manager, 200).Body.String()
		if !strings.Contains(log, `"delta":4`) || !strings.Contains(log, `"delta":-7`) || !strings.Contains(log, "Campera E4 histórica") {
			t.Fatal(log)
		}
		catalogPatch(productsPath+"/"+productID, `{"name":"Campera E4 renombrada"}`, origin, cookie, 200)
		if result := read(path).Body.String(); !strings.Contains(result, "Campera E4 histórica") || strings.Contains(result, "Campera E4 renombrada") {
			t.Fatal(result)
		}

		unchanged := readID(makeDraft("count-e4-unchanged", "Sin diferencia"))
		noDelta := endpoint + "/" + unchanged
		save(noDelta, 0, line(up, "9", "")+","+line(omitted, "null", ""), manager, 200)
		if trimmed := save(noDelta, 1, line(up, "9", ""), manager, 200).Body.String(); !strings.Contains(trimmed, `"lineCount":1`) || strings.Contains(trimmed, omitted) {
			t.Fatalf("la línea omitida en el guardado debe quitarse del borrador: %s", trimmed)
		}
		rev, refs = review(noDelta)
		expect(keyedCash(noDelta+"/close", closeBody(rev, refs), "count-e4-close-unchanged", manager), 200)
		if moveCount(unchanged) != 0 {
			t.Fatal("un cierre sin diferencias inventó movimientos")
		}

		rollback := readID(makeDraft("count-e4-rollback", "Prueba atómica"))
		failPath := endpoint + "/" + rollback
		target := down
		if up > down {
			target = up
		}
		failNote := "Fallar segunda línea E4"
		firstNote, secondNote := "", failNote
		if target == up {
			firstNote, secondNote = failNote, ""
		}
		save(failPath, 0, line(up, "10", firstNote)+","+line(down, "1", secondNote), manager, 200)
		execSQL(t, ctx, db, `CREATE FUNCTION e4_reject_count_line() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN IF NEW.reason='Fallar segunda línea E4' THEN RAISE EXCEPTION 'fixture rollback E4'; END IF; RETURN NEW; END $$`)
		execSQL(t, ctx, db, `CREATE TRIGGER e4_reject_count_line BEFORE INSERT ON stock_movements FOR EACH ROW EXECUTE FUNCTION e4_reject_count_line()`)
		rev, refs = review(failPath)
		expect(keyedCash(failPath+"/close", closeBody(rev, refs), "count-e4-close-rollback", manager), 500)
		if moveCount(rollback) != 0 || physical(up) != 9 || physical(down) != 0 || !strings.Contains(read(failPath).Body.String(), `"status":"DRAFT"`) {
			t.Fatal("el cierre fallido dejó cambios parciales")
		}
		request("GET", failPath+"/close-result"+query+"&key=count-e4-close-rollback", "", "", manager, 404)
		execSQL(t, ctx, db, "DELETE FROM role_permissions WHERE role_code='CASHIER' AND permission_code='inventory:read'")
		t.Cleanup(func() {
			execSQL(t, ctx, db, "INSERT INTO role_permissions(role_code,permission_code) VALUES ('CASHIER','inventory:read') ON CONFLICT DO NOTHING")
		})
		request("GET", path+query, "", "", cashier, 403)
		request("GET", endpoint+"/history"+query, "", "", cashier, 403)

	})

	execSQL(t, ctx, db, `DELETE FROM user_stores WHERE tenant_id=$1 AND user_id=(
		SELECT id FROM users WHERE tenant_id=$1 AND email='manager@example.test'
	)`, created.TenantID)
	request("GET", suppliersPath, "", "", manager, 403)

	t.Run("E4 maestros y asociaciones", func(t *testing.T) {
		const sizePath = "/api/v1/catalog/sizes"
		const colorPath = "/api/v1/catalog/colors"
		const groupPath = "/api/v1/catalog/size-groups"
		group := readID(request("POST", groupPath, `{"name":"Adulto","position":20}`, origin, cookie, 201))
		size := readID(request("POST", sizePath, `{"name":"M","code":" MED ","groupId":"`+group+`","position":1}`, origin, cookie, 201))
		color := readID(request("POST", colorPath, `{"name":"Azul","hex":"#003F72"}`, origin, cookie, 201))
		request("GET", sizePath, "", "", nil, 401)
		request("POST", sizePath, `{"name":"XL"}`, origin, cashier, 403)
		request("GET", "/api/v1/catalog/pending-attributes", "", "", cashier, 403)
		if got := request("GET", sizePath, "", "", cashier, 200).Body.String(); !strings.Contains(got, `"code":"med"`) {
			t.Fatal(got)
		}
		if got := request("GET", colorPath, "", "", cashier, 200).Body.String(); !strings.Contains(got, `"hex":"#003f72"`) || !strings.Contains(got, `"code":"azul"`) {
			t.Fatal(got)
		}
		if got := request("GET", sizePath, "", "", second, 200).Body.String(); strings.Contains(got, size) {
			t.Fatal(got)
		}
		request("POST", sizePath, `{"name":" m "}`, origin, cookie, 409)
		request("POST", sizePath, `{"name":"Otro","code":"MED"}`, origin, cookie, 409)
		request("POST", groupPath, `{"name":"adulto"}`, origin, second, 201)
		request("PATCH", sizePath+"/"+size, `{"name":"Ajeno","status":"active","expectedRevision":1}`, origin, second, 404)
		request("POST", sizePath, `{"name":"Cruce","groupId":"`+color+`"}`, origin, cookie, 404)
		request("PATCH", groupPath+"/"+group, `{"name":"Adulto","status":"inactive","expectedRevision":1}`, origin, cookie, 200)
		request("POST", sizePath, `{"name":"XL","groupId":"`+group+`"}`, origin, cookie, 409)
		request("PATCH", sizePath+"/"+size, `{"name":"M","code":"med","position":2,"status":"active","groupId":"`+group+`","expectedRevision":1}`, origin, cookie, 200)
		request("PATCH", sizePath+"/"+size, `{"name":"M","status":"active","expectedRevision":1}`, origin, cookie, 409)
		request("PATCH", groupPath+"/"+group, `{"name":"Adulto","status":"active","expectedRevision":2}`, origin, cookie, 200)

		asynchronous := func(method, path, body string, start <-chan struct{}, done chan<- *httptest.ResponseRecorder) {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			req.AddCookie(cookie)
			response := httptest.NewRecorder()
			<-start
			router.ServeHTTP(response, req)
			done <- response
		}
		start := make(chan struct{})
		results := make(chan *httptest.ResponseRecorder, 2)
		go asynchronous("POST", sizePath, `{"name":"XL"}`, start, results)
		go asynchronous("POST", sizePath, `{"name":" xl "}`, start, results)
		close(start)
		createdSize, duplicate := <-results, <-results
		if createdSize.Code == 409 {
			createdSize, duplicate = duplicate, createdSize
		}
		expect(createdSize, 201)
		if duplicate.Code != 409 || !strings.Contains(duplicate.Body.String(), "attribute_duplicate") {
			t.Fatal(duplicate)
		}
		concurrentSize := readID(createdSize)
		start = make(chan struct{})
		go asynchronous("PATCH", sizePath+"/"+concurrentSize, `{"name":"XL A","status":"active","expectedRevision":1}`, start, results)
		go asynchronous("PATCH", sizePath+"/"+concurrentSize, `{"name":"XL B","status":"active","expectedRevision":1}`, start, results)
		close(start)
		savedSize, stale := <-results, <-results
		if savedSize.Code == 409 {
			savedSize, stale = stale, savedSize
		}
		expect(savedSize, 200)
		if stale.Code != 409 || !strings.Contains(stale.Body.String(), "revision_conflict") {
			t.Fatal(stale)
		}

		parent := productsPath + "/" + readID(request("POST", productsPath, `{"name":"E4 producto"}`, origin, cookie, 201))
		legacyID := readID(request("POST", parent+"/variants", `{"sku":"E4-LEGACY","size":"M","color":"Azul","barcodes":["000E4"]}`, origin, cookie, 201))
		editPath := parent + "/variants/" + legacyID
		pendingPath := "/api/v1/catalog/pending-attributes?q=E4-LEGACY"
		if got := request("GET", pendingPath, "", "", cookie, 200).Body.String(); !strings.Contains(got, legacyID) || !strings.Contains(got, `"productName":"E4 producto"`) {
			t.Fatal(got)
		}
		lookup := "/api/v1/barcodes/000E4/lookup"
		patch := func(input string, want int) string {
			return request("PATCH", editPath, input, origin, cookie, want).Body.String()
		}
		seedStock := adjustBody(created.StoreID, sellable, legacyID, 3, "Stock de fixture E4")
		expect(adjust(seedStock, "e4-stock", cookie), 201)
		res := readID(expect(keyedCash(reservePath, strings.ReplaceAll(reserveBody(customerID, "1"), variantID, legacyID), "e4-reserva", cashier), 201))
		patch(`{"expectedRevision":1,"sizeId":"`+size+`","size":"M","colorId":"`+color+`","color":"Azul"}`, 200)
		if got := request("GET", pendingPath, "", "", cookie, 200).Body.String(); strings.Contains(got, legacyID) {
			t.Fatal(got)
		}
		if got := request("GET", lookup, "", "", cashier, 200).Body.String(); !strings.Contains(got, size) || !strings.Contains(got, color) {
			t.Fatal(got)
		}
		request("POST", parent+"/variants", `{"sku":"E4-DUPLICADA","sizeId":"`+size+`","colorId":"`+color+`"}`, origin, cookie, 409)
		request("PATCH", sizePath+"/"+size, `{"name":"Mediano","status":"active","expectedRevision":2}`, origin, cookie, 200)
		if got := request("GET", lookup, "", "", cashier, 200).Body.String(); !strings.Contains(got, `"size":"M"`) || strings.Contains(got, "Mediano") {
			t.Fatal(got)
		}
		if got := patch(`{"expectedRevision":2,"sizeId":"`+size+`","size":"M"}`, 409); !strings.Contains(got, "attribute_changed") {
			t.Fatal(got)
		}
		if got := patch(`{"expectedRevision":2,"sizeId":"`+size+`","size":"Mediano","reason":"Misma prenda","confirmSensitive":true}`, 409); !strings.Contains(got, "sensitive_history") {
			t.Fatal(got)
		}
		if got := patch(`{"expectedRevision":2,"colorId":null,"reason":"Misma prenda","confirmSensitive":true}`, 409); !strings.Contains(got, "sensitive_history") {
			t.Fatal(got)
		}
		request("POST", reservePath+"/"+res+"/cancel", storeInput, origin, cashier, 200)
		revised := patch(`{"expectedRevision":2,"sizeId":"`+size+`","size":"Mediano","reason":"Misma prenda","confirmSensitive":true}`, 200)
		if !strings.Contains(revised, `"revision":3`) || !strings.Contains(revised, `"size":"Mediano"`) {
			t.Fatal(revised)
		}
		var historic, before, after string
		var events int
		if err := db.QueryRow(ctx, `SELECT i.size, (SELECT count(*) FROM catalog_sensitive_events e WHERE (e.tenant_id,e.variant_id)=(i.tenant_id,i.variant_id)),
				e.before_data->>'size',e.after_data->>'size' FROM reservation_items i
				JOIN catalog_sensitive_events e ON (e.tenant_id,e.variant_id)=(i.tenant_id,i.variant_id)
				WHERE i.tenant_id=$1 AND i.reservation_id=$2 AND i.variant_id=$3`, created.TenantID, res, legacyID).Scan(&historic, &events, &before, &after); err != nil || historic != "M" || events != 1 || before != "M" || after != "Mediano" {
			t.Fatalf("snapshot/auditoría: %q %d %q %q %v", historic, events, before, after, err)
		}
		request("PATCH", colorPath+"/"+color, `{"name":"Azul","status":"inactive","expectedRevision":1}`, origin, cookie, 200)
		request("POST", parent+"/variants", `{"sku":"E4-INACTIVO","colorId":"`+color+`"}`, origin, cookie, 409)
		patch(`{"expectedRevision":3,"status":"inactive"}`, 200)
		if got := request("GET", parent, "", "", cookie, 200).Body.String(); !strings.Contains(got, color) {
			t.Fatal(got)
		}
		request("POST", parent+"/variants", `{"sku":"E4-VACIA"}`, origin, cookie, 201)
	})

	t.Run("T03 historial y carreras", func(t *testing.T) {
		newVariant := func(sku string) (string, string) {
			parent := productsPath + "/" + readID(request("POST", productsPath, `{"name":"Prueba T03"}`, origin, cookie, 201))
			id := readID(request("POST", parent+"/variants", fmt.Sprintf(`{"sku":%q,"size":"M","color":"Azul","barcodes":[%q]}`, sku, sku+"-1"), origin, cookie, 201))
			return id, parent + "/variants/" + id
		}
		patch := func(path, sku string, revision int, actor *http.Cookie) *httptest.ResponseRecorder {
			body := fmt.Sprintf(`{"expectedRevision":%d,"sku":%q,"size":"L","color":"Rojo","barcodes":[%q],"reason":"Identificación de la misma prenda","confirmSensitive":true}`,
				revision, sku, sku+"-1")
			req := httptest.NewRequest("PATCH", path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			req.AddCookie(actor)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)
			return result
		}
		seed := func(id, key string) {
			expect(adjust(adjustBody(created.StoreID, sellable, id, 2, "Stock de prueba T03"), key, cookie), 201)
		}
		stock := func(id string) int32 {
			var held int32
			if err := db.QueryRow(ctx, "SELECT quantity_reserved FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
				created.TenantID, sellable, id).Scan(&held); err != nil {
				t.Fatal(err)
			}
			return held
		}
		checkVariant := func(id, wantSKU string, wantRevision, wantEvents int) {
			t.Helper()
			var sku, size, color, barcode string
			var revision, productRevision, events int
			if err := db.QueryRow(ctx, `SELECT v.sku,v.size,v.color,v.revision,p.revision,b.code,
				(SELECT count(*) FROM catalog_sensitive_events e WHERE (e.tenant_id,e.variant_id)=(v.tenant_id,v.id))
				FROM product_variants v JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				JOIN barcodes b ON (b.tenant_id,b.variant_id)=(v.tenant_id,v.id) AND b.is_primary
				WHERE v.tenant_id=$1 AND v.id=$2`, created.TenantID, id).Scan(&sku, &size, &color, &revision, &productRevision, &barcode, &events); err != nil {
				t.Fatal(err)
			}
			wantSize, wantColor := "M", "Azul"
			if wantEvents > 0 {
				wantSize, wantColor = "L", "Rojo"
			}
			if sku != wantSKU || size != wantSize || color != wantColor || revision != wantRevision || productRevision != 1 || barcode != wantSKU+"-1" || events != wantEvents {
				t.Fatalf("variante: %s %s %s rev=%d padre=%d código=%s eventos=%d", sku, size, color, revision, productRevision, barcode, events)
			}
		}
		assertBlocked := func(res *httptest.ResponseRecorder) {
			t.Helper()
			if res.Code != 409 || !strings.Contains(res.Body.String(), "sensitive_history") {
				t.Fatalf("se esperaba sensitive_history: %d %s", res.Code, res.Body)
			}
		}
		for _, operationFirst := range []bool{true, false} {
			name := "correccion-primero"
			if operationFirst {
				name = "reserva-primero"
			}
			t.Run(name, func(t *testing.T) {
				oldSKU := "T03-" + name
				newSKU := oldSKU + "-edit"
				id, path := newVariant(oldSKU)
				seed(id, "stock-"+name)
				if stock(id) != 0 {
					t.Fatal("reserva inesperada")
				}
				reserve := func() *httptest.ResponseRecorder {
					return keyedCash(reservePath, strings.ReplaceAll(reserveBody(customerID, "1"), variantID, id), "reservation-"+name, cashier)
				}
				correct := func() *httptest.ResponseRecorder { return patch(path, newSKU, 1, cookie) }
				var held, edit *httptest.ResponseRecorder
				expectedSnapshot := oldSKU
				expectedSize, expectedColor := "M", "Azul"
				if operationFirst {
					held, edit = runCatalogRace(t, db, "reservation_items", reserve, correct)
					assertBlocked(edit)
					checkVariant(id, oldSKU, 1, 0)
				} else {
					edit, held = runCatalogRace(t, db, "catalog_sensitive_events", correct, reserve)
					expect(edit, 200)
					checkVariant(id, newSKU, 2, 1)
					expectedSnapshot, expectedSize, expectedColor = newSKU, "L", "Rojo"
				}
				expect(held, 201)
				if stock(id) != 1 {
					t.Fatal("la operación no reservó exactamente una unidad")
				}
				reservationID := readID(held)
				var sku, size, color, linked string
				snapshot := func() {
					t.Helper()
					err := db.QueryRow(ctx, `SELECT sku,size,color,variant_id FROM reservation_items WHERE tenant_id=$1 AND reservation_id=$2`,
						created.TenantID, reservationID).Scan(&sku, &size, &color, &linked)
					if err != nil || sku != expectedSnapshot || size != expectedSize || color != expectedColor || linked != id {
						t.Fatalf("snapshot de reserva: %q %q %q %s (%v)", sku, size, color, linked, err)
					}
				}
				snapshot()
				if operationFirst {
					expect(patch(path, newSKU, 1, cashier), 403)
					request("POST", reservePath+"/"+reservationID+"/cancel", storeInput, origin, cashier, 200)
					expect(patch(path, newSKU, 1, cookie), 200)
					checkVariant(id, newSKU, 2, 1)
					snapshot()
					expect(patch(path, oldSKU, 1, cookie), 409)
					var audited bool
					if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_sensitive_events
						WHERE tenant_id=$1 AND variant_id=$2 AND actor_id=$3 AND action='correct_variant'
						AND reason='Identificación de la misma prenda' AND before_data->>'sku'=$4 AND after_data->>'sku'=$5
						AND before_data->'barcodes' = jsonb_build_array($4::text || '-1')
						AND after_data->'barcodes' = jsonb_build_array($5::text || '-1')
						AND created_at IS NOT NULL)`, created.TenantID, id, created.OwnerID, oldSKU, newSKU).Scan(&audited); err != nil || !audited {
						t.Fatalf("faltan valores históricos en auditoría: %v", err)
					}
				}
			})
		}

		t.Run("compra parcial bloquea y compra recibida conserva su línea", func(t *testing.T) {
			id, path := newVariant("T03-COMPRA")
			ordered, rejected := runCatalogRace(t, db, "purchase_order_items", func() *httptest.ResponseRecorder {
				return keyedCash(purchasesPath, purchaseInput(created.StoreID, homonymID, "["+purchaseLine(id, 2, "null")+"]"), "t03-compra", cookie)
			}, func() *httptest.ResponseRecorder { return patch(path, "T03-COMPRA-EDIT", 1, cookie) })
			order := readID(expect(ordered, 201))
			assertBlocked(rejected)
			var item string
			if err := db.QueryRow(ctx, "SELECT id FROM purchase_order_items WHERE tenant_id=$1 AND purchase_order_id=$2 AND variant_id=$3",
				created.TenantID, order, id).Scan(&item); err != nil {
				t.Fatal(err)
			}
			assertBlocked(patch(path, "T03-COMPRA-EDIT", 1, cookie))
			for _, key := range []string{"t03-receive-partial", "t03-receive-complete"} {
				expect(keyedCash(purchasesPath+"/"+order+"/receipts", receiptInput(sellable, "", receiptLine(item, 1, 0)), key, cookie), 201)
				if key == "t03-receive-partial" {
					assertBlocked(patch(path, "T03-COMPRA-EDIT", 1, cookie))
					checkVariant(id, "T03-COMPRA", 1, 0)
				}
			}
			expect(patch(path, "T03-COMPRA-EDIT", 1, cookie), 200)
			checkVariant(id, "T03-COMPRA-EDIT", 2, 1)
			var sku string
			if err := db.QueryRow(ctx, "SELECT sku FROM purchase_order_items WHERE id=$1 AND variant_id=$2", item, id).Scan(&sku); err != nil || sku != "T03-COMPRA" {
				t.Fatalf("snapshot de orden: %q %v", sku, err)
			}
		})

		t.Run("conteo y transferencia en borrador", func(t *testing.T) {
			id, path := newVariant("T03-INVENTARIO")
			count := readID(startCount("t03-count", location, []string{id}, cookie, 201))
			assertBlocked(patch(path, "T03-INVENTARIO-EDIT", 1, cookie))
			transfer := openTransfer(transferDraft(salesFloor, transferItem(id, 1)), "t03-transfer", cookie, 201)
			assertBlocked(patch(path, "T03-INVENTARIO-EDIT", 1, cookie))
			request("POST", countsPath+"/"+count+"/cancel", storeInput, origin, cookie, 200)
			assertBlocked(patch(path, "T03-INVENTARIO-EDIT", 1, cookie))
			checkVariant(id, "T03-INVENTARIO", 1, 0)
			request("POST", transfersPath+"/"+transfer+"/cancel", storeInput, origin, cookie, 200)
			expect(patch(path, "T03-INVENTARIO-EDIT", 1, cookie), 200)
			checkVariant(id, "T03-INVENTARIO-EDIT", 2, 1)
			var stocked, transferred string
			if err := db.QueryRow(ctx, `SELECT c.sku_snapshot,t.sku_snapshot FROM stock_count_items c
				JOIN stock_transfer_items t ON (t.tenant_id,t.variant_id)=(c.tenant_id,c.variant_id)
				WHERE c.tenant_id=$1 AND c.count_id=$2 AND t.transfer_id=$3 AND c.variant_id=$4`,
				created.TenantID, count, transfer, id).Scan(&stocked, &transferred); err != nil || stocked != "T03-INVENTARIO" || transferred != stocked {
				t.Fatalf("snapshots de inventario: %q %q (%v)", stocked, transferred, err)
			}
		})

		t.Run("correccion primero y snapshot nuevo de inventario", func(t *testing.T) {
			id, path := newVariant("T03-CONTEO-NUEVO")
			body := fmt.Sprintf(`{"storeId":%q,"locationId":%q,"variantIds":[%q]}`, created.StoreID, location, id)
			updated, count := runCatalogRace(t, db, "catalog_sensitive_events",
				func() *httptest.ResponseRecorder { return patch(path, "T03-CONTEO-NUEVO-EDIT", 1, cookie) },
				func() *httptest.ResponseRecorder { return keyedCash(countsPath, body, "t03-conteo-nuevo", cookie) })
			expect(updated, 200)
			countID := readID(expect(count, 201))
			var sku string
			if err := db.QueryRow(ctx, "SELECT sku_snapshot FROM stock_count_items WHERE tenant_id=$1 AND count_id=$2 AND variant_id=$3",
				created.TenantID, countID, id).Scan(&sku); err != nil || sku != "T03-CONTEO-NUEVO-EDIT" {
				t.Fatalf("snapshot posterior a la corrección: %q (%v)", sku, err)
			}
			assertBlocked(patch(path, "T03-CONTEO-NUEVO-FINAL", 2, cookie))
			checkVariant(id, "T03-CONTEO-NUEVO-EDIT", 2, 1)
		})

		t.Run("venta confirmada y devolucion abierta o liquidada", func(t *testing.T) {
			id, path := newVariant("T03-VENTA")
			seed(id, "t03-stock-venta")
			request("PUT", itemPath(general, id), `{"amount":"10.00"}`, origin, cookie, 200)
			register := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-T03"), origin, cookie, 201))
			session := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, register, `"0"`), "t03-open", cookie), 201))
			sale := readID(expect(keyedCash(salesPath, saleBody(session, sellable, "10", line(id, 1, "10")), "t03-venta", cookie), 201))
			expect(patch(path, "T03-VENTA-EDIT", 1, cookie), 200)
			ref := readID(expect(keyedCash(returnPath(sale), returnBody("Recepción de prueba T03", lineReturn(sellable, id, "sellable", 1)), "t03-return", cookie), 201))
			assertBlocked(patch(path, "T03-VENTA-FINAL", 2, cookie))
			checkVariant(id, "T03-VENTA-EDIT", 2, 1)
			expect(keyedCash(refundPath(sale, ref), refundBody(session), "t03-refund", cookie), 201)
			expect(patch(path, "T03-VENTA-FINAL", 2, cookie), 200)
			checkVariant(id, "T03-VENTA-FINAL", 3, 2)
			var sku, size, color, linked, returnedSKU string
			if err := db.QueryRow(ctx, `SELECT i.sku,i.size,i.color,i.variant_id,m.sku_snapshot FROM sale_items i
				JOIN stock_movements m ON (m.tenant_id,m.sale_id,m.variant_id)=(i.tenant_id,i.sale_id,i.variant_id)
				WHERE i.tenant_id=$1 AND i.sale_id=$2 AND m.return_id=$3`, created.TenantID, sale, ref).Scan(&sku, &size, &color, &linked, &returnedSKU); err != nil || sku != "T03-VENTA" || size != "M" || color != "Azul" || linked != id || returnedSKU != sku {
				t.Fatalf("snapshots de venta y devolución: %q %q %q %s %q (%v)", sku, size, color, linked, returnedSKU, err)
			}
		})
		t.Run("reservas vencidas y senas sin liquidar", func(t *testing.T) {
			id, path := newVariant("T03-SENA")
			seed(id, "t03-sena-stock")
			body := strings.ReplaceAll(reserveBody(customerID, "1"), variantID, id)
			due := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			timed := strings.Replace(body, `"items":`, fmt.Sprintf(`"expiresAt":%q,"items":`, due), 1)
			expired := readID(expect(keyedCash(reservePath, timed, "t03-sena-expire", cookie), 201))
			execSQL(t, ctx, db, "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", created.TenantID, expired)
			assertBlocked(patch(path, "T03-SENA-EDIT", 1, cookie))
			request("POST", reservePath+"/"+expired+"/expire", storeInput, origin, cookie, 200)
			expect(patch(path, "T03-SENA-EDIT", 1, cookie), 200)
			register := readID(request("POST", registersPath, registerBody(created.StoreID, "CAJA-T03-SENA"), origin, cookie, 201))
			session := readID(expect(keyedCash(sessionsPath, openBody(created.StoreID, register, `"0"`), "t03-sena-caja", cookie), 201))
			res := readID(expect(keyedCash(reservePath, body, "t03-sena-reserva", cookie), 201))
			expect(keyedCash(reservePath+"/"+res+"/deposit", collectionBody(session, "transfer", "5"), "t03-sena-payment", cookie), 201)
			request("POST", reservePath+"/"+res+"/cancel", storeInput, origin, cookie, 200)
			if stock(id) != 0 {
				t.Fatal("la reserva cancelada debe haber liberado el saldo")
			}
			assertBlocked(patch(path, "T03-SENA-FINAL", 2, cookie))
			checkVariant(id, "T03-SENA-EDIT", 2, 1)
		})

	})

	t.Run("E5 numeracion idempotencia e historial", func(t *testing.T) {
		const settingsPath = "/api/v1/catalog/identifier-settings"
		const suggestionPath = "/api/v1/catalog/sku-suggestion"
		const identifierHistory = "/api/v1/catalog/identifier-history"
		request("GET", suggestionPath, "", "", cashier, 403)
		request("GET", suggestionPath+"?after=-1", "", "", cookie, 400)
		request("PATCH", settingsPath, `{"skuPrefix":"*-","skuWidth":6,"expectedRevision":1}`, origin, cookie, 400)
		request("PATCH", settingsPath, `{"skuPrefix":"E5-","skuWidth":6,"expectedRevision":1}`, origin, cookie, 200)
		request("PATCH", settingsPath, `{"skuPrefix":"OTRO-","skuWidth":6,"expectedRevision":1}`, origin, cookie, 409)
		if got := request("GET", suggestionPath, "", "", second, 200).Body.String(); strings.Contains(got, "E5-") {
			t.Fatal("la configuración cruzó de comercio", got)
		}
		parent := productsPath + "/" + readID(request("POST", productsPath, `{"name":"E5 producto"}`, origin, cookie, 201))
		send := func(method, path, body, key string, actor *http.Cookie) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			if key != "" {
				req.Header.Set("Idempotency-Key", key)
			}
			req.AddCookie(actor)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)
			return result
		}
		concurrent := func(a, b func() *httptest.ResponseRecorder) (*httptest.ResponseRecorder, *httptest.ResponseRecorder) {
			start := make(chan struct{})
			one, two := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() { <-start; one <- a() }()
			go func() { <-start; two <- b() }()
			close(start)
			return <-one, <-two
		}
		var editorID string
		if err := db.QueryRow(ctx, `INSERT INTO users(tenant_id,email,name,password_hash)
			SELECT tenant_id,'e5-editor@example.test','Segundo titular',password_hash FROM users WHERE tenant_id=$1 AND id=$2 RETURNING id`,
			created.TenantID, created.OwnerID).Scan(&editorID); err != nil {
			t.Fatal(err)
		}
		execSQL(t, ctx, db, `INSERT INTO user_roles(tenant_id,user_id,role_code) VALUES ($1,$2,'OWNER')`, created.TenantID, editorID)
		otherEditor := loginAs("e5-editor@example.test", password)
		a, b := concurrent(
			func() *httptest.ResponseRecorder { return send("GET", suggestionPath, "", "", cookie) },
			func() *httptest.ResponseRecorder { return send("GET", suggestionPath, "", "", otherEditor) },
		)
		if a.Code != 200 || b.Code != 200 || a.Body.String() != b.Body.String() || !strings.Contains(a.Body.String(), "E5-000001") {
			t.Fatalf("sugerencias simultáneas: %d %s / %d %s", a.Code, a.Body, b.Code, b.Body)
		}
		if got := request("GET", suggestionPath+"?after=1", "", "", cookie, 200).Body.String(); !strings.Contains(got, "E5-000002") {
			t.Fatal(got)
		}
		// A historical identifier under another format must be skipped even
		// if its number has never advanced the current sequence.
		archiveParent := productsPath + "/" + readID(request("POST", productsPath, `{"name":"Archivo E5"}`, origin, cookie, 201))
		archived := readID(request("POST", archiveParent+"/variants", `{"sku":"FUT-000003"}`, origin, cookie, 201))
		request("PATCH", archiveParent+"/variants/"+archived, `{"expectedRevision":1,"sku":"ARCHIVADO-E5","reason":"Misma prenda","confirmSensitive":true}`, origin, cookie, 200)
		expect(send("PATCH", archiveParent+"/variants/"+archived, `{"expectedRevision":2,"sku":"FUT-000003","reuseRetired":true,"reason":"Prueba","confirmSensitive":true}`, "", cashier), 403)
		if got := request("GET", identifierHistory+"?kind=sku&value=fut-000003", "", "", cookie, 200).Body.String(); !strings.Contains(got, archived) {
			t.Fatal(got)
		}
		input := `{"sku":"E5-000001","barcodes":["EXT-E5"],"generateInternal":true}`
		create := func(body, key string) *httptest.ResponseRecorder {
			return send("POST", parent+"/variants", body, key, cookie)
		}
		a, b = concurrent(func() *httptest.ResponseRecorder { return create(input, "e5-original-a") }, func() *httptest.ResponseRecorder {
			return send("POST", parent+"/variants", input, "e5-original-b", otherEditor)
		})
		winningKey, winningActor, losingActor := "e5-original-a", cookie, otherEditor
		if a.Code == 409 {
			a, b = b, a
			winningKey, winningActor, losingActor = "e5-original-b", otherEditor, cookie
		}
		original := readID(expect(a, 201))
		if b.Code != 409 || !strings.Contains(b.Body.String(), "sku_taken") {
			t.Fatalf("una sola alta debe adquirir el SKU: %s", b.Body)
		}
		replay := expect(send("POST", parent+"/variants", input, winningKey, winningActor), 201)
		if replay.Body.String() != a.Body.String() {
			t.Fatalf("el reintento debe recuperar la misma variante/código: %s / %s", a.Body, replay.Body)
		}
		if got := send("POST", parent+"/variants", `{"sku":"E5-DISTINTO"}`, winningKey, winningActor); got.Code != 409 || !strings.Contains(got.Body.String(), "idempotency_conflict") {
			t.Fatal(got)
		}
		checkRefundCode(send("POST", parent+"/variants", input, winningKey, losingActor), 409, "idempotency_conflict")
		a, b = concurrent(func() *httptest.ResponseRecorder {
			return create(`{"sku":"E5-BAR-A","generateInternal":true}`, "e5-bar-a")
		}, func() *httptest.ResponseRecorder {
			return create(`{"sku":"E5-BAR-B","generateInternal":true}`, "e5-bar-b")
		})
		expect(a, 201)
		expect(b, 201)
		parseInternal := func(r *httptest.ResponseRecorder) string {
			var entry struct {
				Internal string `json:"internalBarcode"`
			}
			if err := json.Unmarshal(r.Body.Bytes(), &entry); err != nil || !regexp.MustCompile(`^2\d{15}$`).MatchString(entry.Internal) {
				t.Fatalf("código inesperado: %s (%v)", r.Body, err)
			}
			return entry.Internal
		}
		if parseInternal(a) == parseInternal(b) {
			t.Fatal("la secuencia entregó dos códigos iguales")
		}
		originalCode := parseInternal(replay)
		if !strings.Contains(replay.Body.String(), `"barcodes":["EXT-E5",`) {
			t.Fatal(replay.Body)
		}
		request("GET", "/api/v1/barcodes/"+originalCode+"/lookup", "", "", cookie, 200)
		request("GET", "/api/v1/barcodes/"+originalCode+"/lookup", "", "", second, 404)
		p := parent + "/variants/" + original
		patch := func(path, body string) *httptest.ResponseRecorder { return send("PATCH", path, body, "", cookie) }
		confirm := `,"reason":"Identificación de la misma prenda","confirmSensitive":true}`
		originalChange := `{"expectedRevision":1,"sku":"E5-MANUAL","barcodes":["` + originalCode + `"]` + confirm
		expect(adjust(adjustBody(created.StoreID, sellable, original, 4, "Stock para E5"), "e5-stock", cookie), 201)
		makeReservation := func(id, key string) *httptest.ResponseRecorder {
			return keyedCash(reservePath, strings.ReplaceAll(reserveBody(customerID, "1"), variantID, id), key, cashier)
		}
		reservation := readID(expect(makeReservation(original, "e5-original-reserva"), 201))
		checkRefundCode(patch(p, originalChange), 409, "sensitive_history")
		request("POST", reservePath+"/"+reservation+"/cancel", storeInput, origin, cashier, 200)
		expect(patch(p, originalChange), 200)
		checkRefundCode(patch(p, originalChange), 409, "revision_conflict")
		request("GET", "/api/v1/barcodes/EXT-E5/lookup", "", "", cookie, 404)
		if got := request("GET", identifierHistory+"?kind=barcode&value=EXT-E5", "", "", cookie, 200).Body.String(); !strings.Contains(got, original) || !strings.Contains(got, created.OwnerID) || !strings.Contains(got, "retired") {
			t.Fatal(got)
		}
		request("GET", identifierHistory+"?kind=barcode&value=EXT-E5", "", "", cashier, 403)
		if got := request("GET", identifierHistory+"?kind=barcode&value=EXT-E5", "", "", second, 200).Body.String(); strings.Contains(got, original) {
			t.Fatal(got)
		}
		checkRefundCode(create(`{"sku":"e5-000001"}`, "e5-reuse-implicit"), 409, "identifier_retired")
		checkRefundCode(create(`{"sku":"E5-NUEVO","barcodes":["EXT-E5"],"reuseRetired":true`+confirm, "e5-reuse-bar-other"), 409, "barcode_reuse_unsafe")
		expect(patch(p, `{"expectedRevision":2,"barcodes":["`+originalCode+`","EXT-E5"],"reuseRetired":true`+confirm), 200)
		request("GET", "/api/v1/barcodes/EXT-E5/lookup", "", "", cookie, 200)
		checkRefundCode(patch(p, `{"expectedRevision":3,"generateInternal":true`+confirm), 409, "internal_barcode_exists")
		historySKU := ""
		if err := db.QueryRow(ctx, "SELECT sku FROM reservation_items WHERE tenant_id=$1 AND reservation_id=$2", created.TenantID, reservation).Scan(&historySKU); err != nil || historySKU != "E5-000001" {
			t.Fatalf("snapshot original: %q %v", historySKU, err)
		}

		request("PATCH", settingsPath, `{"skuPrefix":"FUT-","skuWidth":6,"expectedRevision":2}`, origin, cookie, 200)
		if got := request("GET", suggestionPath+"?after=2", "", "", cookie, 200).Body.String(); !strings.Contains(got, "FUT-000004") {
			t.Fatal(got)
		}
		for _, opFirst := range []bool{true, false} {
			name := "reutilizacion-primero"
			if opFirst {
				name = "reserva-primero"
			}
			t.Run(name, func(t *testing.T) {
				oldSKU := "E5-" + name
				newSKU := oldSKU + "-ACTUAL"
				source := readID(expect(create(fmt.Sprintf(`{"sku":%q}`, oldSKU), "e5-create-"+name), 201))
				sourcePath := parent + "/variants/" + source
				expect(adjust(adjustBody(created.StoreID, sellable, source, 2, "Stock para carrera E5"), "e5-stock-"+name, cookie), 201)
				expect(patch(sourcePath, fmt.Sprintf(`{"expectedRevision":1,"sku":%q`, newSKU)+confirm), 200)
				reserve := func() *httptest.ResponseRecorder { return makeReservation(source, "e5-reserva-"+name) }
				reuse := func() *httptest.ResponseRecorder {
					return create(fmt.Sprintf(`{"sku":%q,"reuseRetired":true`, oldSKU)+confirm, "e5-reuse-"+name)
				}
				var reserved, reassigned *httptest.ResponseRecorder
				if opFirst {
					reserved, reassigned = runCatalogRace(t, db, "reservation_items", reserve, reuse)
					checkRefundCode(reassigned, 409, "sensitive_history")
				} else {
					reassigned, reserved = runCatalogRace(t, db, "catalog_identifier_events", reuse, reserve)
					expect(reassigned, 201)
				}
				id := readID(expect(reserved, 201))
				var snapshot string
				if err := db.QueryRow(ctx, "SELECT sku FROM reservation_items WHERE tenant_id=$1 AND reservation_id=$2 AND variant_id=$3", created.TenantID, id, source).Scan(&snapshot); err != nil || snapshot != newSKU {
					t.Fatalf("snapshot vigente de la fuente: %q %v", snapshot, err)
				}
				if opFirst {
					request("POST", reservePath+"/"+id+"/cancel", storeInput, origin, cashier, 200)
					reassigned = expect(reuse(), 201)
				}
				target := readID(reassigned)
				if target == source {
					t.Fatal("se perdió la identidad original")
				}
				var events int
				if err := db.QueryRow(ctx, `SELECT count(*) FROM catalog_identifier_events WHERE tenant_id=$1 AND kind='sku' AND normalized=lower($2)
					AND ((variant_id=$3 AND action='retired') OR (variant_id=$4 AND action='reused')) AND actor_id=$5`, created.TenantID, oldSKU, source, target, created.OwnerID).Scan(&events); err != nil || events != 2 {
					t.Fatalf("trazabilidad de retiro y reasignación: %d %v", events, err)
				}
			})
		}
		execSQL(t, ctx, db, `INSERT INTO product_variants(tenant_id,product_id,sku)
			SELECT $1,$2,'BULK-' || lpad(n::text,6,'0') FROM generate_series(101,4197) n`, created.TenantID, parent[len(productsPath)+1:])
		request("PATCH", settingsPath, `{"skuPrefix":"BULK-","skuWidth":6,"expectedRevision":3}`, origin, cookie, 200)
		if got := request("GET", suggestionPath+"?after=100", "", "", cookie, 200).Body.String(); !strings.Contains(got, "BULK-004198") {
			t.Fatal(got)
		}

	})

	t.Run("E6 matriz atomica", func(t *testing.T) {
		type ref = catalog.BatchAttributeRef
		type row = catalog.BatchVariantInput
		size := func(name string) *ref {
			return &ref{ID: readID(request("POST", "/api/v1/catalog/sizes", fmt.Sprintf(`{"name":%q}`, name), origin, cookie, 201)), Revision: 1}
		}
		color := func(name string) *ref {
			return &ref{ID: readID(request("POST", "/api/v1/catalog/colors", fmt.Sprintf(`{"name":%q}`, name), origin, cookie, 201)), Revision: 1}
		}
		product := func() string {
			return readID(request("POST", productsPath, `{"name":"Prenda de matriz E6"}`, origin, cookie, 201))
		}
		send := func(id, operation, key string, entries []row, actor *http.Cookie) *httptest.ResponseRecorder {
			payload, err := json.Marshal(map[string]any{"rows": entries})
			if err != nil {
				panic(err)
			}
			return keyedCash(productsPath+"/"+id+"/variants/"+operation, string(payload), key, actor)
		}
		commit := func(id, key string, entries ...row) *httptest.ResponseRecorder {
			return send(id, "batch", key, entries, cookie)
		}
		parse := func(result *httptest.ResponseRecorder, status int) catalog.VariantBatchResult {
			t.Helper()
			expect(result, status)
			var batch catalog.VariantBatchResult
			if err := json.Unmarshal(result.Body.Bytes(), &batch); err != nil || batch.ID == "" || len(batch.Variants) == 0 {
				t.Fatalf("resultado de lote: %s (%v)", result.Body, err)
			}
			return batch
		}
		issues := func(result *httptest.ResponseRecorder, number int, code string) catalog.VariantBatchIssue {
			t.Helper()
			expect(result, 409)
			var body struct {
				Error struct {
					Rows []catalog.VariantBatchIssue `json:"rows"`
				} `json:"error"`
			}
			if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for _, issue := range body.Error.Rows {
				if issue.Row == number && issue.Code == code {
					return issue
				}
			}
			t.Fatalf("falta error %s de fila %d: %s", code, number, result.Body)
			return catalog.VariantBatchIssue{}
		}
		count := func(id string) int {
			t.Helper()
			var n int
			if err := db.QueryRow(ctx, "SELECT count(*) FROM product_variants WHERE tenant_id=$1 AND product_id=$2", created.TenantID, id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		small, large := size("E6 Pequeño"), size("E6 Grande")
		dark, light := color("E6 Oscuro"), color("E6 Claro")
		p := product()
		entries := []row{
			{Size: small}, {Color: dark, Status: "inactive"}, {Size: small, Color: dark, GenerateInternal: true},
			{Size: large, Color: light}, {},
		}
		request("POST", productsPath+"/"+p+"/variants/batch", `{"rows":[{"sku":"E6-NO-KEY"}]}`, origin, cookie, 400)
		preview := expect(send(p, "preview", "", entries, cookie), 200)
		var response struct {
			Rows []catalog.BatchPreviewRow `json:"rows"`
		}
		if err := json.Unmarshal(preview.Body.Bytes(), &response); err != nil || len(response.Rows) != len(entries) || count(p) != 0 {
			t.Fatalf("previsualización: %s (%v)", preview.Body, err)
		}
		seen := make(map[string]bool)
		for i, item := range response.Rows {
			if item.SKU == "" || seen[item.SKU] || len(item.Issues) > 0 {
				t.Fatalf("fila %d inesperada: %+v", i, item)
			}
			seen[item.SKU] = true
			entries[i].SKU = item.SKU
		}
		entries[0].SKU = "E6-MANUAL"
		saved := parse(commit(p, "e6-modes", entries...), 201)
		if len(saved.Variants) != 5 || count(p) != 5 || saved.Variants[1].Status != "inactive" ||
			saved.Variants[0].SKU != "E6-MANUAL" || saved.Variants[2].InternalBarcode == nil {
			t.Fatal(saved)
		}
		request("GET", "/api/v1/barcodes/"+*saved.Variants[2].InternalBarcode+"/lookup", "", "", cookie, 200)
		var stockRows int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM stock_balances b JOIN product_variants v
			ON (v.tenant_id,v.id)=(b.tenant_id,b.variant_id) WHERE v.tenant_id=$1 AND v.product_id=$2`, created.TenantID, p).Scan(&stockRows); err != nil || stockRows != 0 {
			t.Fatalf("el lote no debe crear saldos: %d %v", stockRows, err)
		}
		if got := parse(commit(p, "e6-modes", entries...), 201); got.ID != saved.ID || got.Variants[2].ID != saved.Variants[2].ID || *got.Variants[2].InternalBarcode != *saved.Variants[2].InternalBarcode {
			t.Fatal(got)
		}
		execSQL(t, ctx, db, `INSERT INTO catalog_variant_batches(tenant_id,product_id,actor_id,idempotency_key,request_hash,response,created_at)
			SELECT tenant_id,product_id,actor_id,'e6-aged',request_hash,response,now()-interval '8 days'
			FROM catalog_variant_batches WHERE tenant_id=$1 AND idempotency_key='e6-modes'`, created.TenantID)
		if got := parse(commit(p, "e6-aged", entries...), 201); got.ID != saved.ID || count(p) != 5 {
			t.Fatal(got)
		}
		if _, err := db.Exec(ctx, "DELETE FROM catalog_variant_batches WHERE tenant_id=$1 AND idempotency_key='e6-aged'", created.TenantID); err == nil {
			t.Fatal("se pudo borrar la protección histórica")
		}
		checkRefundCode(commit(p, "e6-modes", row{SKU: "E6-OTHER"}), 409, "idempotency_conflict")
		conflict := issues(commit(p, "e6-duplicate-active", row{Size: small, SKU: "E6-DD"}), 1, "attribute_combination")
		if conflict.Existing == nil || conflict.Existing.ID != saved.Variants[0].ID || conflict.Existing.Status != "active" {
			t.Fatal(conflict)
		}
		conflict = issues(commit(p, "e6-duplicate-inactive", row{Color: dark, SKU: "E6-DI"}), 1, "attribute_combination")
		if conflict.Existing == nil || conflict.Existing.ID != saved.Variants[1].ID || conflict.Existing.Status != "inactive" {
			t.Fatal(conflict)
		}
		issues(commit(p, "e6-null-duplicate", row{SKU: "E6-NULO"}), 1, "attribute_combination")
		request("POST", productsPath+"/"+p+"/variants", `{"sku":"E6-SINGLE-DUPLICATE"}`, origin, cookie, 409)
		legacy := product()
		existing := readID(request("POST", productsPath+"/"+legacy+"/variants", `{"sku":"E6-LEGADO","size":"e6 pequeño"}`, origin, cookie, 201))
		gotLegacy := issues(commit(legacy, "e6-legacy", row{Size: small, SKU: "E6-LEGADO-NUEVO"}), 1, "attribute_combination")
		if gotLegacy.Existing == nil || gotLegacy.Existing.ID != existing {
			t.Fatal(gotLegacy)
		}

		otherSize := readID(request("POST", "/api/v1/catalog/sizes", `{"name":"E6 de otro comercio"}`, origin, second, 201))
		issues(commit(product(), "e6-cross-attribute", row{Size: &ref{ID: otherSize, Revision: 1}, SKU: "E6-CROSS"}), 1, "attribute_unavailable")
		expect(send(otherProduct, "batch", "e6-cross-product", []row{{SKU: "E6-CROSS"}}, cookie), 404)
		expect(send(p, "batch", "e6-denied", []row{{SKU: "E6-CROSS"}}, cashier), 403)
		expect(send("00000000-0000-4000-8000-000000000003", "batch", "e6-unknown", []row{{SKU: "E6-CROSS"}}, cookie), 404)
		staleSize := size("E6 edición")
		staleProduct := product()
		expect(send(staleProduct, "preview", "", []row{{Size: staleSize}}, cookie), 200)
		request("PATCH", "/api/v1/catalog/sizes/"+staleSize.ID, `{"name":"E6 edición actualizada","status":"active","expectedRevision":1}`, origin, cookie, 200)
		issues(commit(staleProduct, "e6-stale", row{Size: staleSize, SKU: "E6-STALE"}), 1, "attribute_changed")
		request("PATCH", "/api/v1/catalog/sizes/"+staleSize.ID, `{"name":"E6 edición actualizada","status":"inactive","expectedRevision":2}`, origin, cookie, 200)
		issues(commit(staleProduct, "e6-off", row{Size: &ref{ID: staleSize.ID, Revision: 3}, SKU: "E6-STALE"}), 1, "attribute_inactive")
		issues(commit(staleProduct, "e6-repeat", row{Size: small, SKU: "E6-REPETIDO-A"}, row{Size: small, SKU: "E6-REPETIDO-B"}), 2, "batch_duplicate")
		if count(staleProduct) != 0 {
			t.Fatal("una validación dejó filas")
		}

		t.Run("asociacion equivalente protege la identidad de matriz", func(t *testing.T) {
			p := product()
			master := size("E6 valor inicial")
			parse(commit(p, "e6-association", row{Size: master, SKU: "E6-CANONICO"}), 201)
			legacy := readID(request("POST", productsPath+"/"+p+"/variants", `{"sku":"E6-DIFERENTE","size":"E6 valor renovado"}`, origin, cookie, 201))
			request("PATCH", "/api/v1/catalog/sizes/"+master.ID, `{"name":"E6 valor renovado","status":"active","expectedRevision":1}`, origin, cookie, 200)
			request("PATCH", productsPath+"/"+p+"/variants/"+legacy,
				`{"expectedRevision":1,"sizeId":"`+master.ID+`","size":"E6 valor renovado"}`, origin, cookie, 409)
		})

		t.Run("limite 50 y SKU Unicode", func(t *testing.T) {
			p := product()
			big := make([]row, 50)
			for i := range big {
				big[i] = row{Color: color(fmt.Sprintf("E6 color %02d", i)), SKU: fmt.Sprintf("E6-%02d-", i) + strings.Repeat("🎽", 72)}
			}
			tooMany := append(append([]row{}, big...), row{SKU: "E6-51"})
			expect(send(p, "preview", "", tooMany, cookie), 400)
			expect(commit(p, "e6-51", tooMany...), 400)
			if count(p) != 0 {
				t.Fatal("se dividió el lote")
			}
			if result := parse(commit(p, "e6-50", big...), 201); len(result.Variants) != 50 {
				t.Fatal(len(result.Variants))
			}
		})

		t.Run("rollback primera intermedia ultima", func(t *testing.T) {
			p := product()
			execSQL(t, ctx, db, `CREATE FUNCTION e6_reject_variant() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE unique_violation USING CONSTRAINT='variants_tenant_sku'; END $$;
				CREATE TRIGGER e6_reject_variant BEFORE INSERT ON product_variants FOR EACH ROW
				WHEN (NEW.sku LIKE 'E6-BREAK-%') EXECUTE FUNCTION e6_reject_variant()`)
			t.Cleanup(func() {
				execSQL(t, ctx, db, "DROP TRIGGER e6_reject_variant ON product_variants")
				execSQL(t, ctx, db, "DROP FUNCTION e6_reject_variant()")
			})
			state := func() string {
				t.Helper()
				var current string
				if err := db.QueryRow(ctx, `SELECT jsonb_build_array(
					(SELECT count(*) FROM barcodes WHERE tenant_id=$1),
					(SELECT count(*) FROM catalog_sensitive_events WHERE tenant_id=$1),
					(SELECT count(*) FROM catalog_identifier_events WHERE tenant_id=$1),
					(SELECT count(*) FROM catalog_variant_batches WHERE tenant_id=$1),
					(SELECT sku_number FROM catalog_identifier_sequences WHERE tenant_id=$1),
					(SELECT barcode_number FROM catalog_identifier_sequences WHERE tenant_id=$1))::text`, created.TenantID).Scan(&current); err != nil {
					t.Fatal(err)
				}
				return current
			}
			old := state()
			for i := 0; i < 3; i++ {
				drafts := []row{{Size: small, SKU: "E6-SAFE-A", GenerateInternal: true}, {Color: dark, SKU: "E6-SAFE-B", GenerateInternal: true},
					{Size: small, Color: light, SKU: "E6-SAFE-C", GenerateInternal: true}}
				drafts[i].SKU = fmt.Sprintf("E6-BREAK-%d", i)
				issues(commit(p, fmt.Sprintf("e6-atomic-%d", i), drafts...), i+1, "sku_taken")
				if state() != old || count(p) != 0 {
					t.Fatal("rollback incompleto")
				}
			}
		})

		var editorID string
		if err := db.QueryRow(ctx, `INSERT INTO users(tenant_id,email,name,password_hash)
			SELECT tenant_id,'e6-editor@example.test','Segundo titular',password_hash FROM users WHERE tenant_id=$1 AND id=$2 RETURNING id`,
			created.TenantID, created.OwnerID).Scan(&editorID); err != nil {
			t.Fatal(err)
		}
		execSQL(t, ctx, db, `INSERT INTO user_roles (tenant_id,user_id,role_code) VALUES ($1,$2,'OWNER')`, created.TenantID, editorID)
		otherEditor := loginAs("e6-editor@example.test", password)
		t.Run("carrera por combinacion", func(t *testing.T) {
			p := product()
			a, b := runCatalogRace(t, db, "product_variants",
				func() *httptest.ResponseRecorder {
					return commit(p, "e6-race-pair-a", row{Size: small, SKU: "E6-RACE-A"})
				},
				func() *httptest.ResponseRecorder {
					return send(p, "batch", "e6-race-pair-b",
						[]row{{Color: dark, SKU: "E6-RACE-UNCOMMITTED"}, {Size: small, SKU: "E6-RACE-B"}}, otherEditor)
				})
			parse(a, 201)
			issues(b, 2, "attribute_combination")
			if count(p) != 1 {
				t.Fatal("una solicitud competidora dejó filas parciales")
			}
			checkRefundCode(send(p, "batch", "e6-race-pair-a", []row{{Size: small, SKU: "E6-RACE-A"}}, otherEditor), 409, "idempotency_conflict")
		})
		t.Run("carrera por SKU", func(t *testing.T) {
			p := product()
			a, b := runCatalogRace(t, db, "product_variants",
				func() *httptest.ResponseRecorder {
					return commit(p, "e6-race-sku-a", row{Size: small, SKU: "E6-RACE-SKU"})
				},
				func() *httptest.ResponseRecorder {
					return send(p, "batch", "e6-race-sku-b", []row{{Size: large, SKU: "  e6-race-sku  "}}, otherEditor)
				})
			parse(a, 201)
			issues(b, 1, "sku_taken")
			if count(p) != 1 {
				t.Fatal("el segundo SKU duplicó la variante")
			}
		})
	})

	t.Run("Catalog optimistic revisions and sensitive audit", func(t *testing.T) {
		if response := catalogPatch(variantPath, `{"color":"Corrección histórica"}`, origin, cookie, 409).Body.String(); !strings.Contains(response, "sensitive_history") {
			t.Fatal(response)
		}

		parent := productsPath + "/" + readID(request("POST", productsPath, `{"name":"Prueba de revisiones"}`, origin, cookie, 201))
		first := parent + "/variants/" + readID(request("POST", parent+"/variants", `{"sku":"REV-A"}`, origin, cookie, 201))
		secondVariant := parent + "/variants/" + readID(request("POST", parent+"/variants", `{"sku":"REV-B"}`, origin, cookie, 201))
		request("PATCH", parent, `{"name":"Sin revisión"}`, origin, cookie, 400)
		if response := request("PATCH", parent, `{"expectedRevision":1,"name":" "}`, origin, cookie, 400).Body.String(); !strings.Contains(response, `"field":"name"`) {
			t.Fatal(response)
		}
		var colleague string
		if err := db.QueryRow(ctx, `INSERT INTO users (tenant_id,email,name,password_hash)
			SELECT tenant_id,'editor@example.test','Otro editor',password_hash FROM users WHERE tenant_id=$1 AND id=$2 RETURNING id`,
			created.TenantID, created.OwnerID).Scan(&colleague); err != nil {
			t.Fatal(err)
		}
		execSQL(t, ctx, db, `INSERT INTO user_roles (tenant_id,user_id,role_code) VALUES ($1,$2,'OWNER')`, created.TenantID, colleague)
		secondSession := loginAs("editor@example.test", password)
		race := func(paths [2]string, bodies [2]string) [2]*httptest.ResponseRecorder {
			start := make(chan struct{})
			var output [2]*httptest.ResponseRecorder
			var done [2]chan struct{}
			for i := range output {
				done[i] = make(chan struct{})
				go func() {
					defer close(done[i])
					req := httptest.NewRequest("PATCH", paths[i], strings.NewReader(bodies[i]))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Origin", origin)
					req.AddCookie([2]*http.Cookie{cookie, secondSession}[i])
					output[i] = httptest.NewRecorder()
					<-start
					router.ServeHTTP(output[i], req)
				}()
			}
			close(start)
			for _, finished := range done {
				<-finished
			}
			return output
		}
		assertConflict := func(result [2]*httptest.ResponseRecorder) {
			t.Helper()
			if result[0].Code == 409 {
				result[0], result[1] = result[1], result[0]
			}
			if result[0].Code != 200 || result[1].Code != 409 || !strings.Contains(result[1].Body.String(), "revision_conflict") {
				t.Fatalf("escrituras competidoras: %d %s, %d %s", result[0].Code, result[0].Body, result[1].Code, result[1].Body)
			}
		}
		assertConflict(race([2]string{parent, parent}, [2]string{`{"expectedRevision":1,"name":"Edición A"}`, `{"expectedRevision":1,"name":"Edición B"}`}))
		assertConflict(race([2]string{first, first}, [2]string{
			`{"expectedRevision":1,"color":"Azul","barcodes":["REV-CODE-A"],"reason":"Corrección A","confirmSensitive":true}`,
			`{"expectedRevision":1,"color":"Rojo","barcodes":["REV-CODE-B"],"reason":"Corrección B","confirmSensitive":true}`,
		}))
		var audits, codes int
		var oldColor, newColor string
		variant := first[strings.LastIndex(first, "/")+1:]
		if err := db.QueryRow(ctx, `SELECT count(*), min(before_data->>'color'), min(after_data->>'color')
			FROM catalog_sensitive_events WHERE tenant_id=$1 AND variant_id=$2 AND actor_id IN ($3,$4)`, created.TenantID, variant, created.OwnerID, colleague).Scan(&audits, &oldColor, &newColor); err != nil || audits != 1 || oldColor != "" || (newColor != "Azul" && newColor != "Rojo") {
			t.Fatalf("auditoría atómica: %d %q %q %v", audits, oldColor, newColor, err)
		}
		for _, code := range []string{"REV-CODE-A", "REV-CODE-B"} {
			req := httptest.NewRequest("GET", "/api/v1/barcodes/"+code+"/lookup", nil)
			req.AddCookie(cookie)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code == 200 {
				codes++
			} else if res.Code != 404 {
				t.Fatal(res.Code, res.Body)
			}
		}
		if codes != 1 {
			t.Fatal("se aplicó un código de una escritura rechazada")
		}
		request("PATCH", first, `{"expectedRevision":2,"color":"Sin confirmar"}`, origin, cookie, 400)
		request("PATCH", first, `{"expectedRevision":2,"color":"No autorizado","reason":"No autorizado","confirmSensitive":true}`, origin, manager, 403)
		for _, result := range race([2]string{first, secondVariant}, [2]string{`{"expectedRevision":2,"status":"inactive"}`, `{"expectedRevision":1,"status":"inactive"}`}) {
			if result.Code != 200 {
				t.Fatal(result.Code, result.Body)
			}
		}
		request("PATCH", parent, `{"expectedRevision":2,"name":"Revisión consultada"}`, origin, cookie, 200)
		request("PATCH", parent, `{"expectedRevision":3,"name":"Otro tenant"}`, origin, second, 404)
	})

	t.Run("Alta rapida transaccional", func(t *testing.T) {
		var quickStore string
		if err := db.QueryRow(ctx, `INSERT INTO stores(tenant_id,code,name) VALUES ($1,'alta','Alta rápida') RETURNING id`, created.TenantID).Scan(&quickStore); err != nil {
			t.Fatal(err)
		}
		execSQL(t, ctx, db, `INSERT INTO user_stores(tenant_id,user_id,store_id) VALUES ($1,$2,$3)`, created.TenantID, created.OwnerID, quickStore)
		location := readID(request("POST", "/api/v1/locations", `{"storeId":"`+quickStore+`","code":"ALTA","name":"Salón","type":"sales_floor","isSellable":true}`, origin, cookie, 201))
		type attr = catalog.QuickAttribute
		type row = catalog.QuickVariantInput
		internal := catalog.QuickBarcode{Mode: "internal"}
		price := func(value string) *string { return &value }
		send := func(key string, body map[string]any, actor *http.Cookie) *httptest.ResponseRecorder {
			payload, err := json.Marshal(body)
			if err != nil {
				panic(err)
			}
			return keyedCash("/api/v1/quick-products", string(payload), key, actor)
		}
		payload := func(name string, attrs []attr, rows []row) map[string]any {
			return map[string]any{"product": map[string]any{"name": name}, "storeId": quickStore, "locationId": location, "attributes": attrs, "variants": rows}
		}
		var shirt []row
		for _, color := range []string{"Negro", "Blanco"} {
			for _, size := range []string{"S", "M", "L"} {
				shirt = append(shirt, row{Values: []string{size, color}, Barcode: internal, Price: price("24500"), Quantity: 10})
			}
		}
		shirtBody := payload("Remera Alta Integral", []attr{{Name: "Talle", Role: "size"}, {Name: "Color", Role: "color"}}, shirt)
		// Prices need a list assigned to the store; without it nothing is written.
		if res := send("alta-sin-lista", shirtBody, cookie); res.Code != 409 || !strings.Contains(res.Body.String(), "price_list_missing") {
			t.Fatal(res.Code, res.Body)
		}
		list := readID(request("POST", "/api/v1/price-lists", `{"name":"Lista alta rápida"}`, origin, cookie, 201))
		request("PUT", "/api/v1/stores/"+quickStore+"/price-list", `{"listId":"`+list+`"}`, origin, cookie, 204)
		if res := send("alta-cajero", shirtBody, cashier); res.Code != 403 {
			t.Fatal(res.Code, res.Body)
		}
		first := send("alta-remera", shirtBody, cookie)
		if first.Code != 201 {
			t.Fatal(first.Code, first.Body)
		}
		var result catalog.QuickProductResult
		if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil || len(result.Variants) != 6 || result.Units != 60 {
			t.Fatalf("resultado del alta: %v %s", err, first.Body)
		}
		skus, codes := map[string]bool{}, map[string]bool{}
		for _, item := range result.Variants {
			if item.Variant.InternalBarcode == nil || len(*item.Variant.InternalBarcode) != 16 || item.Price == nil || *item.Price != "24500.00" || item.MovementID == nil {
				t.Fatalf("variante incompleta: %+v", item)
			}
			skus[strings.ToLower(item.Variant.SKU)], codes[*item.Variant.InternalBarcode] = true, true
		}
		if len(skus) != 6 || len(codes) != 6 {
			t.Fatal("SKU o códigos repetidos", skus, codes)
		}
		// The retry of a lost response returns the original result without
		// creating a second product or adding stock again.
		replay := send("alta-remera", shirtBody, cookie)
		if replay.Code != 201 || !strings.Contains(replay.Body.String(), result.Product.ID) {
			t.Fatal(replay.Code, replay.Body)
		}
		var products, movements, units, prices, audits int
		if err := db.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM products WHERE tenant_id=$1 AND name='Remera Alta Integral'),
			(SELECT count(*) FROM stock_movements m JOIN product_variants v ON (v.tenant_id,v.id)=(m.tenant_id,m.variant_id) WHERE v.product_id=$2 AND m.reason=$3),
			(SELECT COALESCE(sum(quantity_on_hand),0) FROM stock_balances b JOIN product_variants v ON (v.tenant_id,v.id)=(b.tenant_id,b.variant_id) WHERE v.product_id=$2),
			(SELECT count(*) FROM price_list_items i JOIN product_variants v ON (v.tenant_id,v.id)=(i.tenant_id,i.variant_id) WHERE v.product_id=$2 AND i.amount=24500),
			(SELECT count(*) FROM price_admin_events e JOIN product_variants v ON (v.tenant_id,v.id)=(e.tenant_id,e.variant_id) WHERE v.product_id=$2)`,
			created.TenantID, result.Product.ID, inventory.InitialStockReason).Scan(&products, &movements, &units, &prices, &audits); err != nil {
			t.Fatal(err)
		}
		if products != 1 || movements != 6 || units != 60 || prices != 6 || audits != 6 {
			t.Fatalf("persistencia: productos %d movimientos %d unidades %d precios %d auditoría %d", products, movements, units, prices, audits)
		}
		changed := payload("Remera Alta Integral", []attr{{Name: "Talle", Role: "size"}, {Name: "Color", Role: "color"}}, shirt[:5])
		if res := send("alta-remera", changed, cookie); res.Code != 409 {
			t.Fatal(res.Code, res.Body)
		}
		lookup := request("GET", "/api/v1/barcodes/"+*result.Variants[0].Variant.InternalBarcode+"/lookup", "", "", cookie, 200).Body.String()
		if !strings.Contains(lookup, result.Variants[0].Variant.ID) {
			t.Fatal(lookup)
		}

		// Three attributes: two variants share size and color and differ only
		// in the additional attribute, which is part of their identity.
		var jean []row
		for _, waist := range []string{"W30", "W32"} {
			for _, length := range []string{"L30", "L32"} {
				jean = append(jean, row{Values: []string{waist, length, "Índigo Alta"}, Barcode: internal, Quantity: 2})
			}
		}
		jeanAttrs := []attr{{Name: "Cintura", Role: "size"}, {Name: "Largo", Role: "option"}, {Name: "Color", Role: "color"}}
		jeanResult := send("alta-jean", payload("Jean Alta", jeanAttrs, jean), cookie)
		if jeanResult.Code != 201 || strings.Count(jeanResult.Body.String(), `{"name":"Largo","value":"L3`) != 4 {
			t.Fatal(jeanResult.Code, jeanResult.Body)
		}
		var jeanParsed catalog.QuickProductResult
		if err := json.Unmarshal(jeanResult.Body.Bytes(), &jeanParsed); err != nil {
			t.Fatal(err)
		}
		detail := request("GET", productsPath+"/"+jeanParsed.Product.ID, "", "", cookie, 200).Body.String()
		if !strings.Contains(detail, `"value":"L32"`) || strings.Count(detail, `"options":[{`) != 4 {
			t.Fatal(detail)
		}
		// An inactive catalog value is never reactivated silently.
		execSQL(t, ctx, db, `UPDATE catalog_attributes SET status='inactive' WHERE tenant_id=$1 AND kind='colors' AND name='Índigo Alta'`, created.TenantID)
		if res := send("alta-color-inactivo", payload("Jean inactivo", jeanAttrs, jean[:1]), cookie); res.Code != 409 || !strings.Contains(res.Body.String(), "attribute_inactive") {
			t.Fatal(res.Code, res.Body)
		}
		execSQL(t, ctx, db, `UPDATE catalog_attributes SET status='active' WHERE tenant_id=$1 AND kind='colors' AND name='Índigo Alta'`, created.TenantID)
		repeated := append([]row{}, jean[0], jean[0])
		if res := send("alta-jean-repetido", payload("Jean repetido", jeanAttrs, repeated), cookie); res.Code != 409 || !strings.Contains(res.Body.String(), "batch_duplicate") {
			t.Fatal(res.Code, res.Body)
		}

		// Simple product with a manufacturer code and a zero price.
		existing := catalog.QuickBarcode{Mode: "existing", Code: "7791234567890"}
		simple := send("alta-simple", payload("Billetera Alta", nil, []row{{Values: []string{}, Barcode: existing, Price: price("0")}}), cookie)
		if simple.Code != 201 || !strings.Contains(simple.Body.String(), `"price":"0.00"`) || !strings.Contains(simple.Body.String(), `"movementId":null`) {
			t.Fatal(simple.Code, simple.Body)
		}
		var before int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM products WHERE tenant_id=$1", created.TenantID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		duplicate := send("alta-codigo-repetido", payload("Cartera Alta", nil, []row{{Values: []string{}, Barcode: existing, Quantity: 3}}), cookie)
		if duplicate.Code != 409 || !strings.Contains(duplicate.Body.String(), "barcode_taken") || !strings.Contains(duplicate.Body.String(), "Billetera Alta") {
			t.Fatal(duplicate.Code, duplicate.Body)
		}
		invalid := send("alta-invalida", payload("Cinturón Alta", []attr{{Name: "Largo", Role: "size"}}, []row{
			{Values: []string{"90"}, Barcode: internal, Quantity: -1},
			{Values: []string{"95"}, Barcode: internal, Price: price("12,5")},
		}), cookie)
		if invalid.Code != 409 || !strings.Contains(invalid.Body.String(), `"field":"quantity"`) || !strings.Contains(invalid.Body.String(), `"field":"price"`) {
			t.Fatal(invalid.Code, invalid.Body)
		}
		if res := send("alta-sin-nombre", payload("  ", nil, []row{{Values: []string{}, Barcode: internal}}), cookie); res.Code != 400 {
			t.Fatal(res.Code, res.Body)
		}
		var after int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM products WHERE tenant_id=$1", created.TenantID).Scan(&after); err != nil || after != before {
			t.Fatalf("un alta rechazada dejó datos parciales: %d → %d %v", before, after, err)
		}
	})

	request("POST", logout, "", "https://other.example", cookie, 403)
	execSQL(t, ctx, db, `DELETE FROM user_roles WHERE tenant_id=(SELECT id FROM tenants WHERE slug='uno')`)
	request("GET", stores, "", "", cookie, 403)
	request("POST", logout, "", origin, cookie, 204)
	request("GET", me, "", "", cookie, 401)
	execSQL(t, ctx, db, `UPDATE sessions SET expires_at = now() - interval '1 second'`)
	request("GET", me, "", "", second, 401)
}

func execSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
