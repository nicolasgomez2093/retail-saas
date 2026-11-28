import './commercial.css';
import { resolveRoute, routeHref, type Destination } from './routes';
import { ContextLinks, ManagementHub, Navigation, SalesShortcut, navigationContext, useRouteAccess } from './Navigation';
import { resetNavigationAccess } from '../shared/api/navigationAccess';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Configuration, configurationViews } from '../features/settings/Configuration';
import { Home } from '../features/home/Home';
import { Login } from '../features/auth/Login';
import { Catalog } from '../features/catalog/Catalog';
import { isCatalogHash } from '../features/catalog/catalogQuery';
import { StockCounts } from '../features/inventory/StockCounts';
import { Inventory, stockViews } from '../features/inventory/Inventory';
import { Movements } from '../features/inventory/Movements';
import { isMovementHash, movementHref, movementPath, readMovementHash } from '../features/inventory/movementQuery';
import { Existences } from '../features/inventory/Existences';
import { existencesHref, existencesPath, readExistencesHash } from '../features/inventory/existencesQuery';
import { Cash } from '../features/cash/Cash';
import { PointOfSale } from '../features/pos/PointOfSale';
import { PurchaseOrders } from '../features/purchases/PurchaseOrders';
import { Suppliers } from '../features/suppliers/Suppliers';
import { SalesMarginReport } from '../features/reports/SalesMarginReport';
import { StorefrontAdmin } from '../features/storefront/StorefrontAdmin';
import { api, ApiError, type Credentials, type Identity, type LoginChoice, type Store } from '../shared/api/client';

// Access to the storefront is resolved by the navigation probe; the page itself shows its own load errors.
const ignoreAccess = () => {};

function readReportDay(query: string) {
  const day = new URLSearchParams(query).get('dia') ?? '';
  const parsed = new Date(day + 'T00:00:00Z');
  return /^\d{4}-\d{2}-\d{2}$/.test(day) && !day.startsWith('0000') &&
    !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === day ? day : '';
}

