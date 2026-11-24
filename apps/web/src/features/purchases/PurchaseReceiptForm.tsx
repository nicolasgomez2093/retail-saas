import { useEffect, useId, useRef, useState, type FormEvent } from 'react';

import { api, ApiError, type PurchaseOrder, type PurchaseReceipt, type PurchaseReceiptInput, type RejectionReason, type StockLocation } from '../../shared/api/client';
import { barcodePattern, formatAmount } from '../catalog/quickProduct';
import { itemLabel, lineCents, money, moneyText, newKey, orderCode, parseCost, reasonHelp, reasonLabels, statusLabels, statusTone, sumCents } from './purchaseFormat';
import { Modal, ReceiptLabels } from './PurchaseParts';

type Row = { accepted: string; rejected: string; reason: RejectionReason | ''; cost: string; diff: boolean };
type Attempt = { input: PurchaseReceiptInput; key: string };
const blank: Row = { accepted: '', rejected: '', reason: '', cost: '', diff: false };

function count(raw: string) {
  const text = raw.trim();
  if (text === '') return 0;
  if (!/^\d{1,6}$/.test(text)) return undefined;
  return Number(text);
}

export function PurchaseReceiptForm({ order, onClose, onSaved }: {
  order: PurchaseOrder; onClose: () => void; onSaved: (receipt: PurchaseReceipt) => void;
}) {
  const uid = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const scanInput = useRef<HTMLInputElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const pendingItems = order.items.filter((item) => item.received < item.quantity);
  const [locations, setLocations] = useState<StockLocation[] | { error: string } | null>(null);
  const [locationId, setLocationId] = useState('');
  const [rows, setRows] = useState<Record<string, Row>>(() => Object.fromEntries(pendingItems.map((item) => [item.id,
    { ...blank, cost: item.unitCost ? formatAmount(item.unitCost) : '' }])));
  const [note, setNote] = useState('');
  const [scan, setScan] = useState('');
  const [scanFeedback, setScanFeedback] = useState<{ tone: '' | 'warn' | 'danger'; text: string } | null>(null);
  const [flash, setFlash] = useState('');
  const [reviewing, setReviewing] = useState(false);
  const [phase, setPhase] = useState<'edit' | 'saving' | 'uncertain'>('edit');
  const [error, setError] = useState('');
  const [showErrors, setShowErrors] = useState(false);
  const attempt = useRef<Attempt | null>(null);
  const running = useRef(false);
  const lastScan = useRef({ code: '', at: 0 });
  const codes = useRef(new Map<string, string>());
  const locked = phase !== 'edit';

  useEffect(() => { heading.current?.focus(); }, []);
  useEffect(() => {
    let active = true;
    void api.locations(order.storeId).then((result) => {
      if (!active) return;
      setLocations(result.items);
      const preferred = result.items.find((item) => item.isSellable) ?? result.items[0];
      if (preferred) setLocationId(preferred.id);
    }).catch((cause: unknown) => { if (active) setLocations({ error: cause instanceof Error ? cause.message : 'No pudimos consultar las ubicaciones.' }); });
    return () => { active = false; };
  }, [order.storeId]);
  useEffect(() => {
    if (!flash) return;
    const handle = window.setTimeout(() => setFlash(''), 1400);
    return () => window.clearTimeout(handle);
  }, [flash]);

  const update = (id: string, change: Partial<Row>) => setRows((current) => ({ ...current, [id]: { ...current[id], ...change } }));

  const lines = pendingItems.map((item) => {
    const row = rows[item.id] ?? blank;
    const left = item.quantity - item.received;
    const accepted = count(row.accepted);
    const rejected = row.diff ? count(row.rejected) : 0;
    const cost = parseCost(row.cost);
    const problems: string[] = [];
    if (accepted === undefined) problems.push('Ingresá un número entero de unidades aceptadas.');
    else if (accepted > left) problems.push('Quedan ' + left + ' pendientes. Lo que sobra se registra como excedente.');
    if (rejected === undefined) problems.push('Ingresá un número entero de unidades no aceptadas.');
    else if (rejected > 0 && !row.reason) problems.push('Elegí el motivo de las unidades no aceptadas.');
    if (cost === undefined) problems.push('El costo admite hasta dos decimales.');
    return { item, row, left, accepted, rejected, cost, problems };
  });
  const acceptedTotal = lines.reduce((total, line) => total + (line.accepted ?? 0), 0);
  const rejectedTotal = lines.reduce((total, line) => total + (line.rejected ?? 0), 0);
  const leftAfter = lines.reduce((total, line) => total + Math.max(0, line.left - (line.accepted ?? 0)), 0);
  const value = sumCents(lines.map((line) => line.accepted && line.cost ? lineCents(line.accepted, line.cost) : null));
  const unknownCost = lines.some((line) => (line.accepted ?? 0) > 0 && line.cost === null);
  const invalid = lines.filter((line) => line.problems.length);
  const location = Array.isArray(locations) ? locations.find((item) => item.id === locationId) : undefined;

  async function readScan(event: FormEvent) {
    event.preventDefault();
    const code = scan.trim();
    setScan('');
    scanInput.current?.focus();
    if (!code || locked) return;
    const now = Date.now();
    // A scanner that fires twice within 350 ms is one physical read.
    if (lastScan.current.code === code && now - lastScan.current.at < 350) return;
    lastScan.current = { code, at: now };
    let variantId = codes.current.get(code.toLowerCase()) ?? pendingItems.find((item) => item.sku.toLowerCase() === code.toLowerCase())?.variantId;
    let name = '';
    if (!variantId && barcodePattern.test(code)) {
      try {
        const match = await api.barcode(code);
        variantId = match.variant.id;
        name = match.product.name + ' · ' + itemLabel(match.variant);
        codes.current.set(code.toLowerCase(), variantId);
      } catch (cause) {
        if (!(cause instanceof ApiError) || cause.status !== 404) {
          setScanFeedback({ tone: 'danger', text: cause instanceof Error ? cause.message : 'No pudimos leer el código.' });
          return;
        }
      }
    }
    const item = order.items.find((candidate) => candidate.variantId === variantId);
    if (!variantId) { setScanFeedback({ tone: 'danger', text: 'El código «' + code + '» no corresponde a ningún producto del catálogo.' }); return; }
    if (!item) {
      setScanFeedback({ tone: 'danger', text: (name || 'Ese producto') + ' no está en ' + orderCode(order.number) + '. No se suma al stock. Si vino en lugar de otro, registralo como «Producto equivocado» en ese renglón.' });
      return;
    }
    const line = lines.find((candidate) => candidate.item.id === item.id);
    if (!line) { setScanFeedback({ tone: 'warn', text: item.product + ' · ' + itemLabel(item) + ' ya se recibió completo en este pedido.' }); return; }
    const current = line.accepted ?? 0;
    if (current >= line.left) {
      setScanFeedback({ tone: 'warn', text: 'Ya cargaste las ' + line.left + ' pendientes de ' + item.product + ' · ' + itemLabel(item) + '. Si llegó una más, registrala como excedente.' });
      setFlash(item.id);
      return;
    }
    update(item.id, { accepted: String(current + 1) });
    setFlash(item.id);
    setScanFeedback({ tone: '', text: '＋1 ' + item.product + ' · ' + itemLabel(item) + ' → ' + (current + 1) + ' de ' + line.left });
  }

  function review(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if ((event.nativeEvent as SubmitEvent).submitter?.dataset.submit !== 'receipt' || locked) return;
    setShowErrors(true);
    setError('');
    if (!locationId) { setError('Elegí la ubicación donde ingresa la mercadería.'); return; }
    if (invalid.length) {
      setError('Revisá los renglones marcados antes de confirmar.');
      document.querySelector<HTMLElement>('#' + CSS.escape(uid) + ' [aria-invalid=true]')?.focus();
      return;
    }
    if (!acceptedTotal && !rejectedTotal) { setError('Ingresá al menos una unidad recibida.'); scanInput.current?.focus(); return; }
    setReviewing(true);
  }

  async function save(operation: Attempt) {
    if (running.current) return;
    running.current = true;
    setPhase('saving');
    setError('');
    try {
      const receipt = await api.receivePurchaseOrder(order.id, operation.input, operation.key);
      onSaved(receipt);
    } catch (cause) {
      const unknown = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      if (unknown) {
        setPhase('uncertain');
        setError('No sabemos si la recepción llegó a registrarse porque se cortó la conexión. Reintentá: si ya estaba registrada vas a ver la misma recepción, sin sumar stock dos veces.');
      } else {
        attempt.current = null;
        setPhase('edit');
        setReviewing(false);
        setError(cause.code === 'purchase_receipt_quantity' ? 'Otra persona registró una recepción de este pedido mientras cargabas. Cerrá y volvé a abrir el pedido para ver lo pendiente.'
          : cause.code === 'purchase_order_closed' ? 'El pedido ya está recibido o cancelado. No se registró nada.'
            : cause.code === 'purchase_receipt_location' ? 'La ubicación elegida ya no está activa. Elegí otra.' : cause.message);
      }
    } finally {
      running.current = false;
    }
  }

  function confirm() {
    if (attempt.current) { void save(attempt.current); return; }
    const items = lines.flatMap((line) => {
      const accepted = line.accepted ?? 0;
      const rejected = line.rejected ?? 0;
      if (!accepted && !rejected) return [];
      return [{ orderItemId: line.item.id, accepted, rejected, ...(accepted && line.cost ? { unitCost: line.cost } : {}),
        ...(rejected ? { rejectionReason: line.row.reason as RejectionReason } : {}) }];
    });
    const operation: Attempt = { key: newKey(), input: { storeId: order.storeId, locationId, note: note.trim(), items } };
    attempt.current = operation;
    void save(operation);
  }

  return <section className="pu" aria-labelledby={uid + 'title'} id={uid}>
    <nav className="pu-breadcrumb" aria-label="Ruta"><button type="button" className="pu-link" disabled={locked} onClick={onClose}>Compras</button> / {orderCode(order.number)} / Recepción</nav>
    <header className="pu-head">
      <div><h2 id={uid + 'title'} tabIndex={-1} ref={heading}>Recibir mercadería</h2>
        <p>{orderCode(order.number)} · {order.supplierName}{order.reference ? ' · ' + order.reference : ''}</p></div>
      <span className={'pu-pill ' + statusTone[order.status]}>{statusLabels[order.status]}</span>
    </header>
    {error && <p className={'pu-alert' + (phase === 'uncertain' ? ' warn' : '')} role="alert">{error}</p>}
    <form className="pu-layout pu-form" onSubmit={review} noValidate>
      <div className="pu-main">
        <div className="pu-card">
          <div className="pu-panelhead"><div><h3>Lo que llegó</h3><p>Cargá las unidades que aceptás. Las dañadas, equivocadas o de más se registran como diferencia y no ingresan al stock.</p></div></div>
          <div className="pu-panelbody" style={{ display: 'grid', gap: 16 }}>
            <div className="pu-formgrid">
              <label>Ubicación de ingreso
                {locations === null ? <span className="pu-hint" role="status">Consultando ubicaciones…</span>
                  : !Array.isArray(locations) ? <span className="pu-error" role="alert">{locations.error}</span>
                    : locations.length === 0 ? <span className="pu-error" role="alert">La sucursal no tiene ubicaciones activas. Creá una en Inventario › Ajustes.</span>
                      : <select value={locationId} disabled={locked} onChange={(event) => setLocationId(event.target.value)}>
                        {locations.map((item) => <option key={item.id} value={item.id}>{item.name}{item.isSellable ? '' : ' (no vendible)'}</option>)}
                      </select>}
              </label>
              <div className="pu-label"><span>Completar rápido</span>
                <div className="pu-actions" style={{ marginTop: 0 }}>
                  <button type="button" className="pu-btn" disabled={locked} onClick={() => setRows((current) => Object.fromEntries(lines.map((line) =>
                    [line.item.id, { ...current[line.item.id], accepted: String(line.left) }])))}>Llegó todo lo pendiente</button>
                  <button type="button" className="pu-btn" disabled={locked} onClick={() => setRows((current) => Object.fromEntries(lines.map((line) =>
                    [line.item.id, { ...current[line.item.id], accepted: '' }])))}>Vaciar</button>
                </div>
              </div>
            </div>
            <div className="pu-scan" role="search" aria-label="Escanear mercadería">
              <label className="pu-label">Escanear código o SKU
                <input ref={scanInput} value={scan} disabled={locked} autoComplete="off" maxLength={80} placeholder="Cada lectura suma una unidad aceptada"
                  onChange={(event) => setScan(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void readScan(event); }} />
              </label>
              <button type="button" className="pu-btn" disabled={locked || !scan.trim()} onClick={(event) => void readScan(event)}>Sumar</button>
            </div>
            {scanFeedback && <p className={'pu-feedback ' + scanFeedback.tone} role={scanFeedback.tone === 'danger' ? 'alert' : 'status'}>{scanFeedback.text}</p>}
          </div>
          <div className="pu-table-wrap">
            <table className="pu-table">
              <thead><tr><th scope="col">Variante</th><th scope="col" className="num">Pendiente</th><th scope="col">Aceptadas ahora</th>
                <th scope="col">Diferencias</th><th scope="col">Costo unitario</th></tr></thead>
              <tbody>{lines.map((line) => {
                const { item, row } = line;
                const bad = showErrors && line.problems.length > 0;
                const over = (line.accepted ?? 0) - line.left;
                return <tr key={item.id} className={(flash === item.id ? 'flash' : '') + (bad || over > 0 ? ' invalid' : '')}>
                  <th scope="row">{item.product}<small>{itemLabel(item)} · {item.sku}</small>
                    <small>Pedido {item.quantity} · recibido antes {item.received}{item.rejected ? ' · ' + item.rejected + ' no aceptadas antes' : ''}</small>
                    {(bad || over > 0) && <small className="pu-error" id={uid + item.id + 'err'}>{line.problems.join(' ')}</small>}
                    {over > 0 && <button type="button" className="pu-link" disabled={locked} onClick={() => update(item.id, {
                      accepted: String(line.left), diff: true, rejected: String((line.rejected ?? 0) + over), reason: 'excess' })}>Pasar {over} a excedente</button>}
                  </th>
                  <td data-label="Pendiente" className="num"><b>{line.left}</b></td>
                  <td data-label="Aceptadas ahora"><input className="qty" inputMode="numeric" value={row.accepted} disabled={locked} placeholder="0"
                    aria-label={'Unidades aceptadas de ' + item.sku} aria-invalid={line.accepted === undefined || over > 0 || undefined}
                    aria-describedby={bad || over > 0 ? uid + item.id + 'err' : undefined}
                    onChange={(event) => update(item.id, { accepted: event.target.value })} onKeyDown={(event) => {
                      if (event.key !== 'Enter') return;
                      // Enter moves to the next line, like a spreadsheet.
                      event.preventDefault();
                      const inputs = Array.from(document.querySelectorAll<HTMLInputElement>('#' + CSS.escape(uid) + ' input[aria-label^="Unidades aceptadas"]'));
                      const next = inputs[inputs.indexOf(event.currentTarget) + 1];
                      if (next) next.focus(); else confirmButton.current?.focus();
                    }} /></td>
                  <td data-label="Diferencias">{row.diff ? <div className="pu-diff">
                    <div className="pu-diff-pair">
                      <input inputMode="numeric" value={row.rejected} disabled={locked} placeholder="0" aria-label={'Unidades no aceptadas de ' + item.sku}
                        aria-invalid={line.rejected === undefined || undefined} onChange={(event) => update(item.id, { rejected: event.target.value })} />
                      <select value={row.reason} disabled={locked} aria-label={'Motivo de las no aceptadas de ' + item.sku}
                        aria-invalid={(showErrors && (line.rejected ?? 0) > 0 && !row.reason) || undefined}
                        onChange={(event) => update(item.id, { reason: event.target.value as RejectionReason | '' })}>
                        <option value="">Motivo…</option>
                        {(Object.keys(reasonLabels) as RejectionReason[]).map((reason) => <option key={reason} value={reason} title={reasonHelp[reason]}>{reasonLabels[reason]}</option>)}
                      </select>
                    </div>
                    <button type="button" className="pu-link danger" disabled={locked} onClick={() => update(item.id, { diff: false, rejected: '', reason: '' })}>Quitar diferencia</button>
                  </div> : <button type="button" className="pu-link" disabled={locked} onClick={() => update(item.id, { diff: true })}>＋ Registrar diferencia</button>}</td>
                  <td data-label="Costo unitario"><input className="cost" inputMode="decimal" value={row.cost} disabled={locked} placeholder="Opcional"
                    aria-label={'Costo unitario recibido de ' + item.sku} aria-invalid={line.cost === undefined || undefined}
                    onChange={(event) => update(item.id, { cost: event.target.value })} />
                    {item.unitCost !== null && <small>Pedido: {moneyText(order.currency, item.unitCost)}</small>}</td>
                </tr>;
              })}</tbody>
            </table>
          </div>
          <div className="pu-panelbody">
            <label className="pu-label">Observación<textarea value={note} onChange={(event) => setNote(event.target.value)} maxLength={300} disabled={locked}
              placeholder="Opcional. Ej. remito 0001-00004567, faltante avisado al proveedor" /></label>
          </div>
        </div>
      </div>
      <aside className="pu-side" aria-label="Resumen de la recepción">
        <div className="pu-card pu-sidebox">
          <h3>Resumen de la recepción</h3>
          <ul className="pu-sum">
            <li><span>Ingresan al stock</span><b>{acceptedTotal} u.</b></li>
            <li><span>No aceptadas</span><b>{rejectedTotal} u.</b></li>
            <li><span>Quedan pendientes</span><b>{leftAfter} u.</b></li>
            <li><span>Ubicación</span><b>{location?.name ?? '—'}</b></li>
            <li className="total"><span>Valor aceptado</span><b>{acceptedTotal ? (unknownCost ? 'Parcial · ' : '') + money(order.currency, value) : '—'}</b></li>
          </ul>
          <div className="pu-note"><p>El stock aumenta solo con las unidades aceptadas, una vez que confirmes. Si queda algo pendiente, podés registrar otra recepción después.</p></div>
          <button ref={confirmButton} type="submit" data-submit="receipt" className="pu-btn primary wide" disabled={locked || !locationId}>Revisar y confirmar</button>
          <button type="button" className="pu-btn wide" disabled={phase === 'saving'} onClick={onClose}>Cancelar</button>
        </div>
      </aside>
    </form>
    {reviewing && <Modal title="Confirmar recepción" returnTo={confirmButton.current} busy={phase === 'saving'} onClose={() => { if (phase === 'edit') setReviewing(false); }}
      footer={<>
        <button type="button" className="pu-btn" disabled={phase !== 'edit'} onClick={() => setReviewing(false)}>Volver a editar</button>
        <button type="button" className="pu-btn primary" disabled={phase === 'saving'} onClick={confirm}>
          {phase === 'saving' ? 'Registrando…' : phase === 'uncertain' ? 'Reintentar la misma recepción' : 'Confirmar recepción'}</button>
      </>}>
      <p><b>{orderCode(order.number)}</b> · {order.supplierName}</p>
      {phase === 'uncertain' && <p className="pu-alert warn" role="alert">{error}</p>}
      <div>{lines.filter((line) => (line.accepted ?? 0) || (line.rejected ?? 0)).map((line) => <div key={line.item.id} className="pu-statrow">
        <div><b>{line.item.product}</b><span className="sub">{itemLabel(line.item)} · {line.item.sku}</span></div>
        <div style={{ textAlign: 'right' }}><b>＋{line.accepted ?? 0} u.</b>
          {(line.rejected ?? 0) > 0 && <span className="sub">{line.rejected} {reasonLabels[line.row.reason as RejectionReason].toLowerCase()} · no ingresan</span>}</div>
      </div>)}</div>
      <div className="pu-note"><p>Ingresan <b>{acceptedTotal}</b> unidades a {location?.name ?? 'la ubicación elegida'}.{leftAfter ? ' Quedan ' + leftAfter + ' pendientes.' : ' El pedido queda completo.'}</p></div>
    </Modal>}
  </section>;
}

