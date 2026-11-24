import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import {
  api, ApiError, type CashRegisters, type ReturnHistory, type ReturnOperationInput, type ReturnPreview, type SalePayment,
  type SalePaymentMethod, type SaleReturn, type SaleSummary, type SaleTicket, type StockLocation,
} from '../../shared/api/client';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { barcodePattern, formatAmount, parsePrice } from '../catalog/quickProduct';
import { Modal, VariantFinder, type Picked } from '../purchases/PurchaseParts';
import { itemLabel, newKey } from '../purchases/purchaseFormat';
import '../purchases/purchases.css';
import './ticket.css';

export const methodLabels: Record<SalePaymentMethod, string> = { cash: 'Efectivo', transfer: 'Transferencia', debit: 'Débito', credit: 'Crédito', qr: 'QR' };
const reasons = ['Cambio de talle', 'Cambio de color', 'Producto fallado', 'No le gustó'];
type Condition = 'sellable' | 'quarantine';
type Row = { quantity: string; condition: Condition };
type Replacement = Picked & { quantity: string; available?: number | null };
type Attempt = { input: ReturnOperationInput; key: string };

const cents = (amount: string) => minorUnits(amount);
const show = (currency: string, amount: string | bigint) => currency + ' ' + formatAmount(typeof amount === 'string' ? amount : decimalText(amount));
function count(raw: string) {
  const text = raw.trim();
  if (text === '') return 0;
  return /^\d{1,6}$/.test(text) ? Number(text) : undefined;
}

/**
 * «Cambio o devolución»: one screen and one server operation. The amounts
 * shown come from the server preview; the confirmation is validated again by
 * the server inside a single transaction.
 */
