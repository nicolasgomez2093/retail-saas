import { useEffect, useState } from 'react';
import { api, ApiError, type PurchasesReport, type Store } from '../../shared/api/client';
import { routeHref } from '../../app/routes';
import { BarList, DailyColumns } from './charts';
import { amount, money, plural, units } from './format';

const pageSize = 15;

export function PurchasesView({ store, filters, revision, active }: {
  store: Store; filters: { fromDate: string; toDate: string }; revision: number; active: boolean;
}) {
  const [offset, setOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const filterKey = JSON.stringify(filters);
  const [pagedFor, setPagedFor] = useState(filterKey);
  if (pagedFor !== filterKey) {
    setPagedFor(filterKey);
    setOffset(0);
  }
  const key = JSON.stringify([store.id, filters, offset, revision, retry]);
  const [state, setState] = useState<{ key: string; report?: PurchasesReport; error?: string; denied?: boolean }>();
  const [shown, setShown] = useState<PurchasesReport>();
  const current = state?.key === key ? state : undefined;
  const report = current?.report ?? shown;

  useEffect(() => {
    if (!active || !filters.fromDate) return;
    let live = true;
    api.purchasesReport(store.id, pageSize, offset, filters).then((result) => {
      if (!live) return;
      setState({ key, report: result });
      setShown(result);
    }, (cause: unknown) => {
      if (live) setState({ key, denied: cause instanceof ApiError && cause.status === 403,
        error: cause instanceof Error ? cause.message : 'No pudimos cargar las compras.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, offset, key]);

  if (current?.denied) return <div className="pu-card pu-empty"><p>Tu usuario no tiene permiso para ver compras en esta sucursal.</p></div>;
  const loading = active && !current;
  const summary = report?.summary;
  const currency = report?.currency ?? 'ARS';
  const dates = report && new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: report.timezone });
  const withoutCost = summary ? summary.units - summary.unitsWithCost : 0;

  return <div className="rp-body" aria-busy={loading} data-stale={(loading && Boolean(report)) || undefined}>
    {current?.error && <div className="pu-card pu-panelbody sr-block">
      <p className="pu-alert" role="alert">{current.error}</p>
      <button type="button" className="pu-btn" onClick={() => setRetry((value) => value + 1)}>Reintentar</button>
    </div>}
    {loading && !report && <p className="pu-status" role="status">Consultando compras…</p>}
    {report && summary && <>
      <div className="pu-metrics">
        <div className="pu-metric">
          <small>Costo recibido</small>
          <strong className="sr-money">{money(currency, summary.knownCost)}</strong>
          <span>{withoutCost > 0 ? `${units(withoutCost)} sin costo cargado, no suman` : 'Al costo cargado en cada recepción'}</span>
        </div>
        <div className="pu-metric">
          <small>Mercadería recibida</small>
          <strong>{summary.units.toLocaleString('es-AR')} u.</strong>
          <span>{plural(summary.receiptCount, 'recepción', 'recepciones')}{summary.rejectedUnits ? ` · ${summary.rejectedUnits} rechazadas` : ''}</span>
        </div>
        <div className="pu-metric">
          <small>Órdenes emitidas</small>
          <strong>{summary.placedOrders.toLocaleString('es-AR')}</strong>
          <span>{units(summary.orderedUnits)} pedidas · {plural(summary.pendingOrders, 'orden', 'órdenes')} por recibir hoy</span>
        </div>
      </div>

      <div className="pu-card pu-panelbody sr-block">
        <div className="sr-title"><h3>Recepciones por día</h3><span className="rp-legend"><i aria-hidden="true" />Unidades recibidas</span></div>
        <DailyColumns title="Unidades recibidas por día" fromDate={filters.fromDate} toDate={filters.toDate} emptyText="No se recibió mercadería en este período."
          days={report.daily.map((day) => ({
            date: day.date, value: day.units, label: units(day.units),
            detail: `${plural(day.receiptCount, 'recepción', 'recepciones')} · ${money(currency, day.knownCost)}`,
          }))} />
      </div>

      <div className="sr-grid even">
        <div className="pu-card pu-panelbody">
          <h3>Por proveedor</h3>
          <BarList label="Compras por proveedor" emptyText="Sin recepciones en este período."
            rows={report.suppliers.map((row) => ({
              key: row.supplierId, label: row.supplier, value: amount(row.knownCost) || row.units,
              display: row.knownCost === '0.00' ? units(row.units) : money(currency, row.knownCost), sub: `${units(row.units)} · ${plural(row.receiptCount, 'recepción', 'recepciones')}`,
            }))} />
        </div>
        <div className="pu-card pu-panelbody">
          <h3>Productos recibidos</h3>
          <BarList label="Productos recibidos" emptyText="Sin recepciones en este período."
            rows={report.products.map((row) => ({
              key: row.productId, label: row.product, value: row.units, display: row.units.toLocaleString('es-AR') + ' u.',
              sub: row.knownCost === '0.00' ? 'Sin costo cargado' : money(currency, row.knownCost),
            }))} />
        </div>
      </div>

      <div className="pu-card sr-block">
        <div className="pu-panelhead"><div><h3>Recepciones</h3><p>Mercadería ingresada, de la más reciente a la más antigua.</p></div>
          <a className="pu-btn" href={routeHref('compras')}>Ir a Compras →</a></div>
        {report.items.length === 0 ? <p className="pu-status">No hay recepciones para mostrar.</p> : <div className="pu-table-wrap"><table className="pu-table rp-table">
          <thead><tr><th scope="col">Fecha</th><th scope="col">Orden</th><th scope="col" className="num">Unidades</th><th scope="col" className="num">Costo</th></tr></thead>
          <tbody>{report.items.map((item) => <tr key={item.receiptId}>
            <td>{dates?.format(new Date(item.receivedAt))}<small>{item.locationName}</small></td>
            <td><b>{item.orderNumber ? 'OC-' + String(item.orderNumber).padStart(4, '0') : 'Orden'}</b><small>{item.supplier}</small></td>
            <td className="num" data-label="Unidades">{item.units.toLocaleString('es-AR')}{item.rejectedUnits ? <small>{item.rejectedUnits} rechazadas</small> : null}</td>
            <td className="num" data-label="Costo">{money(currency, item.knownCost)}{item.unitsWithoutCost ? <small>{item.unitsWithoutCost} u. sin costo</small> : null}</td>
          </tr>)}</tbody>
        </table></div>}
        {(offset > 0 || report.hasMore) && <nav className="pu-pager sr-pager" aria-label="Páginas de recepciones">
          <button type="button" className="pu-btn" disabled={offset === 0} onClick={() => setOffset(offset - pageSize)}>← Anterior</button>
          <button type="button" className="pu-btn" disabled={!current?.report?.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente →</button>
        </nav>}
      </div>
    </>}
  </div>;
}
