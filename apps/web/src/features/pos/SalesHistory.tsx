import { useEffect, useId, useRef, useState } from 'react';
import { api, type CashRegisters, type ReturnHistory, type SaleHistoryPage, type SaleSummary, type StockLocation } from '../../shared/api/client';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { formatAmount } from '../catalog/quickProduct';
import { Modal } from '../purchases/PurchaseParts';
import '../purchases/purchases.css';
import { methodLabels, ReturnOperation } from './ReturnOperation';

const pageSize = 25;
const show = (currency: string, amount: string | bigint) => currency + ' ' + formatAmount(typeof amount === 'string' ? amount : decimalText(amount));

export function SalesHistory({ active, storeId, timezone, revision, ticketBusy, locations, registers, disabled, onTicket, onReturnPending, onReturned }: {
  active: boolean; storeId: string; timezone: string; revision: number; ticketBusy: boolean; locations: StockLocation[]; registers: CashRegisters; disabled: boolean;
  onTicket: (id: string, opener: HTMLButtonElement) => void; onReturnPending: (locked: boolean) => void; onReturned: () => void;
}) {
  const uid = useId();
  const [operation, setOperation] = useState<SaleSummary | null>(null);
  const [historyOf, setHistoryOf] = useState<{ sale: SaleSummary; opener: HTMLElement } | null>(null);
  const [retry, setRetry] = useState(0);
  const [search, setSearch] = useState('');
  const [filters, setFilters] = useState({ query: '', fromDate: '', toDate: '' });
  const [offset, setOffset] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: SaleHistoryPage; error?: string } | null>(null);
  const listHeading = useRef<HTMLHeadingElement>(null);
  const key = JSON.stringify([storeId, filters, offset, revision, retry]);
  const current = result?.key === key ? result : null;
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });

  useEffect(() => {
    const handle = window.setTimeout(() => { setFilters((value) => ({ ...value, query: search.trim() })); setOffset(0); }, 300);
    return () => window.clearTimeout(handle);
  }, [search]);
  useEffect(() => {
    if (!active) return;
    let currentRequest = true;
    // A short number is a sale number; anything else (customer, product, SKU or barcode) is a text search.
    const number = /^\d{1,9}$/.test(filters.query) ? filters.query : '';
    void api.saleHistory(storeId, number, offset, pageSize, { query: number ? '' : filters.query, fromDate: filters.fromDate, toDate: filters.toDate }).then((page) => {
      if (currentRequest) setResult({ key, page });
    }).catch((cause: unknown) => {
      if (currentRequest) setResult({ key, error: cause instanceof Error ? cause.message : 'No pudimos cargar las ventas.' });
    });
    return () => { currentRequest = false; };
  }, [active, storeId, filters, offset, key]);

  if (operation) return <ReturnOperation key={operation.id} sale={operation} storeId={storeId} registers={registers} locations={locations} timezone={timezone} onPending={onReturnPending}
    onClose={() => { setOperation(null); setRetry((value) => value + 1); requestAnimationFrame(() => listHeading.current?.focus()); }} onDone={onReturned} />;

  const page = current?.page;
  const filtered = Boolean(filters.query || filters.fromDate || filters.toDate);
  return <section className="pu sales-history" aria-labelledby={uid}>
    <header className="pu-head">
      <div><h2 id={uid} ref={listHeading} tabIndex={-1}>Ventas, cambios y devoluciones</h2><p>Buscá una venta para reimprimir el ticket o registrar un cambio o devolución.</p></div>
    </header>
    <div className="pu-card">
      <div className="pu-filters">
        <label className="pu-search"><span aria-hidden="true">⌕</span>
          <input type="search" value={search} maxLength={120} aria-label="Buscar venta" placeholder="N.º de venta, cliente, producto, SKU o código de barras…"
            onChange={(event) => setSearch(event.target.value)} /></label>
        <input type="date" aria-label="Desde" max={filters.toDate || '9999-12-31'} value={filters.fromDate} onChange={(event) => { setFilters({ ...filters, fromDate: event.target.value }); setOffset(0); }} />
        <input type="date" aria-label="Hasta" min={filters.fromDate || undefined} max="9999-12-31" value={filters.toDate} onChange={(event) => { setFilters({ ...filters, toDate: event.target.value }); setOffset(0); }} />
      </div>
      {active && !current && <p className="pu-status" role="status">Consultando ventas…</p>}
      {current?.error && <div className="pu-panelbody"><p className="pu-alert" role="alert">{current.error}</p>
        <button type="button" className="pu-btn" onClick={() => setRetry((value) => value + 1)}>Reintentar</button></div>}
      {page && (page.items.length === 0 ? <div className="pu-empty"><h3>{filtered ? 'No hay ventas con esos datos' : 'Todavía no hay ventas'}</h3>
        <p>{filtered ? 'Probá con el número del ticket, el nombre del cliente o escaneá la etiqueta del producto.' : 'Las ventas confirmadas aparecen acá.'}</p></div>
        : <ul className="pu-rows">{page.items.map((sale) => <li key={sale.id} className="pu-row">
          <div><b>V-{sale.number} · {show(sale.currency, sale.total)}</b>
            <small>{dates.format(new Date(sale.createdAt))}{sale.units ? ' · ' + sale.units + ' u.' : ''}{sale.products ? ' · ' + sale.products : ''}</small></div>
          <span>{sale.customer ? <span className="pu-pill warning">{sale.onCredit ? 'Fiado · ' : ''}{sale.customer}</span> : <span className="pu-pill muted">Consumidor final</span>}</span>
          <span>{sale.returns ? <button type="button" className="pu-link" onClick={(event) => setHistoryOf({ sale, opener: event.currentTarget })}
            aria-label={'Ver cambios y devoluciones de la venta ' + sale.number}>{sale.returns} {sale.returns === 1 ? 'operación' : 'operaciones'}</button>
            : <small>Sin cambios</small>}</span>
          <div className="pu-row-actions">
            <button type="button" className="pu-btn" disabled={ticketBusy} aria-label={'Reimprimir ticket ' + sale.number} onClick={(event) => onTicket(sale.id, event.currentTarget)}>🖨 Ticket</button>
            <button type="button" className="pu-btn" disabled={disabled} aria-label={'Cambio o devolución de la venta ' + sale.number} onClick={() => setOperation(sale)}>⇄ Cambio o devolución</button>
          </div>
        </li>)}</ul>)}
      {page && (offset > 0 || page.hasMore) && <nav className="pu-pager" aria-label="Páginas del historial de ventas">
        <button type="button" className="pu-btn" disabled={offset === 0} onClick={() => setOffset(offset - pageSize)}>← Anterior</button>
        <span>Página {offset / pageSize + 1}</span>
        <button type="button" className="pu-btn" disabled={!page.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente →</button>
      </nav>}
    </div>
    {historyOf && <SaleOperations sale={historyOf.sale} storeId={storeId} timezone={timezone} returnTo={historyOf.opener} onClose={() => setHistoryOf(null)} />}
  </section>;
}