export function ReturnOperation({ sale, storeId, registers, locations, timezone, onClose, onDone, onPending }: {
  sale: SaleSummary; storeId: string; registers: CashRegisters; locations: StockLocation[]; timezone: string;
  onClose: () => void; onDone: () => void; onPending: (pending: boolean) => void;
}) {
  const uid = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const [loaded, setLoaded] = useState<{ history: ReturnHistory; ticket: SaleTicket } | { error: string } | null>(null);
  const [rows, setRows] = useState<Record<string, Row>>({});
  const [mode, setMode] = useState<'refund' | 'exchange'>('refund');
  const [replacements, setReplacements] = useState<Replacement[]>([]);
  const [reason, setReason] = useState('');
  const [preview, setPreview] = useState<{ key: string; data?: ReturnPreview; error?: string } | null>(null);
  const [chargeMethod, setChargeMethod] = useState<SalePaymentMethod>('cash');
  const [cashReceived, setCashReceived] = useState('');
  const [allocation, setAllocation] = useState<Record<string, string>>({});
  const [sessionId, setSessionId] = useState(() => registers.items.find((item) => item.openSessionId)?.openSessionId ?? '');
  const [scan, setScan] = useState('');
  const [scanFeedback, setScanFeedback] = useState<{ tone: '' | 'warn' | 'danger'; text: string } | null>(null);
  const [reviewing, setReviewing] = useState(false);
  const [phase, setPhase] = useState<'edit' | 'saving' | 'uncertain'>('edit');
  const [error, setError] = useState('');
  const [result, setResult] = useState<SaleReturn | null>(null);
  const attempt = useRef<Attempt | null>(null);
  const running = useRef(false);
  const locked = phase !== 'edit';
  const sessions = registers.items.filter((item) => item.openSessionId);
  const sellableLocations = locations.filter((item) => item.isSellable);
  const quarantine = locations.find((item) => !item.isSellable);

  useEffect(() => { heading.current?.focus(); }, []);
  // Only an operation being saved (or with an uncertain result) blocks changing branch.
  const pendingCallback = useRef(onPending);
  pendingCallback.current = onPending;
  useEffect(() => {
    if (!locked) return;
    pendingCallback.current(true);
    return () => pendingCallback.current(false);
  }, [locked]);
  useEffect(() => {
    let active = true;
    void Promise.all([api.saleReturns(storeId, sale.id, 0), api.saleTicket(storeId, sale.id)]).then(([history, ticket]) => {
      if (active) setLoaded({ history, ticket });
    }).catch((cause: unknown) => { if (active) setLoaded({ error: cause instanceof Error ? cause.message : 'No pudimos cargar la venta.' }); });
    return () => { active = false; };
  }, [storeId, sale.id]);

  const data = loaded && 'history' in loaded ? loaded : null;
  const currency = data?.history.currency ?? sale.currency;
  const returnLocation = data && (sellableLocations.find((item) => item.id === data.ticket.sale.locationId) ?? sellableLocations[0]);
  const lines = (data?.history.items ?? []).map((item) => {
    const row = rows[item.variantId] ?? { quantity: '', condition: 'sellable' as Condition };
    const left = item.sold - item.returned;
    const quantity = count(row.quantity);
    return { item, row, left, quantity, invalid: quantity === undefined || (quantity ?? 0) > left };
  });
  const chosen = lines.filter((line) => (line.quantity ?? 0) > 0 && !line.invalid);
  const invalidLines = lines.filter((line) => line.invalid);
  const replacementRows = replacements.map((item) => ({ item, quantity: count(item.quantity) }));
  const exchangeReady = mode === 'refund' || (replacementRows.length > 0 && replacementRows.every((row) => (row.quantity ?? 0) > 0));

  const request = useMemo<ReturnOperationInput | null>(() => {
    if (!returnLocation || !chosen.length || invalidLines.length || !exchangeReady) return null;
    if (chosen.some((line) => line.row.condition === 'quarantine') && !quarantine) return null;
    return {
      storeId, reason: reason.trim() || 'Sin motivo',
      items: chosen.map((line) => ({ variantId: line.item.variantId, quantity: line.quantity!, disposition: line.row.condition,
        locationId: line.row.condition === 'sellable' ? returnLocation.id : quarantine!.id })),
      ...(mode === 'exchange' ? { exchange: { locationId: returnLocation.id, items: replacementRows.map((row) => ({
        variantId: row.item.variant.id, quantity: row.quantity!, expectedUnitPrice: '' })) } } : {}),
    };
    // The request is identified by these inputs; recomputing it on every render is harmless.
  }, [JSON.stringify([rows, mode, replacements.map((item) => [item.variant.id, item.quantity]), returnLocation?.id, quarantine?.id, storeId, reason])]);
  const previewKey = request ? JSON.stringify({ ...request, reason: '' }) : '';

  // The server calculates every amount; the screen only shows it.
  useEffect(() => {
    if (!request || locked || result) return;
    let active = true;
    const handle = window.setTimeout(() => {
      void api.previewReturnOperation(sale.id, request).then((value) => {
        if (!active) return;
        setPreview({ key: previewKey, data: value });
        let remaining = cents(value.refund);
        const next: Record<string, string> = {};
        for (const option of value.refundOptions) {
          const take = remaining < cents(option.amount) ? remaining : cents(option.amount);
          next[option.method] = take > 0n ? formatAmount(decimalText(take)) : '';
          remaining -= take;
        }
        setAllocation(next);
      }).catch((cause: unknown) => {
        if (active) setPreview({ key: previewKey, error: cause instanceof Error ? cause.message : 'No pudimos calcular la diferencia.' });
      });
    }, 250);
    return () => { active = false; window.clearTimeout(handle); };
  }, [previewKey]);
  const current = preview?.key === previewKey ? preview : null;
  const quote = current?.data;

  // Stock of each replacement at the location that delivers it.
  useEffect(() => {
    const missing = replacements.filter((item) => item.available === undefined);
    if (!missing.length || !returnLocation) return;
    let active = true;
    void Promise.all(missing.map((item) => api.existence(storeId, item.variant.id).then(
      (detail) => [item.variant.id, detail.locations.find((location) => location.id === returnLocation.id)?.sellable ?? 0] as const,
      () => [item.variant.id, null] as const))).then((values) => {
      if (!active) return;
      const found = new Map(values);
      setReplacements((list) => list.map((item) => found.has(item.variant.id) ? { ...item, available: found.get(item.variant.id) } : item));
    });
    return () => { active = false; };
  }, [replacements, storeId, returnLocation]);

  function setRow(variantId: string, change: Partial<Row>) {
    setRows((current) => ({ ...current, [variantId]: { ...(current[variantId] ?? { quantity: '', condition: 'sellable' }), ...change } }));
  }

  async function readScan() {
    const code = scan.trim();
    setScan('');
    if (!code || locked || !data) return;
    let variantId = data.history.items.find((item) => item.sku.toLowerCase() === code.toLowerCase())?.variantId;
    if (!variantId && barcodePattern.test(code)) {
      try { variantId = (await api.barcode(code)).variant.id; } catch (cause) {
        if (!(cause instanceof ApiError) || cause.status !== 404) { setScanFeedback({ tone: 'danger', text: cause instanceof Error ? cause.message : 'No pudimos leer el código.' }); return; }
      }
    }
    const line = lines.find((candidate) => candidate.item.variantId === variantId);
    if (!line) { setScanFeedback({ tone: 'danger', text: 'El código «' + code + '» no corresponde a ningún artículo de esta venta.' }); return; }
    const next = (line.quantity ?? 0) + 1;
    if (next > line.left) { setScanFeedback({ tone: 'warn', text: 'De ' + line.item.product + ' · ' + itemLabel(line.item) + ' no quedan más unidades para devolver.' }); return; }
    setRow(line.item.variantId, { quantity: String(next) });
    setScanFeedback({ tone: '', text: '＋1 ' + line.item.product + ' · ' + itemLabel(line.item) + ' → ' + next + ' de ' + line.left });
  }

  const charge = quote ? cents(quote.charge) : 0n;
  const refund = quote ? cents(quote.refund) : 0n;
  const credit = quote ? cents(quote.debtCredit) : 0n;
  const received = parsePrice(cashReceived);
  const change = chargeMethod === 'cash' && received ? cents(received) - charge : 0n;
  const allocated = Object.values(allocation).reduce((total, value) => total + (parsePrice(value) ? cents(parsePrice(value)!) : 0n), 0n);
  const needsCash = charge > 0n || refund > 0n;
  const problems: string[] = [];
  if (!chosen.length) problems.push('Indicá al menos una unidad que vuelve.');
  if (invalidLines.length) problems.push('Revisá las cantidades marcadas.');
  if (chosen.some((line) => line.row.condition === 'quarantine') && !quarantine) problems.push('Para registrar mercadería dañada hace falta una ubicación no vendible.');
  if (mode === 'exchange' && !exchangeReady) problems.push('Elegí qué se lleva el cliente y en qué cantidad.');
  if (replacementRows.some((row) => row.item.available !== undefined && row.item.available !== null && (row.quantity ?? 0) > row.item.available)) problems.push('Uno de los reemplazos no tiene stock suficiente.');
  if (!reason.trim()) problems.push('Indicá el motivo.');
  if (needsCash && !sessionId) problems.push('Abrí una caja para cobrar o reintegrar.');
  if (charge > 0n && chargeMethod === 'cash' && (!received || cents(received) < charge)) problems.push('El efectivo recibido debe cubrir la diferencia.');
  if (refund > 0n && allocated !== refund) problems.push('Distribuí el reintegro completo entre los medios del pago original.');
  if (current?.error) problems.push(current.error);
  const ready = Boolean(quote) && problems.length === 0;

  function build(): ReturnOperationInput | null {
    if (!request || !quote) return null;
    const input: ReturnOperationInput = { ...request, reason: reason.trim() };
    if (input.exchange) input.exchange.items = input.exchange.items.map((item) => ({ ...item,
      expectedUnitPrice: quote.replacements.find((line) => line.variantId === item.variantId)?.unitPrice ?? '' }));
    if (charge > 0n) input.charge = { cashSessionId: sessionId, method: chargeMethod, amount: quote.charge,
      ...(chargeMethod === 'cash' ? { cashReceived: received! } : {}) };
    if (credit > 0n) input.debtCreditAmount = quote.debtCredit;
    if (refund > 0n) input.refund = { cashSessionId: sessionId, payments: Object.entries(allocation)
      .filter(([, value]) => parsePrice(value) && parsePrice(value) !== '0.00')
      .map(([method, value]) => ({ method: method as SalePaymentMethod, amount: parsePrice(value)! })) };
    return input;
  }

  async function save(operation: Attempt) {
    if (running.current) return;
    running.current = true;
    setPhase('saving');
    setError('');
    try {
      const saved = await api.addReturnOperation(sale.id, operation.input, operation.key);
      setReviewing(false);
      setResult(saved);
      setPhase('edit');
      onDone();
    } catch (cause) {
      const unknown = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      if (unknown) {
        setPhase('uncertain');
        setError('No sabemos si la operación llegó a registrarse porque se cortó la conexión. Reintentá: si ya se había registrado vas a ver el mismo comprobante, sin mover stock ni dinero otra vez.');
      } else {
        attempt.current = null;
        setPhase('edit');
        setReviewing(false);
        setError(cause.code === 'return_quantity_exceeded' ? 'Alguna de esas unidades ya fue devuelta en otra operación. Volvé a abrir la venta para ver lo disponible.'
          : cause.code === 'sale_price_changed' ? 'El precio de un reemplazo cambió. Revisá la diferencia actualizada y confirmá de nuevo.'
            : cause.code === 'return_exchange_stock' ? 'No hay stock suficiente de un reemplazo. No se registró nada.' : cause.message);
        setPreview(null);
      }
    } finally {
      running.current = false;
    }
  }

  function confirm() {
    if (attempt.current) { void save(attempt.current); return; }
    const input = build();
    if (!input) return;
    attempt.current = { input, key: newKey() };
    void save(attempt.current);
  }

  if (result && data) return <OperationResult result={result} data={data} timezone={timezone} onFinish={onClose} />;

  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });
  const original = data?.ticket.sale;
  // A fiado is paid in later collections: what was paid is the total minus the
  // balance still owed and minus what earlier returns already credited.
  const credited = data ? data.history.returns.flatMap((item) => item.debtCredits ?? []).reduce((total, item) => total + cents(item.amount), 0n) : 0n;
  const paid = !original ? 0n : original.credit && data?.history.outstandingDebt != null
    ? cents(original.total) - cents(data.history.outstandingDebt) - credited
    : original.payments.reduce((total, payment) => total + cents(payment.amount), 0n);
  return <section className="pu" aria-labelledby={uid + 'title'} id={uid}>
    <nav className="pu-breadcrumb" aria-label="Ruta"><button type="button" className="pu-link" disabled={locked} onClick={onClose}>Historial de ventas</button> / V-{sale.number} / Cambio o devolución</nav>
    <header className="pu-head">
      <div><h2 id={uid + 'title'} tabIndex={-1} ref={heading}>Cambio o devolución</h2>
        <p>Venta V-{sale.number} · los importes se calculan con lo que el cliente pagó, no con el precio de hoy.</p></div>
    </header>
    {error && <p className={'pu-alert' + (phase === 'uncertain' ? ' warn' : '')} role="alert">{error}</p>}
    {!loaded && <p className="pu-status" role="status">Consultando la venta…</p>}
    {loaded && 'error' in loaded && <p className="pu-alert" role="alert">{loaded.error}</p>}
    {data && original && <form className="pu-layout pu-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      if ((event.nativeEvent as SubmitEvent).submitter?.dataset.submit === 'operation' && ready && !locked) setReviewing(true);
    }}>
      <div className="pu-main">
        <div className="pu-card">
          <div className="pu-panelhead"><div><h3>1 · Venta original</h3><p>{dates.format(new Date(original.createdAt))} · {data.ticket.storeName}</p></div>
            {original.credit && <span className="pu-pill warning">Fiado</span>}</div>
          <div className="pu-panelbody">
            <dl className="pu-facts">
              <div><dt>Total de la venta</dt><dd>{show(currency, original.total)}</dd></div>
              <div><dt>{original.credit ? 'Pagado del fiado' : 'Pagado'}</dt><dd>{show(currency, paid)}{original.payments.length > 0 && <small style={{ display: 'block', fontWeight: 500, fontSize: 12 }}>{original.payments.map((payment) => methodLabels[payment.method]).join(' + ')}</small>}</dd></div>
              {original.credit && <div><dt>Cliente</dt><dd>{original.credit.customerName}</dd></div>}
              {data.history.outstandingDebt != null && <div><dt>Saldo del fiado</dt><dd>{show(currency, data.history.outstandingDebt)}</dd></div>}
            </dl>
            {data.history.returns.length > 0 && <p className="pu-hint">Esta venta ya tiene {data.history.returns.length} {data.history.returns.length === 1 ? 'operación' : 'operaciones'} de cambio o devolución. Solo se muestran las unidades que todavía pueden volver.</p>}
          </div>
        </div>

        <div className="pu-card">
          <div className="pu-panelhead"><div><h3>2 · ¿Qué devuelve el cliente?</h3><p>Escaneá la etiqueta o indicá las unidades. Elegí si cada artículo vuelve a la venta o queda en revisión.</p></div></div>
          <div className="pu-panelbody" style={{ display: 'grid', gap: 12 }}>
            <div className="pu-scan" role="search" aria-label="Escanear artículo devuelto">
              <label className="pu-label">Escanear código o SKU
                <input value={scan} disabled={locked} autoComplete="off" maxLength={80} placeholder="Cada lectura suma una unidad que vuelve"
                  onChange={(event) => setScan(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void readScan(); } }} />
              </label>
              <button type="button" className="pu-btn" disabled={locked || !scan.trim()} onClick={() => void readScan()}>Sumar</button>
            </div>
            {scanFeedback && <p className={'pu-feedback ' + scanFeedback.tone} role={scanFeedback.tone === 'danger' ? 'alert' : 'status'}>{scanFeedback.text}</p>}
          </div>
          <div className="pu-table-wrap">
            <table className="pu-table">
              <thead><tr><th scope="col">Artículo vendido</th><th scope="col" className="num">Disponible</th><th scope="col">Vuelven</th><th scope="col">Estado</th><th scope="col" className="num">Importe</th></tr></thead>
              <tbody>{lines.map((line) => {
                const amount = line.quantity && !line.invalid ? cents(line.item.unitPrice) * BigInt(line.quantity) : 0n;
                return <tr key={line.item.variantId} className={line.invalid ? 'invalid' : ''}>
                  <th scope="row">{line.item.product}<small>{itemLabel(line.item)} · {line.item.sku}</small>
                    <small>Comprado {line.item.sold} · ya devuelto {line.item.returned} · {show(currency, line.item.unitPrice)} c/u pagado</small>
                    {line.invalid && <small className="pu-error">Podés devolver hasta {line.left}.</small>}</th>
                  <td data-label="Disponible" className="num"><b>{line.left}</b></td>
                  <td data-label="Vuelven">{line.left > 0 ? <input className="qty" inputMode="numeric" placeholder="0" value={line.row.quantity} disabled={locked}
                    aria-label={'Unidades que vuelven de ' + line.item.sku} aria-invalid={line.invalid || undefined}
                    onChange={(event) => setRow(line.item.variantId, { quantity: event.target.value })} /> : <span className="pu-pill muted">Ya devuelto</span>}</td>
                  <td data-label="Estado">{line.left > 0 && <select className="pu-condition" value={line.row.condition} disabled={locked} aria-label={'Estado de ' + line.item.sku}
                    onChange={(event) => setRow(line.item.variantId, { condition: event.target.value as Condition })}>
                    <option value="sellable">Apto para la venta</option>
                    <option value="quarantine" disabled={!quarantine}>Dañado o para revisar</option>
                  </select>}</td>
                  <td data-label="Importe" className="num">{amount ? show(currency, amount) : '—'}</td>
                </tr>;
              })}</tbody>
            </table>
          </div>
          <div className="pu-panelbody" style={{ display: 'grid', gap: 10 }}>
            {chosen.some((line) => line.row.condition === 'quarantine') && <p className="pu-note warn" style={{ margin: 0 }}>
              {quarantine ? 'Lo dañado va a «' + quarantine.name + '»: queda registrado pero fuera del stock vendible.' : 'No hay una ubicación no vendible. Creala en Inventario › Ajustes para registrar mercadería dañada.'}</p>}
            <div className="pu-label"><span>Motivo *</span>
              <div className="pu-variants" style={{ padding: 0 }}>{reasons.map((item) => <button key={item} type="button" className="pu-chip" aria-pressed={reason === item}
                disabled={locked} onClick={() => setReason(item)}>{item}</button>)}</div>
              <input value={reason} maxLength={300} disabled={locked} aria-label="Motivo" placeholder="O escribí otro motivo" onChange={(event) => setReason(event.target.value)} />
            </div>
          </div>
        </div>

        {chosen.length > 0 && <div className="pu-card">
          <div className="pu-panelhead"><div><h3>3 · ¿Se lleva otra cosa?</h3><p>Para un cambio, elegí la variante exacta. El precio es el vigente hoy en esta sucursal.</p></div></div>
          <div className="pu-panelbody" style={{ display: 'grid', gap: 14 }}>
            <div className="pu-segment" role="radiogroup" aria-label="Tipo de operación" style={{ display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))', gap: 10 }}>
              {([['refund', 'Solo devolución', 'Se reintegra o descuenta del fiado'], ['exchange', 'Cambio por otro producto', 'Talle, color u otro artículo']] as const).map(([value, title, help]) =>
                <button key={value} type="button" role="radio" aria-checked={mode === value} className="pu-choice" disabled={locked} onClick={() => setMode(value)}>
                  <b>{title}</b><small>{help}</small></button>)}
            </div>
            {mode === 'exchange' && <>
              <VariantFinder disabled={locked} chosen={new Set(replacements.map((item) => item.variant.id))} label="Buscar o escanear reemplazo"
                onPick={(picked) => setReplacements((list) => list.some((item) => item.variant.id === picked.variant.id)
                  ? list.map((item) => item.variant.id === picked.variant.id ? { ...item, quantity: String((count(item.quantity) ?? 0) + 1) } : item)
                  : [...list, { ...picked, quantity: '1' }])} />
              {replacements.length > 0 && <div className="pu-table-wrap">
                <table className="pu-table">
                  <thead><tr><th scope="col">Se lleva</th><th scope="col" className="num">Stock</th><th scope="col">Cantidad</th><th scope="col" className="num">Precio actual</th><th scope="col"><span className="pos-sr-only">Quitar</span></th></tr></thead>
                  <tbody>{replacementRows.map(({ item, quantity }) => {
                    const priced = quote?.replacements.find((line) => line.variantId === item.variant.id);
                    const short = item.available != null && (quantity ?? 0) > item.available;
                    return <tr key={item.variant.id} className={short || quantity === undefined ? 'invalid' : ''}>
                      <th scope="row">{item.product.name}<small>{itemLabel(item.variant)} · {item.variant.sku}</small>{short && <small className="pu-error">Hay {item.available} disponibles.</small>}</th>
                      <td data-label="Stock" className="num">{item.available === undefined ? '…' : item.available ?? '—'}</td>
                      <td data-label="Cantidad"><input className="qty" inputMode="numeric" value={item.quantity} disabled={locked} aria-label={'Cantidad de ' + item.variant.sku}
                        aria-invalid={quantity === undefined || !quantity || short || undefined}
                        onChange={(event) => setReplacements((list) => list.map((entry) => entry.variant.id === item.variant.id ? { ...entry, quantity: event.target.value } : entry))} /></td>
                      <td data-label="Precio actual" className="num">{priced ? show(currency, priced.unitPrice) : '…'}</td>
                      <td className="pu-cell-actions"><button type="button" className="pu-icon" disabled={locked} aria-label={'Quitar ' + item.variant.sku}
                        onClick={() => setReplacements((list) => list.filter((entry) => entry.variant.id !== item.variant.id))}>×</button></td>
                    </tr>;
                  })}</tbody>
                </table>
              </div>}
            </>}
          </div>
        </div>}

        {quote && <div className="pu-card">
          <div className="pu-panelhead"><div><h3>4 · Diferencia</h3><p>Calculada por el sistema con los importes de la venta original.</p></div></div>
          <div className="pu-panelbody" style={{ display: 'grid', gap: 14 }}>
            <ul className="pu-sum">
              <li><span>Vuelve (precio pagado)</span><b>{show(currency, quote.returnAmount)}</b></li>
              {mode === 'exchange' && <li><span>Se lleva (precio actual)</span><b>{show(currency, quote.replacementTotal)}</b></li>}
              <li className="total"><span>{charge > 0n ? 'El cliente paga' : refund + credit > 0n ? 'A favor del cliente' : 'Sin diferencia'}</span>
                <b>{show(currency, charge > 0n ? quote.charge : decimalText(refund + credit))}</b></li>
            </ul>
            {charge === 0n && refund === 0n && credit === 0n && <p className="pu-note" style={{ margin: 0 }}>No hay cobro ni reintegro: solo se registran el cambio y los movimientos de stock.</p>}
            {credit > 0n && <p className="pu-note" style={{ margin: 0 }}>Primero se descuentan <b>{show(currency, quote.debtCredit)}</b> del fiado pendiente ({show(currency, quote.outstandingDebt ?? '0')}). No sale dinero de la caja por esa parte.</p>}
            {needsCash && <label className="pu-label">Caja
              {sessions.length ? <select value={sessionId} disabled={locked} onChange={(event) => setSessionId(event.target.value)}>
                {sessions.map((item) => <option key={item.id} value={item.openSessionId!}>{item.name}</option>)}
              </select> : <span className="pu-error">No hay una caja abierta. <a href="#/finanzas/caja">Abrir caja</a></span>}
            </label>}
            {charge > 0n && <div className="pu-formgrid">
              <label>Cobrar con<select value={chargeMethod} disabled={locked} onChange={(event) => setChargeMethod(event.target.value as SalePaymentMethod)}>
                {(Object.keys(methodLabels) as SalePaymentMethod[]).map((method) => <option key={method} value={method}>{methodLabels[method]}</option>)}</select></label>
              {chargeMethod === 'cash' && <label>Efectivo recibido<input inputMode="decimal" value={cashReceived} disabled={locked} placeholder={formatAmount(quote.charge)}
                aria-invalid={Boolean(cashReceived) && (!received || cents(received) < charge) || undefined} onChange={(event) => setCashReceived(event.target.value)} />
                {change > 0n && <span className="pu-hint">Vuelto: {show(currency, change)}</span>}</label>}
            </div>}
            {refund > 0n && <div className="pu-label"><span>Reintegro de {show(currency, quote.refund)} con los medios del pago original</span>
              {quote.refundOptions.length === 0 && <span className="pu-error">La venta no tiene cobros disponibles para reintegrar.</span>}
              <div className="pu-formgrid">{quote.refundOptions.map((option: SalePayment) => <label key={option.method}>{methodLabels[option.method]} <small className="pu-hint">hasta {show(currency, option.amount)}</small>
                <input inputMode="decimal" value={allocation[option.method] ?? ''} disabled={locked} aria-label={'Reintegro en ' + methodLabels[option.method]}
                  onChange={(event) => setAllocation((value) => ({ ...value, [option.method]: event.target.value }))} /></label>)}</div>
              {quote.refundOptions.some((option) => option.method !== 'cash' && cents(allocation[option.method] && parsePrice(allocation[option.method]) || '0.00') > 0n) &&
                <p className="pu-hint">Los reintegros por transferencia, tarjeta o QR no salen del efectivo de la caja. El sistema no revierte pagos con tarjeta: hacelo en el procesador y registralo acá.</p>}
            </div>}
          </div>
        </div>}
      </div>

      <aside className="pu-side" aria-label="Resumen de la operación">
        <div className="pu-card pu-sidebox">
          <h3>Resumen</h3>
          <ul className="pu-sum">
            <li><span>Vuelven</span><b>{chosen.reduce((total, line) => total + line.quantity!, 0)} u.</b></li>
            <li><span>A revisión</span><b>{chosen.filter((line) => line.row.condition === 'quarantine').reduce((total, line) => total + line.quantity!, 0)} u.</b></li>
            {mode === 'exchange' && <li><span>Se lleva</span><b>{replacementRows.reduce((total, row) => total + (row.quantity ?? 0), 0)} u.</b></li>}
            <li><span>Cobro</span><b>{quote ? show(currency, quote.charge) : '—'}</b></li>
            <li><span>Descuento del fiado</span><b>{quote ? show(currency, quote.debtCredit) : '—'}</b></li>
            <li className="total"><span>Reintegro</span><b>{quote ? show(currency, quote.refund) : '—'}</b></li>
          </ul>
          {request && !quote && !current?.error && <p className="pu-hint" role="status">Calculando la diferencia…</p>}
          {problems.length > 0 && chosen.length > 0 && <ul className="pu-hint" style={{ paddingLeft: 18, margin: '12px 0 0' }}>{problems.map((item) => <li key={item}>{item}</li>)}</ul>}
          <button ref={confirmButton} type="submit" data-submit="operation" className="pu-btn primary wide" disabled={!ready || locked}>Revisar y confirmar</button>
          <button type="button" className="pu-btn wide" disabled={phase === 'saving'} onClick={onClose}>Cancelar</button>
        </div>
      </aside>
    </form>}
    {reviewing && quote && <Modal title="Confirmar operación" returnTo={confirmButton.current} busy={phase === 'saving'} onClose={() => { if (phase === 'edit') setReviewing(false); }}
      footer={<>
        <button type="button" className="pu-btn" disabled={phase !== 'edit'} onClick={() => setReviewing(false)}>Volver a editar</button>
        <button type="button" className="pu-btn primary" disabled={phase === 'saving'} onClick={confirm}>
          {phase === 'saving' ? 'Registrando…' : phase === 'uncertain' ? 'Reintentar la misma operación' : 'Confirmar'}</button>
      </>}>
      <p><b>V-{sale.number}</b> · {reason}</p>
      {phase === 'uncertain' && <p className="pu-alert warn" role="alert">{error}</p>}
      {chosen.map((line) => <div key={line.item.variantId} className="pu-statrow"><div><b>Vuelve: {line.item.product}</b>
        <span className="sub">{itemLabel(line.item)} · {line.quantity} u. · {line.row.condition === 'sellable' ? 'apto para la venta' : 'a revisión, fuera del stock vendible'}</span></div>
        <b>{show(currency, cents(line.item.unitPrice) * BigInt(line.quantity!))}</b></div>)}
      {mode === 'exchange' && quote.replacements.map((line) => <div key={line.variantId} className="pu-statrow"><div><b>Se lleva: {line.product}</b>
        <span className="sub">{[line.size, line.color].filter(Boolean).join(' · ')} · {line.quantity} u.</span></div><b>{show(currency, line.subtotal)}</b></div>)}
      <div className="pu-note"><p>{charge > 0n ? 'El cliente paga ' + show(currency, quote.charge) + ' con ' + methodLabels[chargeMethod].toLowerCase() + '.'
        : refund + credit === 0n ? 'Sin diferencia: no hay cobro ni reintegro.'
          : [credit > 0n && 'Se descuentan ' + show(currency, quote.debtCredit) + ' del fiado.', refund > 0n && 'Se reintegran ' + show(currency, quote.refund) + '.'].filter(Boolean).join(' ')}
        {' '}Todo se registra en una sola operación.</p></div>
    </Modal>}
  </section>;
}

