import { useEffect, useState, type ReactNode } from 'react';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { routeHref } from '../../app/routes';
import { SalesShortcut } from '../../app/Navigation';
import { api, ApiError, type Store } from '../../shared/api/client';
import { posPrice } from '../pos/priceDisplay';
import { loadSalesWindow } from '../reports/salesWindow';
import './home.css';

type ReadState<T> = { status: 'loading' | 'denied' } | { status: 'error'; message: string } | { status: 'ready'; data: T };

function useRead<T>(storeId: string, load: (storeId: string) => Promise<T>) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ attempt: number; storeId: string; state: ReadState<T> }>();
  const state: ReadState<T> = result?.attempt === attempt && result.storeId === storeId
    ? result.state : { status: 'loading' };

  useEffect(() => {
    let active = true;
    function save(state: ReadState<T>) {
      if (active) setResult({ attempt, storeId, state });
    }
    void load(storeId).then((data) => save({ status: 'ready', data })).catch((cause: unknown) => {
      const status = cause instanceof ApiError ? cause.status : undefined;
      if (status === 403) save({ status: 'denied' });
      else save({ status: 'error', message: status === 401 ? 'Volvé a iniciar sesión para consultar este dato.'
        : status === 404 ? 'El dato no está disponible para esta sucursal.'
        : status === 0 ? 'No pudimos establecer la conexión.' : 'No pudimos cargar este dato.' });
    });
    return () => { active = false; };
  }, [attempt, load, storeId]);

  return { state, retry: () => setAttempt((value) => value + 1) };
}

function Section<T>({ name, read, className = '', errorDetail, children }: {
  name: string; read: ReturnType<typeof useRead<T>>; className?: string; errorDetail?: string; children: (data: T) => ReactNode;
}) {
  const { state, retry } = read;
  return <section className={'home-panel ' + className} aria-label={name} aria-busy={state.status === 'loading'}>
    <h2>{name}</h2>
    {state.status === 'loading' && <p role="status">Cargando…</p>}
    {state.status === 'denied' && <p>Sin permiso para consultar este dato.</p>}
    {state.status === 'error' && <>
      <p role="status">{state.message}{errorDetail && ' ' + errorDetail}</p>
      <button type="button" aria-label={'Reintentar ' + name} onClick={retry}>Reintentar</button>
    </>}
    {state.status === 'ready' && children(state.data)}
  </section>;
}

function amount(value: string, currency: string) {
  return currency + ' ' + posPrice(value);
}

function Metric({ value, detail }: { value: string; detail?: string }) {
  return <p className="home-value"><strong>{value}</strong>{detail && <span>{detail}</span>}</p>;
}

const loadToday = (store: string) => loadSalesWindow(store, 1);
const loadCash = (store: string) => api.cashRegisters(store);
const loadCredit = (store: string) => api.receivables(store, { status: 'pending', query: '' }, 0);
const loadPurchases = (store: string) => api.purchaseOrders(store, 0, { status: 'pending', query: '', fromDate: '', toDate: '' });
const loadCreateAccess = (store: string) => api.products('', 0, undefined, store);
const loadRecent = (store: string) => api.saleHistory(store, '', 0, 5);

async function loadStock(store: string) {
  const [{ items: locations, canManage }, { items }] = await Promise.all([
    api.locations(store), api.stockBalances(store, '', true),
  ]);
  const knownLocations = new Set(locations.map((location) => location.id));
  const alerts = items.filter((row) => row.active && row.lowStock && row.minimum !== null && knownLocations.has(row.locationId));
  const affectedLocations = [...new Set(alerts.map((row) => row.locationId))];
  const params = new URLSearchParams({ bajo: '1' });
  if (affectedLocations.length) params.set('ubicacion', affectedLocations[0]);
  return {
    href: '#/inventario/existencias?' + params,
    count: new Set(alerts.map((row) => row.variantId)).size,
    locationCount: affectedLocations.length,
    hasLocations: locations.length > 0,
    missingSellable: canManage && !locations.some((location) => location.isSellable),
  };
}

