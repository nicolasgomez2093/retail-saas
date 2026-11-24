import { useEffect, useId, useMemo, useState, useSyncExternalStore, type FormEvent } from 'react';
import {
  api, type ReceivableStatus, type SaleHistoryPage, type SaleMethodFilter, type SalePaymentMethod, type SalesMarginFilters,
  type SalesMarginPage, type Store,
} from '../../shared/api/client';
import { navigationAccess, navigationAccessKey, navigationAccessVersion, subscribeNavigationAccess } from '../../shared/api/navigationAccess';
import { minorUnits } from '../../shared/lib/decimal';
import { routeHref } from '../../app/routes';
import { posPrice } from '../pos/priceDisplay';
import { paymentLabels } from '../pos/SalePayments';
import { BarList, DailyColumns } from './charts';
import { CollectionsView } from './CollectionsView';
import { amount, methodLabel, money, percent, shortDate, units } from './format';
import { PurchasesView } from './PurchasesView';
import { ReceivablesView } from './ReceivablesView';
import { dateKey, shiftDay } from './salesWindow';
import '../purchases/purchases.css';
import './reports.css';

const linePageSize = 25;
const salePageSize = 8;
type View = 'sales' | 'collections' | 'purchases' | 'receivables';
type Period = 'today' | 'yesterday' | 'week' | 'month30' | 'month' | 'custom';
const views: { id: View; label: string; hint: string }[] = [
  { id: 'sales', label: 'Ventas', hint: 'Lo vendido, fiado incluido' },
  { id: 'collections', label: 'Cobros', hint: 'El dinero que entró y salió' },
  { id: 'purchases', label: 'Compras', hint: 'La mercadería recibida' },
  { id: 'receivables', label: 'Fiados', hint: 'Lo que deben los clientes' },
];
const periods: { id: Period; label: string }[] = [
  { id: 'today', label: 'Hoy' }, { id: 'yesterday', label: 'Ayer' }, { id: 'week', label: '7 días' },
  { id: 'month30', label: '30 días' }, { id: 'month', label: 'Este mes' }, { id: 'custom', label: 'Personalizado' },
];
const methodOrder: SalePaymentMethod[] = ['cash', 'debit', 'credit', 'transfer', 'qr'];
const statuses: { id: ReceivableStatus; label: string }[] = [
  { id: 'pending', label: 'Pendientes' }, { id: 'partial', label: 'Con abonos' }, { id: 'settled', label: 'Saldados' },
];

function range(period: Period, today: string, custom: { fromDate: string; toDate: string }) {
  switch (period) {
    case 'today': return { fromDate: today, toDate: today };
    case 'yesterday': return { fromDate: shiftDay(today, -1), toDate: shiftDay(today, -1) };
    case 'week': return { fromDate: shiftDay(today, -6), toDate: today };
    case 'month30': return { fromDate: shiftDay(today, -29), toDate: today };
    case 'month': return { fromDate: today.slice(0, 8) + '01', toDate: today };
    case 'custom': return custom;
  }
}

export function SalesMarginReport({ stores, revision, active, reportDay }: { stores: Store[]; revision: number; active: boolean; reportDay: string }) {
  const store = stores[0];
  if (!store) return null;
  return <Reports key={store.id} store={store} revision={revision} active={active} reportDay={reportDay} />;
}