export function App() {
  const [{ destination, query: routeQuery, existencesHash, movementHash, lastPurchase, visitedViews, catalogHash }, setNavigation] = useState(() => {
    const hash = window.location.hash;
    const initial = resolveRoute(hash);
    return { ...initial, catalogHash: isCatalogHash(hash) ? hash : '#/productos', existencesHash: initial.destination === 'stock' ? hash : existencesPath, movementHash: isMovementHash(hash) ? hash : movementPath,
      lastPurchase: initial.destination === 'compras' || initial.destination === 'compras/pendientes' ? initial : resolveRoute('#/compras'),
      visitedViews: new Set<Destination>([initial.destination]) };
  });
  const [supplierContext, setSupplierContext] = useState<{ userId: string; id: string; name: string } | null>(null);
  const purchaseOrdersView = destination === 'compras' || destination === 'compras/pendientes';
  const supplierId = purchaseOrdersView || destination === 'proveedores'
    ? new URLSearchParams(routeQuery).get('proveedor') ?? '' : '';
  const customerAccounts = destination === 'fiados';
  const reportDay = destination === 'reportes' ? readReportDay(routeQuery) : '';
  const [branchId, setBranchId] = useState('');
  const [catalogCreateRequest, setCatalogCreateRequest] = useState(0);
  const [carts, setCarts] = useState<Record<string, { units: number; total: string }>>({});
  const updateCart = useCallback((storeId: string, summary: { units: number; total: string }) => {
    setCarts(current => current[storeId]?.units === summary.units && current[storeId]?.total === summary.total
      ? current : { ...current, [storeId]: summary });
  }, []);

  const [visitedBranches, setVisitedBranches] = useState<string[]>([]);
  const [pendingSalesView, setPendingSalesView] = useState<'vender' | 'ventas' | 'clientes' | null>(null);
  const [adjustmentBusy, setAdjustmentBusy] = useState(false);
  const activeAdjustment = useRef(false);
  function updateAdjustmentBusy(value: boolean) {
    activeAdjustment.current = value;
    setAdjustmentBusy(value);
  }
  const [countBusy, setCountBusy] = useState(false);
  const countGuard = useRef<(() => boolean) | null>(null);
  const salePending = pendingSalesView !== null;
  const [online, setOnline] = useState(() => navigator.onLine);
  const heading = useRef<HTMLHeadingElement>(null);
  const currentDestination = useRef(destination);
  const catalogContext = useRef({ navigation: 0, scope: 0, movements: 0 });
  const identityGeneration = useRef(0);
  const storesGeneration = useRef(0);
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [loginChoice, setLoginChoice] = useState<LoginChoice | null>(null);
  const [stores, setStores] = useState<Store[] | null>(null);
  const [busy, setBusy] = useState(true);
  const [initializing, setInitializing] = useState(true);
  const [storeRevision, setStoreRevision] = useState(0);
  const [priceRevision, setPriceRevision] = useState(0);
  const [cashRevision, setCashRevision] = useState(0);
  const [locationRevision, setLocationRevision] = useState(0);
  const [salesRevision, setSalesRevision] = useState(0);
  const [stockRevision, setStockRevision] = useState(0);
  const [error, setError] = useState('');

  const supplierName = supplierContext && supplierContext.userId === identity?.userId && supplierContext.id === supplierId
    ? supplierContext.name : '';
  const branch = stores?.find((store) => store.id === branchId) ?? stores?.[0];
  const branchIds = new Set([...visitedBranches, branch?.id]);
  const stockView = stockViews.find(([id]) => id === destination)?.[0];
  const movementView = destination === 'stock/movimientos';
  const countView = destination === 'stock/conteos';
  const legacyStockView = stockView && stockView !== 'stock' && stockView !== 'stock/movimientos' && stockView !== 'stock/conteos' ? stockView : undefined;
  const configurationView = destination === 'configuracion' ? destination : configurationViews.find(([id]) => id === destination)?.[0];
  const section = stockView ? 'stock' : purchaseOrdersView || destination === 'proveedores' ? 'compras' : configurationView ? 'configuracion' : destination;
  const visitedStock = stockViews.some(([id]) => id !== 'stock' && id !== 'stock/movimientos' && id !== 'stock/conteos' && visitedViews.has(id));
  const visitedPurchases = visitedViews.has('compras') || visitedViews.has('compras/pendientes');
  const visitedPOS = visitedViews.has('fiados') || visitedViews.has('ventas') || visitedViews.has('vender') || visitedViews.has('clientes');
  const visitedConfiguration = visitedViews.has('configuracion') || configurationViews.some(([id]) => visitedViews.has(id));
  const branchScoped = customerAccounts || destination !== 'proveedores' && ['vender', 'ventas', 'clientes', 'caja', 'stock', 'compras', 'reportes'].includes(section);

  useLayoutEffect(() => {
    function onNavigate(event?: HashChangeEvent) {
      const hash = window.location.hash;
      const next = resolveRoute(hash);
      const previousDestination = currentDestination.current;
      if (event && previousDestination === 'stock/conteos' && countGuard.current && !countGuard.current()) {
        window.history.replaceState(window.history.state, '', new URL(event.oldURL).hash || routeHref('stock/conteos'));
        return;
      }
      // Process each transition even if another hash change has already queued.
      if (previousDestination === 'productos' || next.destination === 'productos' ||
          (event && isCatalogHash(new URL(event.newURL).hash))) {
        catalogContext.current.navigation++;
      }
      if (previousDestination === 'stock/movimientos' || next.destination === 'stock/movimientos' ||
          (event && isMovementHash(new URL(event.newURL).hash))) catalogContext.current.movements++;
      if (previousDestination !== next.destination) setError('');
      currentDestination.current = next.destination;
      if (next.destination !== 'productos' && next.destination !== 'stock' && next.destination !== 'stock/movimientos' && hash !== next.href) {
        window.history.replaceState(window.history.state, '', next.href);
      }
      setNavigation(({ visitedViews: previous, existencesHash, movementHash, lastPurchase, catalogHash }) => ({
        ...next,
        catalogHash: next.destination === 'productos' ? hash : catalogHash,
        existencesHash: next.destination === 'stock' ? hash : existencesHash,
        movementHash: next.destination === 'stock/movimientos' ? hash : movementHash,
        lastPurchase: next.destination === 'compras' || next.destination === 'compras/pendientes' ? next : lastPurchase,
        visitedViews: previous.has(next.destination) ? previous : new Set([...previous, next.destination]),
      }));
      if (event && !(previousDestination === next.destination && (next.destination === 'productos' || next.destination === 'stock' || next.destination === 'stock/movimientos'))) {
        requestAnimationFrame(() => heading.current?.focus());
      }
    }
    onNavigate();
    function onConnection() { setOnline(navigator.onLine); }
    window.addEventListener('hashchange', onNavigate);
    window.addEventListener('online', onConnection);
    window.addEventListener('offline', onConnection);
    return () => {
      window.removeEventListener('hashchange', onNavigate);
      window.removeEventListener('online', onConnection);
      window.removeEventListener('offline', onConnection);
    };
  }, []);

  useLayoutEffect(() => {
    if (loginChoice) heading.current?.focus();
  }, [loginChoice]);

  useEffect(() => {
    if (!salePending) return;
    function warnPending(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = '';
    }
    window.addEventListener('beforeunload', warnPending);
    return () => window.removeEventListener('beforeunload', warnPending);
  }, [salePending]);

  function changeBranch(id: string) {
    if (!branch || id === branch.id || salePending || activeAdjustment.current || countBusy) return;
    if (countGuard.current && !countGuard.current()) return;
    if (!window.confirm('¿Cambiar de sucursal? El trabajo en curso quedará en su sucursal original.')) return;
    catalogContext.current.scope++;
    if (destination === 'stock') {
      const { values, errors } = readExistencesHash(window.location.hash);
      if (!errors.length && (values.ubicacion || values.detalle || values.page !== '1')) {
        window.location.hash = existencesHref({ ...values, ubicacion: '', detalle: '', page: '1' });
      }
    }
    if (movementView) {
      const { values, errors } = readMovementHash(window.location.hash);
      if (!errors.length && (values.ubicacion || values.detalle || values.siguiente || values.anterior || values.referencia)) {
        window.location.hash = movementHref({ ...values, ubicacion: '', detalle: '', siguiente: '', anterior: '', referencia: '' });
      }
    }
    if (legacyStockView === 'stock/ajustes') {
      const params = new URLSearchParams(routeQuery);
      if (params.has('ubicacion')) {
        params.delete('ubicacion');
        window.location.hash = routeHref('stock/ajustes', params);
      }
    }
    if (countView && routeQuery) window.location.hash = routeHref('stock/conteos');
    setVisitedBranches((current) => [...new Set([...current, branch.id])]);
    setBranchId(id);
  }

  function showError(cause: unknown) {
    setError(cause instanceof Error ? cause.message : 'No pudimos completar la operación.');
  }

  async function readStores(identityAttempt = identityGeneration.current) {
    const attempt = ++storesGeneration.current;
    try {
      const result = await api.stores();
      if (attempt !== storesGeneration.current || identityAttempt !== identityGeneration.current) return;
      catalogContext.current.scope++;
      setStores(result.items);
    } catch (cause) {
      if (attempt === storesGeneration.current && identityAttempt === identityGeneration.current) throw cause;
    }
  }

  async function refresh() {
    const attempt = ++identityGeneration.current;
    const user = await api.me();
    if (attempt !== identityGeneration.current) return;
    catalogContext.current.scope++;
    setStores(null);
    setIdentity(user);
    await readStores(attempt);
  }

  useEffect(() => {
    void refresh().catch((cause: unknown) => {
      if (!(cause instanceof ApiError && cause.status === 401)) showError(cause);
    }).finally(() => { setInitializing(false); setBusy(false); });
  }, []);

  async function login(credentials: Credentials) {
    setBusy(true);
    setError('');
    try {
      const choice = await api.login(credentials);
      if (choice) setLoginChoice(choice);
      else await refresh();
    } catch (cause) {
      showError(cause);
    } finally {
      setBusy(false);
    }
  }

  async function selectCommerce(choice: LoginChoice, commerceId: string) {
    setBusy(true);
    setError('');
    try {
      await api.chooseCommerce(choice.token, commerceId);
      setLoginChoice(null);
      await refresh();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        setLoginChoice(null);
        setError('Volvé a iniciar sesión para elegir un comercio.');
      } else showError(cause);
    } finally {
      setBusy(false);
    }
  }

  const confirmCatalogExit = useRef<(() => boolean) | null>(null);

  async function logout() {
    if (countGuard.current && !countGuard.current()) return;
    if (activeAdjustment.current) { setError('Esperá a que termine la operación de inventario.'); return; }
    if (confirmCatalogExit.current && !confirmCatalogExit.current()) return;
    setBusy(true);
    setError('');
    try {
      await api.logout();
      identityGeneration.current++;
      catalogContext.current.scope++;
      catalogContext.current.navigation++;
      currentDestination.current = 'inicio';
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
      setNavigation({ ...resolveRoute(''), catalogHash: '#/productos', existencesHash: existencesPath, movementHash: movementPath, lastPurchase: resolveRoute('#/compras'), visitedViews: new Set(['inicio']) });
      setSupplierContext(null);
      setBranchId('');
      setVisitedBranches([]);
      setPendingSalesView(null);
      setCarts({});
      resetNavigationAccess();
      setIdentity(null);
      setStores(null);
    } catch (cause) {
      showError(cause);
    } finally {
      setBusy(false);
    }
  }

  const activeNavigation = navigationContext(destination);
  const routeAccess = useRouteAccess(activeNavigation, branch?.id);
  return (
    <main className={identity ? "app-shell commercial-shell" : "auth-shell"} data-view={destination} aria-busy={busy}>
      {!identity && <header className="brand">retail<span>Gestión comercial</span></header>}
      {!identity && error && <p role="alert">{error}</p>}
      {identity ? (
        <>
          <button type="button" className="skip-link" onClick={() => heading.current?.focus()}>Ir al contenido</button>
          <Navigation destination={activeNavigation} branchId={branch?.id} user={identity.name} busy={busy}
            onLogout={() => void logout()} onCurrentLink={() => { if (!isCatalogHash(window.location.hash)) heading.current?.focus(); }} />
          <div className="workspace" id="workspace">
            <header className="workspace-header">
              <div>
                {activeNavigation.group !== 'inicio' && activeNavigation.groupLabel !== activeNavigation.label && <p className="workspace-breadcrumb">{activeNavigation.groupLabel} · {activeNavigation.label}</p>}
                {destination === 'inicio' && <p className="workspace-breadcrumb">Resumen operativo</p>}
                <h1 ref={heading} tabIndex={-1}>{destination === 'productos' ? 'Productos' : activeNavigation.label}</h1>
                
              </div>
              <div className="workspace-controls">
                <label>Sucursal
                  <select value={branch?.id ?? ''} onChange={(event) => changeBranch(event.target.value)}
                    disabled={salePending || adjustmentBusy || countBusy || (stores?.length ?? 0) < 2}>
                    {!branch && <option value="">{stores ? 'Sin sucursales asignadas' : 'Cargando sucursales…'}</option>}
                    {stores?.map((store) => <option key={store.id} value={store.id}>{store.name}</option>)}
                  </select>
                </label>
                {destination === 'inicio' ? <SalesShortcut branchId={branch?.id} primary label="＋ Nueva venta" />
                  : destination !== 'vender' && destination !== 'productos' && <SalesShortcut branchId={branch?.id} />}
              </div>
            </header>
            <div className="workspace-body">
              {error && <div className="workspace-feedback" role="alert"><span>{error}</span><button type="button" onClick={() => setError('')}>Cerrar aviso</button></div>}
              {branch && destination !== 'vender' && destination !== 'inicio' && (carts[branch.id]?.units ?? 0) > 0 && <a className="commercial-resume-cart" href={routeHref('vender')}>
                <span><strong>Venta en curso · {carts[branch.id].units} unidades</strong><small>Retomá el carrito donde lo dejaste</small></span>
                <strong>{carts[branch.id].total}</strong><span>Continuar →</span>
              </a>}
              {!online && <p role="status" className="workspace-notice">Sin conexión. Revisá el resultado de las operaciones en curso cuando vuelvas a conectarte.</p>}
              {adjustmentBusy && <p role="status" className="workspace-notice">Esperá a que termine la operación de inventario antes de cambiar de sucursal.</p>}
              {salePending && <p role="status" className="workspace-notice">Resolvé la operación en <a href={routeHref(pendingSalesView === 'clientes' ? 'fiados' : pendingSalesView)}>{navigationContext(pendingSalesView === 'clientes' ? 'fiados' : pendingSalesView).label}</a> antes de cambiar de sucursal.</p>}
              {(destination === 'stock' || movementView) && !branch && <p role="status">{stores ? 'No tenés sucursales asignadas.' : 'Cargando sucursales…'}</p>}
              {routeAccess === 'denied' && (destination !== 'stock' && !movementView || branch) && <p role="status">No tenés permiso para acceder a esta sección.</p>}
              <div hidden={routeAccess === 'denied'}>
                {(destination === 'vender' || destination === 'ventas') && branch && routeAccess === 'allowed' &&
                  <nav className="report-links" aria-label="Operaciones de venta">
                    <a href={routeHref('vender')} aria-current={destination === 'vender' ? 'page' : undefined}>Punto de venta</a>
                    <a href={routeHref('ventas')} aria-current={destination === 'ventas' ? 'page' : undefined}>Historial de ventas</a>
                  </nav>}
                {stockView && branch && <nav className="report-links inventory-tabs" aria-label="Secciones de inventario">
                  {stockViews.map(([id, label]) => <a key={id} href={routeHref(id)} aria-current={stockView === id ? 'page' : undefined}>{label}</a>)}
                </nav>}
                {(destination === 'clientes' || customerAccounts) && branch && routeAccess === 'allowed' &&
                  <nav className="report-links" aria-label="Operaciones de clientes">
                    <a href={routeHref('clientes')} aria-current={destination === 'clientes' ? 'page' : undefined}>Clientes</a>
                    <a href={routeHref('fiados')} aria-current={customerAccounts ? 'page' : undefined}>Fiados y abonos</a>
                  </nav>}
                {destination === 'gestion' && <ManagementHub branchId={branch?.id} />}
                {destination === 'inicio' && <Home key={branch?.id} branch={branch} cart={branch ? carts[branch.id] : undefined} loading={stores === null} onCreate={() => {
                  setCatalogCreateRequest(value => value + 1); window.location.hash = routeHref('productos');
                }} />}
                {branchScoped && !branch && stores && destination !== 'stock' && !movementView && <p>No tenés sucursales asignadas.</p>}
                {destination === 'stock' && routeAccess !== 'denied' && branch &&
                  <Existences key={identity.tenantId + ':' + identity.userId} branch={branch} hash={existencesHash}
                    context={catalogContext.current} scope={catalogContext.current.scope}
                    stockRevision={salesRevision + stockRevision} locationRevision={locationRevision} />}
                {movementView && routeAccess !== 'denied' && branch &&
                  <Movements key={identity.tenantId + ':' + identity.userId} branch={branch} hash={movementHash}
                    context={catalogContext.current} scope={catalogContext.current.scope} navigation={catalogContext.current.movements}
                    stockRevision={salesRevision + stockRevision} />}
                {countView && routeAccess !== 'denied' && branch &&
                  <StockCounts key={identity.tenantId + ':' + identity.userId + ':' + branch.id} store={branch}
                    identityScope={identity.tenantId + ':' + identity.userId} query={routeQuery}
                    context={catalogContext.current} scope={catalogContext.current.scope} guard={countGuard}
                    onBusy={setCountBusy} onStockChanged={() => setStockRevision(value => value + 1)} />}
                {stores?.filter((store) => branchIds.has(store.id)).map((store) => (
                  <div key={store.id} hidden={store.id !== branch?.id || !branchScoped} className="branch-panels">
                    {visitedViews.has('caja') && <div className="feature-panel" hidden={destination !== 'caja'}>
                      <Cash key={'cash-' + identity.userId} stores={[store]} salesRevision={salesRevision}
                        onCashChanged={() => setCashRevision((revision) => revision + 1)} />
                    </div>}
                    {visitedPOS && <div className="feature-panel" hidden={destination !== 'vender' && destination !== 'ventas' && destination !== 'clientes' && !customerAccounts}>
                      <PointOfSale key={identity.tenantId + ':' + identity.userId} stores={[store]} active={store.id === branch?.id && ['vender','ventas','clientes'].includes(destination) || store.id === branch?.id && customerAccounts}
                        favoriteScope={[identity.tenantId, identity.userId]} customerAccounts={customerAccounts} onCartChanged={updateCart}
                        settingsRevision={cashRevision + storeRevision + locationRevision} historyRevision={salesRevision}
                        view={destination === 'clientes' || customerAccounts ? 'customers' : destination === 'ventas' ? 'history' : 'checkout'}
                        onOperationPending={(pending, origin) => setPendingSalesView(pending ? origin : null)} onSold={() => setSalesRevision((revision) => revision + 1)} />
                    </div>}
                    {visitedViews.has('reportes') && <div className="feature-panel" hidden={destination !== 'reportes'}>
                      <SalesMarginReport key={'sales-margin-' + identity.userId} stores={[store]} revision={salesRevision}
                        active={destination === 'reportes' && branch?.id === store.id} reportDay={reportDay} />
                      {destination === 'reportes' && <ContextLinks branchId={store.id} />}
                    </div>}
                    {visitedStock && <div className="feature-panel" hidden={!legacyStockView}>
                      <Inventory key={'inventory-' + identity.userId} stores={[store]} stockRevision={salesRevision + stockRevision} view={legacyStockView ?? 'stock'} active={Boolean(legacyStockView && branch?.id === store.id)}
                        linkedLocation={legacyStockView ? new URLSearchParams(routeQuery).get('ubicacion') ?? '' : ''}
                        linkedVariant={legacyStockView === 'stock/ajustes' ? new URLSearchParams(routeQuery).get('variante') ?? '' : ''}
                        context={catalogContext.current} scope={catalogContext.current.scope}
                        recoveryScope={identity.tenantId + ':' + identity.userId} onAdjustmentBusy={updateAdjustmentBusy}
                        onStockChanged={() => setStockRevision((revision) => revision + 1)} onLocationsChanged={() => setLocationRevision((revision) => revision + 1)} />
                    </div>}
                    {visitedPurchases && <div className="feature-panel" hidden={destination !== 'compras' && destination !== 'compras/pendientes'}>
                      <PurchaseOrders key={'purchases-' + identity.userId} stores={[store]} pendingOnly={lastPurchase.destination === 'compras/pendientes'}
                        supplierId={new URLSearchParams(lastPurchase.query).get('proveedor') ?? ''} supplierName={supplierName}
                        onReceived={() => setStockRevision((value) => value + 1)}
                        onCreateProduct={() => { setCatalogCreateRequest(value => value + 1); window.location.hash = routeHref('productos'); }} />
                    </div>}
                  </div>
                ))}
                {visitedViews.has('productos') && <div className="feature-panel" hidden={destination !== 'productos'}>
                  <Catalog key={identity.tenantId + ':' + identity.userId} stores={stores} branch={branch}
                    confirmExit={confirmCatalogExit} hash={catalogHash} active={destination === 'productos'} context={catalogContext.current}
                    navigation={catalogContext.current.navigation} scope={catalogContext.current.scope}
                    priceRevision={priceRevision} stockRevision={salesRevision + stockRevision}
                    recoveryScope={identity.tenantId + ':' + identity.userId} onInventoryBusy={updateAdjustmentBusy}
                    onInventoryChanged={() => setStockRevision(value => value + 1)} createRequest={catalogCreateRequest} />
                </div>}
                {visitedViews.has('proveedores') && <div className="feature-panel" hidden={destination !== 'proveedores'}>
                  <Suppliers key={'suppliers-' + identity.userId} storeRevision={storeRevision} selectedId={destination === 'proveedores' ? supplierId : ''}
                    onHistory={(id, name) => setSupplierContext({ userId: identity.userId, id, name })} />
                </div>}
                {visitedViews.has('tienda') && <div className="feature-panel" hidden={destination !== 'tienda'}>
                  <StorefrontAdmin key={identity.tenantId + ':' + identity.userId} storeRevision={storeRevision} onAccessChange={ignoreAccess} />
                </div>}
                {visitedConfiguration && <Configuration key={identity.tenantId + ':' + identity.userId} view={configurationView} branch={branch} storeRevision={storeRevision}
                  onPricesChanged={() => setPriceRevision((revision) => revision + 1)} onStoresChanged={() => {
                    setStoreRevision((revision) => revision + 1);
                    void readStores().catch(showError);
                  }} />}
              </div>
            </div>
          </div>
        </>
      ) : initializing ? <p role="status">Conectando…</p> : loginChoice ? (
        <section aria-labelledby="commerce-choice-title">
          <h1 id="commerce-choice-title" ref={heading} tabIndex={-1}>Elegí un comercio</h1>
          <ul className="catalog-items">{loginChoice.commerces.map((commerce) => (
            <li key={commerce.id}><button type="button" disabled={busy} onClick={() => void selectCommerce(loginChoice, commerce.id)}>
              {commerce.name}{loginChoice.commerces.some((other) => other.id !== commerce.id && other.name === commerce.name)
                ? ` (${commerce.code})` : ''}
            </button></li>
          ))}</ul>
          <button type="button" disabled={busy} onClick={() => { setLoginChoice(null); setError(''); }}>Volver al inicio de sesión</button>
        </section>
      ) : <Login busy={busy} onLogin={(value) => void login(value)} />}
    </main>
  );
}