function Dashboard({ branch, onCreate, cart }: { branch: Store; onCreate: () => void; cart?: { units: number; total: string } }) {
  const sales = useRead(branch.id, loadToday);
  const cash = useRead(branch.id, loadCash);
  const fiados = useRead(branch.id, loadCredit);
  const stock = useRead(branch.id, loadStock);
  const purchases = useRead(branch.id, loadPurchases);
  const recent = useRead(branch.id, loadRecent);
  const createAccess = useRead(branch.id, loadCreateAccess);
  const [recentTimezone, setRecentTimezone] = useState<string>();
  useEffect(() => {
    if (cash.state.status === 'loading') return;
    setRecentTimezone(current => current ?? (cash.state.status === 'ready' ? cash.state.data.timezone : 'UTC'));
  }, [cash.state]);
  const dates = recentTimezone && new Intl.DateTimeFormat('es-AR', {
    timeZone: recentTimezone, dateStyle: 'short', timeStyle: 'short',
  });
  const attentionKnown = [stock, fiados, purchases].every(read => read.state.status === 'ready' || read.state.status === 'denied');
  const needsAttention = stock.state.status === 'ready' && (stock.state.data.count > 0 || stock.state.data.missingSellable)
    || fiados.state.status === 'ready' && fiados.state.data.summary.some(row => row.pendingCount > 0)
    || purchases.state.status === 'ready' && purchases.state.data.summary.orderCount > 0;
  return <>
    <div className="home-metrics">
      <Section name="Ventas registradas" read={sales} className="home-metric">
        {({ report, today }) => {
          const rows = (report.dailyTotals ?? []).filter(entry => entry.date === today);
          return rows.length ? rows.map(entry => <Metric key={entry.currency} value={amount(entry.netSales, entry.currency)} detail={entry.saleCount + ' operaciones de hoy'} />)
            : <Metric value="0 ventas" detail="Sin operaciones registradas hoy" />;
        }}
      </Section>
      <Section name="Cobros registrados" read={sales} className="home-metric">
        {({ report }) => {
          const totals = new Map<string, bigint>();
          for (const payment of report.paymentTotals ?? []) {
            if (payment.method === 'on_credit') continue;
            totals.set(payment.currency, (totals.get(payment.currency) ?? 0n) + minorUnits(payment.amount));
          }
          return totals.size ? [...totals].map(([currency,total]) => <Metric key={currency} value={amount(decimalText(total),currency)} detail="Pagos de ventas de hoy · sin deuda pendiente" />)
            : <Metric value="0 cobros" detail="Sin pagos de ventas registrados hoy" />;
        }}
      </Section>
      <Section name="Stock bajo o agotado" read={stock} className="home-metric">
        {({ count, hasLocations }) => <Metric value={hasLocations ? String(count) : 'Sin ubicaciones'} detail="Según mínimos configurados" />}
      </Section>
      <section className="home-panel home-metric" aria-label="Preparación de productos">
        <h2>Productos por preparar</h2><a className="home-catalog-review" href={routeHref('productos')}>Revisar catálogo</a>
        <small>Consultá el estado por variante</small>
      </section>
    </div>
    {cart && cart.units > 0 && <a className="commercial-resume-cart" href={routeHref('vender')}>
      <span className="home-resume-icon" aria-hidden="true">🛒</span><span><strong>Venta en curso · {cart.units} {cart.units === 1 ? 'unidad' : 'unidades'}</strong><small>Retomá el carrito donde lo dejaste</small></span>
      <strong>{cart.total}</strong><span>Continuar →</span>
    </a>}
    <nav className="commercial-shortcuts" aria-label="Accesos rápidos de Inicio">
      <strong>Accesos rápidos</strong><div className="home-shortcut-strip">
      <SalesShortcut branchId={branch.id} label="Vender" icon="＋" />
      {createAccess.state.status === 'ready' && createAccess.state.data.canManage && <button type="button" className="button-secondary" onClick={onCreate}><span aria-hidden="true">▦</span>Crear producto</button>}
      <a href={routeHref('stock')}><span aria-hidden="true">▤</span>Inventario</a><a href={routeHref('compras')}><span aria-hidden="true">◈</span>Compras</a><a href={routeHref('caja')}><span aria-hidden="true">◴</span>Caja</a><a href={routeHref('reportes')}><span aria-hidden="true">▥</span>Reportes</a>
      </div>
    </nav>
    <div className="commercial-home-columns">
      <section className="home-panel home-recent-sales" aria-label="Últimas ventas" aria-busy={recent.state.status === 'loading'}>
        <header className="home-panel-heading"><h2>Últimas ventas</h2><a href={routeHref('ventas')}>Ver historial →</a></header>
        {recent.state.status === 'loading' && <p role="status">Consultando ventas…</p>}
        {recent.state.status === 'error' && <p className="home-inline-read">{recent.state.message} <button type="button" onClick={recent.retry}>Reintentar ventas</button></p>}
        {recent.state.status === 'denied' && <p>Sin permiso para consultar ventas.</p>}
        {recent.state.status === 'ready' && (recent.state.data.items.length ? <ul>{recent.state.data.items.slice(0,5).map(sale => <li key={sale.id}>
          <span className="home-activity-icon" aria-hidden="true">✓</span><span className="home-activity-description"><strong>Venta N.º {sale.number}</strong>
          {dates ? <time dateTime={sale.createdAt}>{dates.format(new Date(sale.createdAt))}{recentTimezone === 'UTC' ? ' UTC' : ''}</time> : <small>Consultando zona horaria…</small>}</span>
          <strong>{amount(sale.total,sale.currency)}</strong>
        </li>)}</ul> : <div className="home-compact-empty"><span aria-hidden="true">▤</span><div><strong>Todavía no hay ventas</strong><p>Las operaciones aparecerán acá.</p></div><SalesShortcut branchId={branch.id} label="Vender →" /></div>)}
      </section>
      <div className="commercial-home-operations">
        <section className="home-panel" aria-label="Caja" aria-busy={cash.state.status === 'loading'}>
          <header className="home-panel-heading"><h2>Caja</h2><a href={routeHref('caja')}>Ver caja →</a></header>
          {cash.state.status === 'loading' && <p role="status">Consultando caja…</p>}
          {cash.state.status === 'error' && <p className="home-inline-read">{cash.state.message} <button type="button" onClick={cash.retry}>Reintentar caja</button></p>}
          {cash.state.status === 'denied' && <p>Sin permiso para consultar caja.</p>}
          {cash.state.status === 'ready' && <div className="home-cash-status"><span className={'home-status-pill ' + (cash.state.data.items.some(item => item.openSessionId) ? 'open' : 'closed')}>
            {cash.state.data.items.some(item => item.openSessionId) ? '● Abierta' : '● Cerrada'}</span><span>{cash.state.data.items.some(item => item.openSessionId) ? 'Tenés una sesión disponible.' : cash.state.data.items.length ? 'Abrí una sesión para vender.' : 'Sin cajas configuradas.'}</span></div>}
        </section>
        <section className="home-panel home-attention" aria-label="Requiere atención">
          <header className="home-panel-heading"><h2>Requiere atención</h2></header>
          {stock.state.status === 'ready' && stock.state.data.missingSellable && <a className="home-attention-row" href={routeHref('stock')}><span aria-hidden="true">!</span><span><strong>Ubicación de venta pendiente</strong><small>Revisá las ubicaciones de esta sucursal</small></span><span>→</span></a>}
          {stock.state.status === 'ready' && stock.state.data.count > 0 && <a className="home-attention-row" href={stock.state.data.href}><span aria-hidden="true">▤</span><span><strong>{stock.state.data.count} variantes con stock bajo</strong><small>Según los mínimos configurados</small></span><span>→</span></a>}
          {fiados.state.status === 'ready' && fiados.state.data.summary.filter(row => row.pendingCount > 0).map(row => <a className="home-attention-row" key={row.currency} href={routeHref('fiados')}><span aria-hidden="true">◷</span><span><strong>Fiados pendientes · {amount(row.outstanding,row.currency)}</strong><small>{row.pendingCount} {row.pendingCount === 1 ? 'cuenta por cobrar' : 'cuentas por cobrar'}</small></span><span>→</span></a>)}
          {purchases.state.status === 'ready' && purchases.state.data.summary.orderCount > 0 && <a className="home-attention-row" href="#/compras/ordenes?vista=pendientes"><span aria-hidden="true">↓</span><span><strong>Compras por recibir</strong><small>{purchases.state.data.summary.orderCount} órdenes pendientes</small></span><span>→</span></a>}
          {[['stock',stock],['fiados',fiados],['compras',purchases]].map(([label, resource]) => {
            const read = resource as typeof stock | typeof fiados | typeof purchases;
            return read.state.status === 'error' ? <p key={label as string} className="home-inline-read">{label as string}: {read.state.message} <button type="button" onClick={read.retry}>Reintentar {label as string}</button></p> : null;
          })}
          {!attentionKnown && [stock,fiados,purchases].some(read => read.state.status === 'loading') && <p role="status">Consultando pendientes…</p>}
          {attentionKnown && !needsAttention && <p className="home-attention-empty">Sin alertas en las consultas disponibles.</p>}
        </section>
      </div>
    </div>
  </>;
}

export function Home({ branch, loading, onCreate, cart }: { branch: Store | undefined; loading: boolean; onCreate: () => void; cart?: { units: number; total: string } }) {
  return <section className="home-view" aria-label="Resumen operativo">
    {loading && <p role="status">Cargando sucursales…</p>}
    {!loading && !branch && <p>No tenés sucursales asignadas.</p>}
    {branch && <Dashboard key={branch.id} branch={branch} onCreate={onCreate} cart={cart} />}
  </section>;
}
