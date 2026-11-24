import { useEffect, useId, useMemo, useState, type FormEvent } from 'react';
import { api, type SaleHistoryPage, type SalesMarginFilters, type SalesMarginPage, type Store } from '../../shared/api/client';
import { minorUnits } from '../../shared/lib/decimal';
import { routeHref } from '../../app/routes';
import { posPrice } from '../pos/priceDisplay';
import { paymentLabels } from '../pos/SalePayments';
import { dateKey, shiftDay } from './salesWindow';
import '../purchases/purchases.css';
import './reports.css';

const linePageSize = 25;
const salePageSize = 8;
type Period = 'today' | 'week' | 'month30' | 'month' | 'custom';
const periods: { id: Period; label: string }[] = [
  { id: 'today', label: 'Hoy' }, { id: 'week', label: '7 días' }, { id: 'month30', label: '30 días' },
  { id: 'month', label: 'Este mes' }, { id: 'custom', label: 'Personalizado' },
];

function money(currency: string, value: string | null | undefined) {
  return value === null || value === undefined ? 'No disponible' : currency + ' ' + posPrice(value);
}
function percent(value: string | null) {
  return value === null ? 'No disponible' : Number(value).toLocaleString('es-AR', { maximumFractionDigits: 1 }) + ' %';
}
function shortDate(day: string) {
  return day.split('-').reverse().join('/');
}
function units(count: number) {
  return count.toLocaleString('es-AR') + (count === 1 ? ' unidad' : ' unidades');
}

function range(period: Period, today: string, custom: { fromDate: string; toDate: string }) {
  switch (period) {
    case 'today': return { fromDate: today, toDate: today };
    case 'week': return { fromDate: shiftDay(today, -6), toDate: today };
    case 'month30': return { fromDate: shiftDay(today, -29), toDate: today };
    case 'month': return { fromDate: today.slice(0, 8) + '01', toDate: today };
    case 'custom': return custom;
  }
}

export function SalesMarginReport({ stores, revision, active, reportDay }: { stores: Store[]; revision: number; active: boolean; reportDay: string }) {
  const store = stores[0];
  if (!store) return null;
  return <SalesReport key={store.id} store={store} revision={revision} active={active} reportDay={reportDay} />;
}