/** Shown after confirming: the result, the stock movement and the optional labels. */
export function ReceiptResult({ order, receipt, onFinish, onOpenOrder }: {
  order: PurchaseOrder; receipt: PurchaseReceipt; onFinish: () => void; onOpenOrder: () => void;
}) {
  const uid = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const [labels, setLabels] = useState(false);
  const labelButton = useRef<HTMLButtonElement>(null);
  const [current, setCurrent] = useState(order);
  useEffect(() => { heading.current?.focus(); }, []);
  useEffect(() => {
    let active = true;
    void api.purchaseOrder(order.storeId, order.id).then((fresh) => { if (active) setCurrent(fresh); }).catch(() => undefined);
    return () => { active = false; };
  }, [order.id, order.storeId]);
  const accepted = receipt.items.reduce((total, item) => total + item.accepted, 0);
  const rejected = receipt.items.reduce((total, item) => total + item.rejected, 0);
  const pending = current.status === 'CANCELLED' ? 0 : current.items.reduce((total, item) => total + item.quantity - item.received, 0);
  const products = new Map(order.items.map((item) => [item.id, item.productId]));
  const labelLines = receipt.items.filter((item) => item.accepted > 0)
    .map((item) => ({ productId: products.get(item.orderItemId) ?? '', variantId: item.variantId, count: item.accepted }));

  return <section className="pu" aria-labelledby={uid}>
    <nav className="pu-breadcrumb" aria-label="Ruta"><button type="button" className="pu-link" onClick={onFinish}>Compras</button> / {orderCode(order.number)} / Recepción registrada</nav>
    <div className="pu-card">
      <div className="pu-panelhead"><div className="pu-success"><span aria-hidden="true">✓</span>
        <div><h2 id={uid} tabIndex={-1} ref={heading}>Recepción registrada</h2>
          <p>{orderCode(order.number)} · {order.supplierName} · {accepted} {accepted === 1 ? 'unidad ingresó' : 'unidades ingresaron'} a {receipt.locationName}</p></div></div>
        <span className={'pu-pill ' + statusTone[current.status]}>{statusLabels[current.status]}</span></div>
      <div className="pu-panelbody">
        <dl className="pu-facts">
          <div><dt>Ingresaron al stock</dt><dd>{accepted} u.</dd></div>
          <div><dt>No aceptadas</dt><dd>{rejected} u.</dd></div>
          <div><dt>Quedan pendientes</dt><dd>{pending} u.</dd></div>
          <div><dt>Ubicación</dt><dd>{receipt.locationName}</dd></div>
        </dl>
        <div className="pu-table-wrap" style={{ border: '1px solid var(--p-line)', borderRadius: 12, overflow: 'hidden' }}>
          <table className="pu-table">
            <thead><tr><th scope="col">Variante</th><th scope="col" className="num">Aceptadas</th><th scope="col">Stock en {receipt.locationName}</th><th scope="col">No aceptadas</th><th scope="col">Costo</th></tr></thead>
            <tbody>{receipt.items.map((item) => <tr key={item.orderItemId}>
              <th scope="row">{item.product}<small>{itemLabel(item)} · {item.sku}</small></th>
              <td data-label="Aceptadas" className="num">{item.accepted}</td>
              <td data-label={'Stock en ' + receipt.locationName}>{item.stockBefore === null ? 'Sin cambios' : item.stockBefore + ' → ' + item.stockAfter}</td>
              <td data-label="No aceptadas">{item.rejected ? item.rejected + ' · ' + reasonLabels[item.rejectionReason ?? 'other'].toLowerCase() : '—'}</td>
              <td data-label="Costo">{item.accepted ? moneyText(order.currency, item.unitCost) : '—'}</td>
            </tr>)}</tbody>
          </table>
        </div>
        <div className="pu-note"><p>Las etiquetas son opcionales y no modifican el stock. Se usan los SKU y códigos que ya tienen los productos. Podés reimprimirlas después desde el pedido.</p></div>
        <div className="pu-actions">
          {labelLines.length > 0 && <button ref={labelButton} type="button" className="pu-btn primary" onClick={() => setLabels(true)}>🖨 Preparar etiquetas</button>}
          <button type="button" className="pu-btn" onClick={onFinish}>Finalizar</button>
          <button type="button" className="pu-btn" onClick={onOpenOrder}>Ver pedido</button>
        </div>
      </div>
    </div>
    {labels && <ReceiptLabels storeId={order.storeId} lines={labelLines} returnTo={labelButton.current} onClose={() => setLabels(false)} />}
  </section>;
}