/** Every exchange and return of a sale, read from the server (immutable records). */
function SaleOperations({ sale, storeId, timezone, returnTo, onClose }: { sale: SaleSummary; storeId: string; timezone: string; returnTo: HTMLElement; onClose: () => void }) {
  const [data, setData] = useState<ReturnHistory | { error: string } | null>(null);
  useEffect(() => {
    let active = true;
    void api.saleReturns(storeId, sale.id, 0).then((value) => { if (active) setData(value); })
      .catch((cause: unknown) => { if (active) setData({ error: cause instanceof Error ? cause.message : 'No pudimos cargar el historial.' }); });
    return () => { active = false; };
  }, [storeId, sale.id]);
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });
  return <Modal wide title={'Venta V-' + sale.number + ' · cambios y devoluciones'} returnTo={returnTo} onClose={onClose}
    footer={<button type="button" className="pu-btn primary" onClick={onClose}>Cerrar</button>}>
    {!data && <p className="pu-hint" role="status">Consultando…</p>}
    {data && 'error' in data && <p className="pu-alert" role="alert">{data.error}</p>}
    {data && 'items' in data && <>
      <p>Original: {data.items.map((item) => item.sold + ' × ' + item.product + (item.returned ? ' (' + item.returned + ' devueltas)' : '')).join(' · ')}</p>
      {data.outstandingDebt != null && <p className="pu-hint">Saldo del fiado: {show(data.currency, data.outstandingDebt)}</p>}
      {data.returns.map((item) => <article key={item.id} className="pu-receipt">
        <header><div><b>{item.exchange ? 'Cambio' : 'Devolución'} D-{item.id.slice(0, 8).toUpperCase()}</b>
          <small>{dates.format(new Date(item.createdAt))} · {item.reason}</small></div>
          <b>{show(data.currency, item.referenceAmount)}</b></header>
        <ul>
          {item.items.map((line) => <li key={line.variantId + line.locationId}><div>Vuelve {line.quantity} × {line.product}<br /><span>{line.sku} · {line.disposition === 'sellable' ? 'apto' : 'a revisión'} · {line.locationName}</span></div>
            <b>{show(data.currency, minorUnits(line.unitPrice) * BigInt(line.quantity))}</b></li>)}
          {item.exchange?.items.map((line) => <li key={line.variantId}><div>Se lleva {line.quantity} × {line.product}<br /><span>{[line.size, line.color].filter(Boolean).join(' · ')}</span></div>
            <b>{show(data.currency, line.subtotal)}</b></li>)}
          {item.exchange?.charge?.payments.map((payment) => <li key={'c' + payment.method}><div>Cobro · {methodLabels[payment.method]}</div><b>{show(data.currency, payment.amount)}</b></li>)}
          {item.debtCredits?.map((credit) => <li key={credit.id}><div>Descuento del fiado<br /><span>saldo {show(data.currency, credit.balanceAfter)}</span></div><b>{show(data.currency, credit.amount)}</b></li>)}
          {item.refund?.payments.map((payment) => <li key={'r' + payment.method}><div>Reintegro · {methodLabels[payment.method]}</div><b>{show(data.currency, payment.amount)}</b></li>)}
        </ul>
      </article>)}
    </>}
  </Modal>;
}