function Reports({ store, revision, active, reportDay }: { store: Store; revision: number; active: boolean; reportDay: string }) {
  const uid = useId();
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  const [timezone, setTimezone] = useState(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
  const today = dateKey(new Date(), timezone);
  const [view, setView] = useState<View>('sales');
  const [period, setPeriod] = useState<Period>('month30');
  const [custom, setCustom] = useState({ fromDate: '', toDate: '' });
  const [query, setQuery] = useState('');
  const [method, setMethod] = useState<SaleMethodFilter>('');
  const [status, setStatus] = useState<ReceivableStatus>('pending');
  const [formError, setFormError] = useState('');
  const purchasesDenied = navigationAccess(navigationAccessKey('/purchase-orders', store.id)) === 'denied';
  const shownViews = views.filter((entry) => entry.id !== 'purchases' || !purchasesDenied);
  const currentView = shownViews.some((entry) => entry.id === view) ? view : 'sales';

  const dates = reportDay ? { fromDate: reportDay, toDate: reportDay } : range(period, today, custom);
  const filters: SalesMarginFilters = useMemo(() => ({ fromDate: dates.fromDate, toDate: dates.toDate, query: reportDay ? '' : query, method }),
    [dates.fromDate, dates.toDate, reportDay, query, method]);
  const collectionFilters = useMemo(() => ({ fromDate: dates.fromDate, toDate: dates.toDate, method: method === 'on_credit' ? '' as const : method }),
    [dates.fromDate, dates.toDate, method]);
  const periodFilters = useMemo(() => ({ fromDate: dates.fromDate, toDate: dates.toDate }), [dates.fromDate, dates.toDate]);

  function choose(next: Period) {
    setPeriod(next);
    setFormError('');
    if (next === 'custom' && !custom.fromDate) setCustom({ fromDate: shiftDay(today, -29), toDate: today });
  }

  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const next = { fromDate: String(fields.get('fromDate') ?? ''), toDate: String(fields.get('toDate') ?? '') };
    if (period === 'custom' && (!next.fromDate || !next.toDate)) return setFormError('Elegí las dos fechas del período.');
    if (period === 'custom' && next.fromDate > next.toDate) return setFormError('La fecha Desde debe ser igual o anterior a la fecha Hasta.');
    setFormError('');
    if (period === 'custom') setCustom(next);
    setQuery(String(fields.get('query') ?? '').trim());
  }

  const periodLabel = !dates.fromDate ? 'Elegí las fechas' : dates.fromDate === dates.toDate
    ? (dates.fromDate === today ? 'Hoy, ' : dates.fromDate === shiftDay(today, -1) ? 'Ayer, ' : '') + shortDate(dates.fromDate)
    : `Del ${shortDate(dates.fromDate)} al ${shortDate(dates.toDate)}`;
  const selection = [views.find((entry) => entry.id === currentView)?.label, periodLabel,
    currentView === 'sales' && method ? methodLabel(method) : '',
    currentView === 'collections' && collectionFilters.method ? methodLabel(collectionFilters.method) : '',
    currentView === 'receivables' ? statuses.find((entry) => entry.id === status)?.label : '',
    currentView === 'sales' && query ? `«${query}»` : ''].filter(Boolean).join(' · ');
  const methodChoices: SaleMethodFilter[] = currentView === 'sales' ? ['', ...methodOrder, 'on_credit'] : ['', ...methodOrder];
  const selectedMethod = currentView === 'sales' ? method : collectionFilters.method;

  return <section className="pu sales-report" aria-labelledby={uid}>
    <header className="pu-head">
      <div>
        <h2 id={uid}>Reportes</h2>
        <p>{store.name} · <span className="rp-selection">{selection}</span></p>
      </div>
    </header>

    <nav className="rp-views" aria-label="Qué querés ver">
      {shownViews.map((entry) => <button key={entry.id} type="button" aria-pressed={currentView === entry.id}
        onClick={() => setView(entry.id)}><b>{entry.label}</b><small>{entry.hint}</small></button>)}
    </nav>

    <div className="pu-card sr-filters">
      {reportDay ? <div className="pu-filters">
        <p className="sr-day">Operaciones del <time dateTime={reportDay}>{shortDate(reportDay)}</time></p>
        <a className="pu-btn" href={routeHref('reportes')}>Elegir otro período</a>
      </div> : <form className="pu-filters pu-form" aria-label="Período del reporte" onSubmit={apply}>
        <span className="rp-filter-label" aria-hidden="true">Período</span>
        <div className="sr-periods" role="group" aria-label="Período">
          {periods.map((entry) => <button key={entry.id} type="button" className="pu-chip" aria-pressed={period === entry.id}
            onClick={() => choose(entry.id)}>{entry.label}</button>)}
        </div>
        {period === 'custom' && <>
          <label className="sr-date">Desde<input name="fromDate" type="date" max="9999-12-31" defaultValue={custom.fromDate} key={'from' + custom.fromDate} /></label>
          <label className="sr-date">Hasta<input name="toDate" type="date" max="9999-12-31" defaultValue={custom.toDate} key={'to' + custom.toDate} /></label>
        </>}
        {currentView === 'sales' && <label className="pu-search"><span aria-hidden="true">⌕</span>
          <input name="query" type="search" maxLength={120} defaultValue={query} placeholder="Filtrar por SKU o producto" aria-label="Filtrar por SKU o producto" />
        </label>}
        {(period === 'custom' || currentView === 'sales') && <button className="pu-btn primary">Aplicar</button>}
      </form>}
      {(currentView === 'sales' || currentView === 'collections') && <div className="pu-filters rp-secondary" role="group" aria-label="Medio de pago">
        <span className="rp-filter-label" aria-hidden="true">Medio</span>
        {methodChoices.map((entry) => <button key={entry || 'all'} type="button" className="pu-chip"
          aria-pressed={selectedMethod === entry} onClick={() => setMethod(entry)}>{entry ? methodLabel(entry) : 'Todos'}</button>)}
      </div>}
      {currentView === 'receivables' && <div className="pu-filters rp-secondary" role="group" aria-label="Estado del fiado">
        <span className="rp-filter-label" aria-hidden="true">Estado</span>
        {statuses.map((entry) => <button key={entry.id} type="button" className="pu-chip" aria-pressed={status === entry.id}
          onClick={() => setStatus(entry.id)}>{entry.label}</button>)}
      </div>}
      {formError && <p className="pu-alert sr-inline" role="alert">{formError}</p>}
    </div>

    {currentView === 'sales' && <SalesView store={store} filters={filters} revision={revision} active={active} onTimezone={setTimezone} />}
    {currentView === 'collections' && <CollectionsView store={store} filters={collectionFilters} revision={revision} active={active} onTimezone={setTimezone} />}
    {currentView === 'purchases' && <PurchasesView store={store} filters={periodFilters} revision={revision} active={active} />}
    {currentView === 'receivables' && <ReceivablesView store={store} filters={periodFilters} status={status} revision={revision} active={active} />}
  </section>;
}

