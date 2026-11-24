import { useEffect, useState } from 'react';
import { api, type CollectionSource, type CollectionsFilters, type CollectionsReport, type Store } from '../../shared/api/client';
import { minorUnits } from '../../shared/lib/decimal';
import { paymentLabels } from '../pos/SalePayments';
import { BarList, DailyColumns } from './charts';
import { amount, methodLabel, money, plural, signedMoney } from './format';

const pageSize = 20;
const sourceLabels: Record<CollectionSource, string> = {
  sale: 'Ventas', deposit: 'Señas de reservas', receivable: 'Abonos de fiados', exchange: 'Diferencias de cambios', refund: 'Reintegros',
};

function cents(value: bigint) {
  const sign = value < 0n ? '-' : '';
  const absolute = value < 0n ? -value : value;
  return sign + (absolute / 100n).toString() + '.' + (absolute % 100n).toString().padStart(2, '0');
}

export function CollectionsView({ store, filters, revision, active, onTimezone }: {
  store: Store; filters: CollectionsFilters; revision: number; active: boolean; onTimezone: (timezone: string) => void;
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
  const [state, setState] = useState<{ key: string; report?: CollectionsReport; error?: string }>();
  const [shown, setShown] = useState<CollectionsReport>();
  const current = state?.key === key ? state : undefined;
  const report = current?.report ?? shown;

  useEffect(() => {
    if (!active || !filters.fromDate) return;
    let live = true;
    api.collectionsReport(store.id, pageSize, offset, filters).then((result) => {
      if (!live) return;
      setState({ key, report: result });
      setShown(result);
      onTimezone(result.timezone);
    }, (cause: unknown) => {
      if (live) setState({ key, error: cause instanceof Error ? cause.message : 'No pudimos cargar los cobros.' });
    });
    return () => { live = false; };
  }, [active, store.id, filters, offset, key, onTimezone]);

  const loading = active && !current;
  const currency = report?.methods[0]?.currency ?? report?.sources[0]?.currency ?? 'ARS';
  let collected = 0n, refunded = 0n, count = 0;
  for (const row of report?.methods ?? []) {
    if (row.currency !== currency) continue;
    collected += minorUnits(row.collected);
    refunded += minorUnits(row.refunded);
    count += row.count;
  }
  const cash = report?.methods.find((row) => row.currency === currency && row.method === 'cash');
  const dates = report && new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: report.timezone });

  return <div className="rp-body" aria-busy={loading} data-stale={(loading && Boolean(report)) || undefined}>
    {current?.error && <div className="pu-card pu-panelbody sr-block">
      <p className="pu-alert" role="alert">{current.error}</p>
      <button type="button" className="pu-btn" onClick={() => setRetry((value) => value + 1)}>Reintentar</button>
    </div>}
    {loading && !report && <p className="pu-status" role="status">Consultando cobros…</p>}
    {report && <>
      <div className="pu-metrics">
        <div className="pu-metric">
          <small>Cobrado neto</small>
          <strong className="sr-money">{signedMoney(currency, cents(collected - refunded))}</strong>
          <span>Ingresos {money(currency, cents(collected))}{refunded ? ` · reintegros −${money(currency, cents(refunded))}` : ''}</span>
        </div>
        <div className="pu-metric">
          <small>Movimientos</small>
          <strong>{count.toLocaleString('es-AR')}</strong>
          <span>{filters.method ? 'Solo ' + methodLabel(filters.method).toLowerCase() : 'Cobros y reintegros de todos los medios'}</span>
        </div>
        <div className="pu-metric">
          <small>{filters.method && filters.method !== 'cash' ? 'Medio elegido' : 'Efectivo neto'}</small>
          <strong className="sr-money">{filters.method && filters.method !== 'cash' ? methodLabel(filters.method) : signedMoney(currency, cash?.net ?? '0.00')}</strong>
          <span>{filters.method && filters.method !== 'cash' ? 'Elegí «Todos» para comparar medios' : 'Lo que tendría que sumar la caja, sin el fondo inicial'}</span>
        </div>
      </div>
      <p className="pu-note rp-note">Cada pago cuenta el día que se recibió y con su medio real. Una venta fiada no suma hasta que el cliente abona; una venta mixta aparece una vez por cada medio.</p>

      <div className="pu-card pu-panelbody sr-block">
        <div className="sr-title"><h3>Cobros por día</h3><span className="rp-legend"><i aria-hidden="true" />Cobrado neto</span></div>
        <DailyColumns title="Cobrado neto por día" fromDate={filters.fromDate} toDate={filters.toDate} emptyText="No hubo cobros en este período."
          days={report.daily.filter((day) => day.currency === currency).map((day) => {
            const net = minorUnits(day.collected) - minorUnits(day.refunded);
            return {
              date: day.date, value: Math.max(0, amount(cents(net))), label: signedMoney(day.currency, cents(net)),
              detail: Number(day.refunded) > 0 ? 'reintegros −' + money(day.currency, day.refunded) : undefined,
            };
          })} />
      </div>

      <div className="sr-grid even">
        <div className="pu-card pu-panelbody">
          <h3>Por medio de pago</h3>
          <BarList label="Cobrado por medio de pago" emptyText="Sin cobros en este período."
            rows={report.methods.map((row) => ({
              key: row.currency + row.method, label: paymentLabels[row.method], value: Math.max(0, amount(row.net)),
              display: signedMoney(row.currency, row.net),
              sub: plural(row.count, 'movimiento', 'movimientos') + (Number(row.refunded) > 0 ? ` · reintegrado −${money(row.currency, row.refunded)}` : ''),
            }))} />
        </div>
        <div className="pu-card pu-panelbody">
          <h3>Por origen</h3>
          <BarList label="Cobrado por origen" emptyText="Sin cobros en este período."
            rows={report.sources.map((row) => ({
              key: row.currency + row.source, label: sourceLabels[row.source], value: Math.abs(amount(row.amount)),
              display: signedMoney(row.currency, row.amount), sub: plural(row.count, 'movimiento', 'movimientos'),
            }))} />
          <p className="pu-hint">Los reintegros restan: es el dinero devuelto a clientes en devoluciones y cambios.</p>
        </div>
      </div>

      <div className="pu-card sr-block">
        <div className="pu-panelhead"><div><h3>Movimientos</h3><p>Del más reciente al más antiguo.</p></div></div>
        {report.items.length === 0 ? <p className="pu-status">No hay cobros para mostrar.</p> : <div className="pu-table-wrap"><table className="pu-table rp-table">
          <thead><tr><th scope="col">Fecha</th><th scope="col">Origen</th><th scope="col">Medio</th><th scope="col" className="num">Importe</th></tr></thead>
          <tbody>{report.items.map((item, index) => <tr key={index + item.happenedAt + item.source + item.method}>
            <td>{dates?.format(new Date(item.happenedAt))}</td>
            <td><b>{sourceLabels[item.source]}</b><small>{[item.saleNumber ? 'V-' + item.saleNumber : '', item.customer ?? ''].filter(Boolean).join(' · ') || '—'}</small></td>
            <td data-label="Medio">{paymentLabels[item.method]}</td>
            <td className="num" data-label="Importe" data-negative={item.amount.startsWith('-') || undefined}>{signedMoney(item.currency, item.amount)}</td>
          </tr>)}</tbody>
        </table></div>}
        {(offset > 0 || report.hasMore) && <nav className="pu-pager sr-pager" aria-label="Páginas de movimientos">
          <button type="button" className="pu-btn" disabled={offset === 0} onClick={() => setOffset(offset - pageSize)}>← Anterior</button>
          <button type="button" className="pu-btn" disabled={!current?.report?.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente →</button>
        </nav>}
      </div>
    </>}
  </div>;
}