function SalesReport({ store, revision, active, reportDay }: { store: Store; revision: number; active: boolean; reportDay: string }) {
  const uid = useId();
  const [timezone, setTimezone] = useState(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
  const today = dateKey(new Date(), timezone);
  const [period, setPeriod] = useState<Period>('month30');
  const [custom, setCustom] = useState({ fromDate: '', toDate: '' });
  const [query, setQuery] = useState('');
  const [formError, setFormError] = useState('');
  const [lineOffset, setLineOffset] = useState(0);
  const [saleOffset, setSaleOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');

  const filters: SalesMarginFilters = useMemo(() => reportDay
    ? { fromDate: reportDay, toDate: reportDay, query: '' }
    : { ...range(period, today, custom), query }, [reportDay, period, today, custom, query]);
  const reportKey = JSON.stringify([store.id, filters, lineOffset, revision, retry]);
  const historyKey = JSON.stringify([store.id, filters, saleOffset, revision, retry]);
  const [report, setReport] = useState<{ key: string; page?: SalesMarginPage; error?: string }>();
  const [history, setHistory] = useState<{ key: string; page?: SaleHistoryPage; error?: string }>();
  const currentReport = report?.key === reportKey ? report : undefined;
  const currentHistory = history?.key === historyKey ? history : undefined;
  // Keep the last figures on screen while the next period loads.
  const [shown, setShown] = useState<SalesMarginPage>();
  const page = currentReport?.page ?? shown;

  useEffect(() => {
    if (!active) return;
    let live = true;
    api.salesMarginReport(store.id, linePageSize, lineOffset, filters).then((result) => {
      if (!live) return;
      setReport({ key: reportKey, page: result });
      setShown(result);
      if (result.timezone !== timezone) setTimezone(result.timezone);
    }, (cause: unknown) => {
      if (live) setReport({ key: reportKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar el reporte.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, lineOffset, reportKey, timezone]);

  useEffect(() => {
    if (!active) return;
    let live = true;
    api.saleHistory(store.id, '', saleOffset, salePageSize, filters).then((result) => {
      if (live) setHistory({ key: historyKey, page: result });
    }, (cause: unknown) => {
      if (live) setHistory({ key: historyKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar las ventas.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, saleOffset, historyKey]);

  function choose(next: Period) {
    setPeriod(next);
    setFormError('');
    setLineOffset(0);
    setSaleOffset(0);
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
    setLineOffset(0);
    setSaleOffset(0);
  }

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

  const periodLabel = filters.fromDate === filters.toDate
    ? (filters.fromDate === today ? 'Hoy, ' : '') + shortDate(filters.fromDate)
    : `Del ${shortDate(filters.fromDate)} al ${shortDate(filters.toDate)}`;
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

  return <section className="pu sales-report" aria-labelledby={uid} aria-busy={loading}>
    <header className="pu-head">
      <div>
        <h2 id={uid}>Reportes</h2>
        <p>Indicadores comerciales de {store.name} · {periodLabel}.</p>
      </div>
      <div className="pu-head-actions">
        <button type="button" className="pu-btn" disabled={exporting || !currentReport?.page} onClick={() => void downloadCSV()}>
          {exporting ? 'Preparando CSV…' : '↓ Exportar ventas CSV'}
        </button>
      </div>
    </header>
    {exportError && <p className="pu-alert" role="alert">{exportError}</p>}

    <div className="pu-card sr-filters">
      {reportDay ? <div className="pu-filters">
        <p className="sr-day">Ventas del <time dateTime={reportDay}>{shortDate(reportDay)}</time></p>
        <a className="pu-btn" href={routeHref('reportes')}>Elegir otro período</a>
      </div> : <form className="pu-filters pu-form" aria-label="Período del reporte" onSubmit={apply}>
        <div className="sr-periods" role="group" aria-label="Período">
          {periods.map((entry) => <button key={entry.id} type="button" className="pu-chip" aria-pressed={period === entry.id}
            onClick={() => choose(entry.id)}>{entry.label}</button>)}
        </div>
        {period === 'custom' && <>
          <label className="sr-date">Desde<input name="fromDate" type="date" max="9999-12-31" defaultValue={custom.fromDate} key={'from' + custom.fromDate} /></label>
          <label className="sr-date">Hasta<input name="toDate" type="date" max="9999-12-31" defaultValue={custom.toDate} key={'to' + custom.toDate} /></label>
        </>}
        <label className="pu-search"><span aria-hidden="true">⌕</span>
          <input name="query" type="search" maxLength={120} defaultValue={query} placeholder="Filtrar por SKU o producto" aria-label="Filtrar por SKU o producto" />
        </label>
        <button className="pu-btn primary">Aplicar</button>
      </form>}
      {formError && <p className="pu-alert sr-inline" role="alert">{formError}</p>}
    </div>

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
            : 'Ventas del período, fiado incluido'}</span>
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

      <div className="sr-grid">
        <div className="pu-card pu-panelbody">
          <div className="sr-title"><h3>Ventas registradas</h3><a href={routeHref('ventas')}>Ver historial completo →</a></div>
          {!currentHistory && <p className="pu-hint" role="status">Consultando ventas…</p>}
          {currentHistory?.error && <p className="pu-alert" role="alert">{currentHistory.error}</p>}
          {currentHistory?.page && (currentHistory.page.items.length === 0
            ? <div className="pu-empty sr-empty"><p>No hay ventas para analizar en este período.</p></div>
            : <ul className="sr-list">{currentHistory.page.items.map((sale) => <li key={sale.id} className="pu-statrow">
              <div><b>V-{sale.number}</b>
                <span className="sub">{units(sale.units ?? 0)} · {[...(sale.payments ?? []).map((method) => paymentLabels[method]), ...(sale.onCredit ? ['Fiado'] : [])].join(' + ') || 'Sin pago'}
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
          {page.topProducts.length === 0 ? <p className="pu-hint">Todavía no hay datos de ventas en este período.</p>
            : <ul className="sr-list">{page.topProducts.map((product) => <li key={product.currency + product.productId} className="pu-statrow">
              <div><span>{product.product}</span>
                {product.returnedUnits > 0 && <span className="sub">{product.unitsSold} vendidas · {product.returnedUnits} devueltas</span>}</div>
              <b>{product.netUnits.toLocaleString('es-AR')} u.</b>
            </li>)}</ul>}
          <div className="pu-note">Unidades que se llevaron los clientes: ventas y cambios entregados, menos devoluciones del período.</div>
        </div>
      </div>

      <div className="sr-grid even">
        <div className="pu-card pu-panelbody">
          <h3>Medios de pago</h3>
          {page.paymentTotals.length === 0 ? <p className="pu-hint">Sin cobros en este período.</p>
            : <ul className="sr-list">{page.paymentTotals.map((payment) => <li key={payment.currency + payment.method} className="pu-statrow">
              <div><span>{payment.method === 'on_credit' ? 'Fiado' : paymentLabels[payment.method]}</span>
                <span className="sub">{payment.saleCount} {payment.saleCount === 1 ? 'venta' : 'ventas'}</span></div>
              <b>{money(payment.currency, payment.amount)}</b>
            </li>)}</ul>}
          <p className="pu-hint">Cobrado al confirmar cada venta, incluida la seña de reservas y el saldo fiado inicial.</p>
        </div>
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
      </div>

      <div className="sr-grid even">
        <div className="pu-card sr-daily">
          <div className="pu-panelhead"><div><h3>Ventas por día</h3><p>Hasta 31 días con ventas, del más reciente al más antiguo.</p></div></div>
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
        <div className="pu-card pu-panelbody">
          <h3>Ventas por categoría</h3>
          {categories.length === 0 ? <p className="pu-hint">Sin ventas en este período.</p>
            : <ul className="sr-list">{categories.map((entry) => {
              const total = categories.filter((other) => other.currency === entry.currency).reduce((sum, other) => sum + other.cents, 0n);
              const share = total > 0n && entry.cents >= 0n ? Number(entry.cents * 100n / total) : null;
              return <li key={entry.currency + entry.name} className="pu-statrow sr-category">
                <div><span>{entry.name}</span>{share !== null && <span className="sub">{share} % del total</span>}
                  {share !== null && <span className="pu-progress" aria-hidden="true"><span style={{ width: share + '%' }} /></span>}</div>
                <b>{money(entry.currency, (Number(entry.cents) / 100).toFixed(2))}</b>
              </li>;
            })}</ul>}
          <p className="pu-hint">Según la categoría actual de cada producto en el catálogo.</p>
        </div>
      </div>

      <div className="pu-card sr-block">
        <div className="pu-panelhead"><div><h3>Detalle por producto vendido</h3><p>Cada línea de venta con su costo y margen.</p></div></div>
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
  </section>;
}