function SalesView({ store, filters, revision, active, onTimezone }: {
  store: Store; filters: SalesMarginFilters; revision: number; active: boolean; onTimezone: (timezone: string) => void;
}) {
  const [lineOffset, setLineOffset] = useState(0);
  const [saleOffset, setSaleOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const filterKey = JSON.stringify(filters);
  const [pagedFor, setPagedFor] = useState(filterKey);
  if (pagedFor !== filterKey) {
    setPagedFor(filterKey);
    setLineOffset(0);
    setSaleOffset(0);
  }
  const reportKey = JSON.stringify([store.id, filters, lineOffset, revision, retry]);
  const historyKey = JSON.stringify([store.id, filters, saleOffset, revision, retry]);
  const [report, setReport] = useState<{ key: string; page?: SalesMarginPage; error?: string }>();
  const [history, setHistory] = useState<{ key: string; page?: SaleHistoryPage; error?: string }>();
  const currentReport = report?.key === reportKey ? report : undefined;
  const currentHistory = history?.key === historyKey ? history : undefined;
  // Keep the last figures on screen while the next selection loads.
  const [shown, setShown] = useState<SalesMarginPage>();
  const page = currentReport?.page ?? shown;

  useEffect(() => {
    if (!active || !filters.fromDate) return;
    let live = true;
    api.salesMarginReport(store.id, linePageSize, lineOffset, filters).then((result) => {
      if (!live) return;
      setReport({ key: reportKey, page: result });
      setShown(result);
      onTimezone(result.timezone);
    }, (cause: unknown) => {
      if (live) setReport({ key: reportKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar el reporte.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, lineOffset, reportKey, onTimezone]);

  useEffect(() => {
    if (!active || !filters.fromDate) return;
    let live = true;
    api.saleHistory(store.id, '', saleOffset, salePageSize, filters).then((result) => {
      if (live) setHistory({ key: historyKey, page: result });
    }, (cause: unknown) => {
      if (live) setHistory({ key: historyKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar las ventas.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, saleOffset, historyKey]);

  async function downloadCSV() {
    setExporting(true);
    setExportError('');
    try {
      const data = await api.downloadSalesMarginCSV(store.id, filters);
      const url = URL.createObjectURL(data);
      const link = document.createElement('a');
      link.href = url;
      link.download = `ventas-${filters.fromDate}-a-${filters.toDate}.csv`;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : 'No pudimos descargar el reporte.');
    } finally {
      setExporting(false);
    }
  }

  const dates = page && new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: page.timezone });
  const loading = active && !currentReport;
  const activity = page?.activity ?? [];
  const currency = activity[0]?.currency ?? page?.summary[0]?.currency ?? 'ARS';
  const categories = useMemo(() => {
    const totals = new Map<string, { name: string; currency: string; cents: bigint }>();
    for (const item of page?.categoryTotals ?? []) {
      const key = item.currency + ':' + (item.categoryId ?? '');
      const entry = totals.get(key) ?? { name: item.categoryName ?? 'Sin categoría', currency: item.currency, cents: 0n };
      entry.cents += minorUnits(item.netSales);
      totals.set(key, entry);
    }
    return [...totals.values()].sort((a, b) => (b.cents > a.cents ? 1 : b.cents < a.cents ? -1 : 0));
  }, [page]);
  const filtered = Boolean(filters.method || filters.query);

  return <div className="rp-body" aria-busy={loading} data-stale={(loading && Boolean(page)) || undefined}>
    {exportError && <p className="pu-alert" role="alert">{exportError}</p>}
    {currentReport?.error && <div className="pu-card pu-panelbody sr-block">
      <p className="pu-alert" role="alert">{currentReport.error}</p>
      <button type="button" className="pu-btn" onClick={() => setRetry((value) => value + 1)}>Reintentar</button>
    </div>}
    {loading && !page && <p className="pu-status" role="status">Consultando ventas…</p>}

    {page && <>
      {(activity.length ? activity : [undefined]).map((total) => <div className="pu-metrics" key={total?.currency ?? 'empty'}>
        <div className="pu-metric">
          <small>Facturación neta</small>
          <strong className="sr-money">{money(total?.currency ?? currency, total?.netSales ?? '0.00')}</strong>
          <span>{total && (total.returnCount || total.exchangeCount)
            ? `Vendido ${posPrice(total.grossSales)} · devuelto −${posPrice(total.returnedAmount)} · cambios +${posPrice(total.exchangeAmount)}`
            : filters.method === 'on_credit' ? 'Ventas fiadas del período' : filtered ? 'Ventas que coinciden con el filtro' : 'Ventas del período, fiado incluido'}</span>
        </div>
        <div className="pu-metric">
          <small>Ventas</small>
          <strong>{(total?.saleCount ?? 0).toLocaleString('es-AR')}</strong>
          <span>{units(total?.unitsSold ?? 0)} vendidas{total?.returnedUnits ? ` · ${total.returnedUnits} devueltas` : ''}{total?.exchangedUnits ? ` · ${total.exchangedUnits} entregadas en cambios` : ''}</span>
        </div>
        <div className="pu-metric">
          <small>Ticket promedio</small>
          <strong className="sr-money">{total?.averageTicket ? money(total.currency, total.averageTicket) : '—'}</strong>
          <span>Importe cobrado por venta, antes de cambios</span>
        </div>
      </div>)}
      {filters.method && filters.method !== 'on_credit' && <p className="pu-note rp-note">Se muestran las ventas cobradas, al menos en parte, con {methodLabel(filters.method).toLowerCase()}: una venta mixta suma completa. Para ver solo el dinero que entró por ese medio, abrí <b>Cobros</b>.</p>}

      <div className="pu-card pu-panelbody sr-block">
        <div className="sr-title"><h3>Ventas por día</h3><span className="rp-legend"><i aria-hidden="true" />Importe vendido</span></div>
        <DailyColumns title="Importe vendido por día" fromDate={filters.fromDate} toDate={filters.toDate} emptyText="No hay ventas en este período."
          days={page.dailyTotals.filter((day) => day.currency === currency).map((day) => ({
            date: day.date, value: amount(day.netSales), label: money(day.currency, day.netSales),
            detail: `${day.saleCount} ${day.saleCount === 1 ? 'venta' : 'ventas'} · ${units(day.unitsSold)}`,
          }))} />
      </div>

      <div className="sr-grid">
        <div className="pu-card pu-panelbody">
          <div className="sr-title"><h3>Ventas registradas</h3><a href={routeHref('ventas')}>Ver historial completo →</a></div>
          {!currentHistory && <p className="pu-hint" role="status">Consultando ventas…</p>}
          {currentHistory?.error && <p className="pu-alert" role="alert">{currentHistory.error}</p>}
          {currentHistory?.page && (currentHistory.page.items.length === 0
            ? <div className="pu-empty sr-empty"><p>No hay ventas para analizar en este período.</p></div>
            : <ul className="sr-list">{currentHistory.page.items.map((sale) => <li key={sale.id} className="pu-statrow">
              <div><b>V-{sale.number}</b>
                <span className="sub">{units(sale.units ?? 0)} · {[...(sale.payments ?? []).map((entry) => paymentLabels[entry]), ...(sale.onCredit ? ['Fiado'] : [])].join(' + ') || 'Sin pago'}
                  {sale.returns ? ` · ${sale.returns} ${sale.returns === 1 ? 'cambio o devolución' : 'cambios o devoluciones'}` : ''}</span></div>
              <div className="sr-amount"><b>{money(sale.currency, sale.total)}</b><span className="sub">{dates?.format(new Date(sale.createdAt))}</span></div>
            </li>)}</ul>)}
          {currentHistory?.page && (saleOffset > 0 || currentHistory.page.hasMore) && <nav className="pu-pager sr-pager" aria-label="Páginas de ventas registradas">
            <button type="button" className="pu-btn" disabled={saleOffset === 0} onClick={() => setSaleOffset(saleOffset - salePageSize)}>← Anteriores</button>
            <button type="button" className="pu-btn" disabled={!currentHistory.page.hasMore} onClick={() => setSaleOffset(saleOffset + salePageSize)}>Siguientes →</button>
          </nav>}
        </div>
        <div className="pu-card pu-panelbody">
          <h3>Productos más vendidos</h3>
          <BarList label="Productos más vendidos" emptyText="Todavía no hay datos de ventas en este período."
            rows={page.topProducts.map((product) => ({
              key: product.currency + product.productId, label: product.product, value: product.netUnits,
              display: product.netUnits.toLocaleString('es-AR') + ' u.',
              sub: product.returnedUnits > 0 ? `${product.unitsSold} vendidas · ${product.returnedUnits} devueltas` : money(product.currency, product.netSales),
            }))} />
          <div className="pu-note">Unidades que se llevaron los clientes: ventas y cambios entregados, menos devoluciones del período.</div>
        </div>
      </div>

      <div className="sr-grid even">
        <div className="pu-card pu-panelbody">
          <h3>Medios de pago</h3>
          <BarList label="Medios de pago" emptyText="Sin cobros en este período."
            rows={page.paymentTotals.map((payment) => ({
              key: payment.currency + payment.method, label: payment.method === 'on_credit' ? 'Fiado' : paymentLabels[payment.method],
              value: amount(payment.amount), display: money(payment.currency, payment.amount),
              sub: `${payment.saleCount} ${payment.saleCount === 1 ? 'venta' : 'ventas'}`,
            }))} />
          <p className="pu-hint">Cobrado al confirmar cada venta, incluida la seña de reservas y el saldo fiado inicial.</p>
        </div>
        <div className="pu-card pu-panelbody">
          <h3>Ventas por categoría</h3>
          <BarList label="Ventas por categoría" emptyText="Sin ventas en este período."
            rows={categories.map((entry) => {
              const total = categories.filter((other) => other.currency === entry.currency).reduce((sum, other) => sum + other.cents, 0n);
              const share = total > 0n && entry.cents >= 0n ? Number(entry.cents * 100n / total) : null;
              return {
                key: entry.currency + entry.name, label: entry.name, value: Number(entry.cents),
                display: money(entry.currency, (Number(entry.cents) / 100).toFixed(2)), sub: share === null ? undefined : share + ' % del total',
              };
            })} />
          <p className="pu-hint">Según la categoría actual de cada producto en el catálogo.</p>
        </div>
      </div>

      <div className="sr-grid even">
        <div className="pu-card pu-panelbody">
          <h3>Margen bruto</h3>
          {page.summary.length === 0 ? <p className="pu-hint">Sin ventas en este período.</p> : page.summary.map((total) => <ul key={total.currency} className="sr-list">
            <li className="pu-statrow"><span>Ventas con costo cargado</span><b>{money(total.currency, total.netSalesWithCost)}</b></li>
            <li className="pu-statrow"><span>Costo de la mercadería</span><b>{money(total.currency, total.costOfGoodsWithCost)}</b></li>
            <li className="pu-statrow"><span>Ganancia bruta</span><b>{money(total.currency, total.grossProfitWithCost)}</b></li>
            <li className="pu-statrow"><span>Margen</span><b>{percent(total.grossMarginPercentWithCost)}</b></li>
            <li className="pu-statrow"><span>Líneas con costo</span><b>{total.lineCount - total.unknownCostLines} de {total.lineCount}</b></li>
          </ul>)}
          <p className="pu-hint">Con el costo guardado al vender. Las líneas sin costo no entran en el margen y no se descuentan devoluciones.</p>
        </div>
        <div className="pu-card sr-daily">
          <div className="pu-panelhead"><div><h3>Detalle por día</h3><p>Hasta 31 días con ventas, del más reciente al más antiguo.</p></div></div>
          {page.dailyTotals.length === 0 ? <p className="pu-status">Sin ventas en este período.</p> : <div className="pu-table-wrap"><table className="pu-table">
            <thead><tr><th scope="col">Fecha</th><th scope="col" className="num">Ventas</th><th scope="col" className="num">Unidades</th><th scope="col" className="num">Importe</th></tr></thead>
            <tbody>{page.dailyTotals.map((day) => <tr key={day.date + day.currency}>
              <td data-label="Fecha"><time dateTime={day.date}>{shortDate(day.date)}</time></td>
              <td className="num" data-label="Ventas">{day.saleCount.toLocaleString('es-AR')}</td>
              <td className="num" data-label="Unidades">{day.unitsSold.toLocaleString('es-AR')}</td>
              <td className="num" data-label="Importe">{money(day.currency, day.netSales)}</td>
            </tr>)}</tbody>
          </table></div>}
        </div>
      </div>

      <div className="pu-card sr-block">
        <div className="pu-panelhead"><div><h3>Detalle por producto vendido</h3><p>Cada línea de venta con su costo y margen.</p></div>
          <button type="button" className="pu-btn" disabled={exporting || !currentReport?.page} onClick={() => void downloadCSV()}>
            {exporting ? 'Preparando CSV…' : '↓ Exportar CSV'}
          </button></div>
        {page.items.length === 0 ? <p className="pu-status">No hay ventas para mostrar.</p> : <div className="pu-table-wrap"><table className="pu-table">
          <thead><tr><th scope="col">Venta</th><th scope="col">Producto</th><th scope="col" className="num">Unidades</th><th scope="col" className="num">Importe</th>
            <th scope="col" className="num">Costo</th><th scope="col" className="num">Ganancia</th><th scope="col" className="num">Margen</th></tr></thead>
          <tbody>{page.items.map((line) => <tr key={line.saleId + line.variantId}>
            <td><b>V-{line.saleNumber}</b><small>{dates?.format(new Date(line.soldAt))}</small></td>
            <td>{line.product}<small>SKU {line.sku}</small></td>
            <td className="num" data-label="Unidades">{line.quantity}</td>
            <td className="num" data-label="Importe">{money(line.currency, line.netSales)}</td>
            <td className="num" data-label="Costo">{money(line.currency, line.costOfGoods)}</td>
            <td className="num" data-label="Ganancia">{money(line.currency, line.grossProfit)}</td>
            <td className="num" data-label="Margen">{percent(line.grossMarginPercent)}</td>
          </tr>)}</tbody>
        </table></div>}
        {(lineOffset > 0 || page.hasMore) && <nav className="pu-pager sr-pager" aria-label="Páginas del detalle">
          <button type="button" className="pu-btn" disabled={lineOffset === 0} onClick={() => setLineOffset(lineOffset - linePageSize)}>← Anterior</button>
          <button type="button" className="pu-btn" disabled={!currentReport?.page?.hasMore} onClick={() => setLineOffset(lineOffset + linePageSize)}>Siguiente →</button>
        </nav>}
      </div>
    </>}
  </div>;
}