/** Shown after confirming: the persisted operation, printable as a non fiscal voucher. */
function OperationResult({ result, data, timezone, onFinish }: {
  result: SaleReturn; data: { history: ReturnHistory; ticket: SaleTicket }; timezone: string; onFinish: () => void;
}) {
  const uid = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const [printing, setPrinting] = useState(false);
  useEffect(() => { heading.current?.focus(); }, []);
  const currency = data.history.currency;
  const when = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone }).format(new Date(result.createdAt));
  const exchange = result.exchange;
  const refund = result.refund;
  const credits = result.debtCredits ?? [];
  const charge = exchange?.charge;
  const code = 'D-' + result.id.slice(0, 8).toUpperCase();
  const body = <article className="receipt" aria-label="Comprobante de cambio o devolución" style={{ width: '74mm' }}>
    <header>
      <strong>{data.ticket.merchantName}</strong>
      <div>{data.ticket.storeName} ({data.ticket.storeCode})</div>
      <p className="receipt-notice"><strong>COMPROBANTE NO FISCAL</strong><b>NO VÁLIDO COMO FACTURA</b></p>
      <h3>{exchange ? 'Cambio' : 'Devolución'} {code}</h3>
      <div>Venta original N.º {data.history.number}</div>
      <time dateTime={result.createdAt}>{when}</time>
    </header>
    <ul>
      {result.items.map((item) => <li key={item.variantId + item.locationId}><strong>Vuelve: {item.product}</strong>
        <div className="receipt-variant">{item.sku} · {item.disposition === 'sellable' ? 'Apto · ' : 'Revisión · '}{item.locationName}</div>
        <dl className="receipt-line"><dt>{item.quantity} × {show(currency, item.unitPrice)}</dt><dd>{show(currency, cents(item.unitPrice) * BigInt(item.quantity))}</dd></dl></li>)}
      {exchange?.items.map((item) => <li key={item.variantId}><strong>Se lleva: {item.product}</strong>
        <div className="receipt-variant">{[item.sku, item.size, item.color].filter(Boolean).join(' · ')}</div>
        <dl className="receipt-line"><dt>{item.quantity} × {show(currency, item.unitPrice)}</dt><dd>{show(currency, item.subtotal)}</dd></dl></li>)}
    </ul>
    <section className="receipt-payment" aria-label="Diferencia">
      <dl>
        <dt>Devuelto</dt><dd>{show(currency, result.referenceAmount)}</dd>
        {exchange && <><dt>Entregado</dt><dd>{show(currency, exchange.total)}</dd></>}
        {charge && <><dt>Cobrado</dt><dd>{show(currency, charge.amount)}</dd>
          {charge.payments.map((payment) => <span key={payment.method} style={{ display: 'contents' }}><dt>· {methodLabels[payment.method]}</dt><dd>{show(currency, payment.amount)}</dd></span>)}
          {charge.change && charge.change !== '0.00' && <><dt>Vuelto</dt><dd>{show(currency, charge.change)}</dd></>}</>}
        {credits.map((item) => <span key={item.id} style={{ display: 'contents' }}><dt>Descontado del fiado</dt><dd>{show(currency, item.amount)}</dd>
          <dt>Saldo del fiado</dt><dd>{show(currency, item.balanceAfter)}</dd></span>)}
        {refund && <><dt>Reintegrado</dt><dd>{show(currency, refund.amount)}</dd>
          {refund.payments.map((payment) => <span key={payment.method} style={{ display: 'contents' }}><dt>· {methodLabels[payment.method]}</dt><dd>{show(currency, payment.amount)}</dd></span>)}</>}
        {!charge && !refund && !credits.length && <><dt>Diferencia</dt><dd>{show(currency, '0.00')}</dd></>}
      </dl>
      <p>Motivo: {result.reason}</p>
    </section>
  </article>;
  return <section className="pu" aria-labelledby={uid}>
    <nav className="pu-breadcrumb" aria-label="Ruta"><button type="button" className="pu-link" onClick={onFinish}>Historial de ventas</button> / V-{data.history.number} / Operación registrada</nav>
    <div className="pu-card">
      <div className="pu-panelhead"><div className="pu-success"><span aria-hidden="true">✓</span>
        <div><h2 id={uid} tabIndex={-1} ref={heading}>{exchange ? 'Cambio registrado' : 'Devolución registrada'}</h2>
          <p>{code} · venta original V-{data.history.number} · {when} · {data.ticket.storeName}</p></div></div>
        <span className="pu-pill">Confirmada</span></div>
      <div className="pu-panelbody">
        <dl className="pu-facts">
          <div><dt>Vuelven</dt><dd>{result.items.reduce((total, item) => total + item.quantity, 0)} u.</dd></div>
          {exchange && <div><dt>Se lleva</dt><dd>{exchange.items.reduce((total, item) => total + item.quantity, 0)} u.</dd></div>}
          <div><dt>{charge ? 'Cobrado' : refund ? 'Reintegrado' : credits.length ? 'Descontado del fiado' : 'Diferencia'}</dt>
            <dd>{show(currency, charge?.amount ?? refund?.amount ?? credits[0]?.amount ?? '0.00')}</dd></div>
          {credits.length > 0 && refund && <div><dt>Descontado del fiado</dt><dd>{show(currency, credits[0].amount)}</dd></div>}
        </dl>
        <div className="pu-table-wrap" style={{ border: '1px solid var(--p-line)', borderRadius: 12, overflow: 'hidden' }}>
          <table className="pu-table">
            <thead><tr><th scope="col">Movimiento</th><th scope="col">Detalle</th><th scope="col" className="num">Importe</th></tr></thead>
            <tbody>
              {result.items.map((item) => <tr key={item.variantId + item.locationId}><th scope="row">Ingreso de stock<small>{item.disposition === 'sellable' ? 'Vendible' : 'Fuera de la venta'} · {item.locationName}</small></th>
                <td data-label="Detalle">{item.product} · {item.sku} · {item.quantity} u.</td><td data-label="Importe" className="num">{show(currency, cents(item.unitPrice) * BigInt(item.quantity))}</td></tr>)}
              {exchange?.items.map((item) => <tr key={item.variantId}><th scope="row">Salida de stock<small>Cambio · {exchange.locationName}</small></th>
                <td data-label="Detalle">{item.product} · {[item.size, item.color].filter(Boolean).join(' · ')} · {item.quantity} u.</td><td data-label="Importe" className="num">{show(currency, item.subtotal)}</td></tr>)}
              {charge?.payments.map((payment) => <tr key={'c' + payment.method}><th scope="row">Cobro<small>{payment.method === 'cash' ? 'Entra a la caja' : 'No mueve efectivo'}</small></th>
                <td data-label="Detalle">{methodLabels[payment.method]}</td><td data-label="Importe" className="num">{show(currency, payment.amount)}</td></tr>)}
              {credits.map((item) => <tr key={item.id}><th scope="row">Descuento del fiado<small>Saldo: {show(currency, item.balanceAfter)}</small></th>
                <td data-label="Detalle">Cuenta corriente</td><td data-label="Importe" className="num">{show(currency, item.amount)}</td></tr>)}
              {refund?.payments.map((payment) => <tr key={'r' + payment.method}><th scope="row">Reintegro<small>{payment.method === 'cash' ? 'Sale de la caja' : 'No mueve efectivo'}</small></th>
                <td data-label="Detalle">{methodLabels[payment.method]}</td><td data-label="Importe" className="num">{show(currency, payment.amount)}</td></tr>)}
            </tbody>
          </table>
        </div>
        <div className="pu-note"><p>La venta original no se modifica: la operación queda vinculada y se consulta desde su historial. El comprobante no es fiscal.</p></div>
        <div className="pu-actions">
          <button type="button" className="pu-btn primary" onClick={() => setPrinting(true)}>🖨 Imprimir comprobante</button>
          <button type="button" className="pu-btn" onClick={onFinish}>Finalizar</button>
        </div>
      </div>
    </div>
    {printing && <Modal title="Comprobante" returnTo={null} onClose={() => setPrinting(false)} footer={<>
      <button type="button" className="pu-btn" onClick={() => setPrinting(false)}>Cerrar</button>
      <button type="button" className="pu-btn primary" onClick={() => window.print()}>Imprimir o guardar PDF</button></>}>
      <div className="ticket-preview">{body}</div>
    </Modal>}
    {printing && createPortal(<div className="ticket-print" aria-hidden="true">{body}</div>, document.body)}
  </section>;
}
