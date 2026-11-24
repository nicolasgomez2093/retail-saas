import { useEffect, useState } from 'react';
import { api, type ReceivablePage, type ReceivableStatus, type Store } from '../../shared/api/client';
import { routeHref } from '../../app/routes';
import { BarList } from './charts';
import { amount, money, plural } from './format';

type Period = { fromDate: string; toDate: string };
type Flow = { granted: string; grantedCount: number; collected: string; collectedCount: number; currency: string };

const emptyText: Record<ReceivableStatus, string> = {
  pending: 'No hay fiados pendientes.', partial: 'No hay fiados con abonos pendientes de saldar.', settled: 'No hay fiados saldados.',
};

export function ReceivablesView({ store, filters, status, revision, active }: {
  store: Store; filters: Period; status: ReceivableStatus; revision: number; active: boolean;
}) {
  const [offset, setOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const [pagedFor, setPagedFor] = useState(status);
  if (pagedFor !== status) {
    setPagedFor(status);
    setOffset(0);
  }
  const listKey = JSON.stringify([store.id, status, offset, revision, retry]);
  const flowKey = JSON.stringify([store.id, filters, revision, retry]);
  const [list, setList] = useState<{ key: string; page?: ReceivablePage; error?: string }>();
  const [flow, setFlow] = useState<{ key: string; value?: Flow; error?: string }>();
  const [shown, setShown] = useState<ReceivablePage>();
  const currentList = list?.key === listKey ? list : undefined;
  const currentFlow = flow?.key === flowKey ? flow : undefined;
  const page = currentList?.page ?? shown;

  useEffect(() => {
    if (!active) return;
    let live = true;
    api.receivables(store.id, { status, query: '' }, offset).then((result) => {
      if (!live) return;
      setList({ key: listKey, page: result });
      setShown(result);
    }, (cause: unknown) => {
      if (live) setList({ key: listKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar los fiados.' });
    });
    return () => { live = false; };
  }, [active, store.id, status, offset, listKey]);

  useEffect(() => {
    if (!active || !filters.fromDate) return;
    let live = true;
    // Fiado granted comes from the sales report; payments from the collections report, both in the period.
    Promise.all([
      api.salesMarginReport(store.id, 1, 0, { ...filters, query: '', method: 'on_credit' }),
      api.collectionsReport(store.id, 1, 0, { ...filters, method: '' }),
    ]).then(([sales, collections]) => {
      if (!live) return;
      const granted = sales.paymentTotals.find((row) => row.method === 'on_credit');
      const collected = collections.sources.find((row) => row.source === 'receivable');
      setFlow({ key: flowKey, value: {
        currency: granted?.currency ?? collected?.currency ?? 'ARS', granted: granted?.amount ?? '0.00', grantedCount: granted?.saleCount ?? 0,
        collected: collected?.amount ?? '0.00', collectedCount: collected?.count ?? 0,
      } });
    }, (cause: unknown) => {
      if (live) setFlow({ key: flowKey, error: cause instanceof Error ? cause.message : 'No pudimos cargar el movimiento de fiados.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, flowKey]);

  const loading = active && !currentList;
  const summary = page?.summary[0];
  const currency = summary?.currency ?? currentFlow?.value?.currency ?? 'ARS';
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short' });
  const error = currentList?.error ?? currentFlow?.error;

  return <div className="rp-body" aria-busy={loading} data-stale={(loading && Boolean(page)) || undefined}>
    {error && <div className="pu-card pu-panelbody sr-block">
      <p className="pu-alert" role="alert">{error}</p>
      <button type="button" className="pu-btn" onClick={() => setRetry((value) => value + 1)}>Reintentar</button>
    </div>}
    {loading && !page && <p className="pu-status" role="status">Consultando fiados…</p>}
    {page && <>
      <div className="pu-metrics">
        <div className="pu-metric">
          <small>Saldo por cobrar hoy</small>
          <strong className="sr-money">{money(currency, summary?.outstanding ?? '0.00')}</strong>
          <span>{plural(summary?.pendingCount ?? 0, 'fiado pendiente', 'fiados pendientes')} · no depende del período</span>
        </div>
        <div className="pu-metric">
          <small>Fiado en el período</small>
          <strong className="sr-money">{currentFlow?.value ? money(currentFlow.value.currency, currentFlow.value.granted) : '…'}</strong>
          <span>{currentFlow?.value ? plural(currentFlow.value.grantedCount, 'venta fiada', 'ventas fiadas') : 'Consultando…'}</span>
        </div>
        <div className="pu-metric">
          <small>Abonado en el período</small>
          <strong className="sr-money">{currentFlow?.value ? money(currentFlow.value.currency, currentFlow.value.collected) : '…'}</strong>
          <span>{currentFlow?.value ? plural(currentFlow.value.collectedCount, 'abono', 'abonos') : 'Consultando…'}</span>
        </div>
      </div>

      <div className="sr-grid">
        <div className="pu-card sr-block rp-flush">
          <div className="pu-panelhead"><div><h3>{status === 'pending' ? 'Fiados pendientes' : status === 'partial' ? 'Fiados con abonos' : 'Fiados saldados'}</h3>
            <p>{status === 'partial' ? 'Todavía deben, pero ya pagaron una parte.' : 'Por número de venta, del más reciente al más antiguo.'}</p></div>
            <a className="pu-btn" href={routeHref('fiados')}>Registrar abonos →</a></div>
          {page.items.length === 0 ? <p className="pu-status">{emptyText[status]}</p> : <div className="pu-table-wrap"><table className="pu-table rp-table">
            <thead><tr><th scope="col">Cliente</th><th scope="col">Venta</th><th scope="col" className="num">Fiado</th><th scope="col" className="num">Saldo</th></tr></thead>
            <tbody>{page.items.map((item) => <tr key={item.id}>
              <td><b>{item.customerName}</b></td>
              <td>V-{item.saleNumber}<small>{dates.format(new Date(item.createdAt))}</small></td>
              <td className="num" data-label="Fiado">{money(item.currency, item.originalAmount)}</td>
              <td className="num" data-label="Saldo"><b>{money(item.currency, item.outstandingAmount)}</b>
                {item.outstandingAmount !== item.originalAmount && Number(item.outstandingAmount) > 0 && <small>{Math.round((1 - amount(item.outstandingAmount) / amount(item.originalAmount)) * 100)} % pagado</small>}</td>
            </tr>)}</tbody>
          </table></div>}
          {(offset > 0 || page.hasMore) && <nav className="pu-pager sr-pager" aria-label="Páginas de fiados">
            <button type="button" className="pu-btn" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 25))}>← Anterior</button>
            <button type="button" className="pu-btn" disabled={!currentList?.page?.hasMore} onClick={() => setOffset(offset + 25)}>Siguiente →</button>
          </nav>}
        </div>
        <div className="pu-card pu-panelbody">
          <h3>Clientes que más deben</h3>
          <BarList label="Clientes que más deben" emptyText="Ningún cliente tiene saldo pendiente."
            rows={(summary?.topDebtors ?? []).map((row) => ({
              key: row.customerId, label: row.customerName, value: amount(row.outstanding), display: money(currency, row.outstanding),
              sub: plural(row.pendingCount, 'fiado', 'fiados'),
            }))} />
          <p className="pu-hint">Saldo actual de cada cliente en esta sucursal.</p>
        </div>
      </div>
    </>}
  </div>;
}
