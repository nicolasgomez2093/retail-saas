import { useEffect, useId, useMemo, useRef, useState, type FormEvent } from 'react';

import { api, ApiError, type PurchaseOrder, type PurchaseOrderInput, type ResolvedPrice, type Store, type Supplier } from '../../shared/api/client';
import { productHandoff } from './productHandoff';
import { itemLabel, lineCents, money, newKey, parseCost, sumCents } from './purchaseFormat';
import { SupplierPicker, VariantFinder, type Picked } from './PurchaseParts';

type Line = Picked & { quantity: string; cost: string };
type Attempt = { input: PurchaseOrderInput; key: string };
const maxLines = 100;

function quantityOf(raw: string) {
  const text = raw.trim();
  if (!/^\d{1,7}$/.test(text)) return undefined;
  const value = Number(text);
  return value >= 1 && value <= 1000000 ? value : undefined;
}

export function NewPurchaseOrder({ store, currency, onClose, onSaved, onCreateProduct }: {
  store: Store; currency: string; onClose: () => void; onSaved: (order: PurchaseOrder) => void; onCreateProduct?: () => void;
}) {
  const uid = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const [supplier, setSupplier] = useState<Supplier | null>(null);
  const [reference, setReference] = useState('');
  const [lines, setLines] = useState<Line[]>([]);
  const [flash, setFlash] = useState('');
  const [prices, setPrices] = useState<Record<string, ResolvedPrice | null>>({});
  const [phase, setPhase] = useState<'edit' | 'saving' | 'uncertain'>('edit');
  const [error, setError] = useState('');
  const [showErrors, setShowErrors] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const attempt = useRef<Attempt | null>(null);
  const running = useRef(false);
  const locked = phase !== 'edit';

  useEffect(() => { heading.current?.focus(); }, []);

  // A product created in the quick creation comes back here.
  useEffect(() => productHandoff.listen((result) => {
    const created = result.variants.filter((item) => item.variant.status === 'active').map((item) => ({ product: result.product, variant: item.variant }));
    setLines((current) => [...current, ...created.filter((item) => !current.some((line) => line.variant.id === item.variant.id))
      .map((item) => ({ ...item, quantity: '1', cost: '' }))].slice(0, maxLines));
    setFlash(created[0]?.variant.id ?? '');
    setError('');
  }), []);

  const lineIds = useMemo(() => lines.map((line) => line.variant.id).join(','), [lines]);
  useEffect(() => {
    const missing = lineIds.split(',').filter((id) => id && !(id in prices));
    if (!missing.length) return;
    let active = true;
    void Promise.all(missing.map((id) => api.resolvePrice(store.id, id).then((price) => [id, price] as const, () => [id, null] as const)))
      .then((rows) => { if (active) setPrices((current) => ({ ...current, ...Object.fromEntries(rows) })); });
    return () => { active = false; };
  }, [lineIds, prices, store.id]);

  useEffect(() => {
    if (!flash) return;
    const handle = window.setTimeout(() => setFlash(''), 1400);
    return () => window.clearTimeout(handle);
  }, [flash]);

  function pick(picked: Picked) {
    if (locked) return;
    const existing = lines.find((line) => line.variant.id === picked.variant.id);
    if (existing) {
      // A repeated read adds one unit to the same line instead of a duplicate.
      setLines((current) => current.map((line) => line.variant.id === picked.variant.id
        ? { ...line, quantity: String((quantityOf(line.quantity) ?? 0) + 1) } : line));
    } else if (lines.length >= maxLines) {
      setError('Un pedido admite hasta ' + maxLines + ' variantes. Registrá este y empezá otro.');
      return;
    } else {
      setLines((current) => [...current, { ...picked, quantity: '1', cost: '' }]);
    }
    setFlash(picked.variant.id);
  }

  const update = (id: string, change: Partial<Pick<Line, 'quantity' | 'cost'>>) =>
    setLines((current) => current.map((line) => line.variant.id === id ? { ...line, ...change } : line));

  const rows = lines.map((line) => {
    const quantity = quantityOf(line.quantity);
    const cost = parseCost(line.cost);
    return { line, quantity, cost, subtotal: quantity !== undefined && cost ? lineCents(quantity, cost) : null };
  });
  const invalid = rows.filter((row) => row.quantity === undefined || row.cost === undefined);
  const units = rows.reduce((total, row) => total + (row.quantity ?? 0), 0);
  const withoutCost = rows.filter((row) => row.cost === null).length;
  const total = sumCents(rows.map((row) => row.subtotal));

  async function record(operation: Attempt) {
    if (running.current) return;
    running.current = true;
    setPhase('saving');
    setError('');
    try {
      const order = await api.createPurchaseOrder(operation.input, operation.key);
      onSaved(order);
    } catch (cause) {
      const unknown = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      if (unknown) {
        setPhase('uncertain');
        setError('No sabemos si el pedido llegó a registrarse porque se cortó la conexión. Reintentá: si ya estaba registrado vas a ver el mismo pedido, sin duplicarlo.');
      } else {
        attempt.current = null;
        setPhase('edit');
        if (cause.code === 'purchase_supplier_unavailable') {
          setSupplier(null);
          setError('El proveedor ya no está activo. Elegí otro.');
        } else if (cause.code === 'purchase_variant_unavailable') {
          setError('Una de las variantes se desactivó mientras armabas el pedido. Quitala y volvé a registrar.');
        } else setError(cause.message);
      }
    } finally {
      running.current = false;
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Only the «Registrar pedido» button submits: Enter inside a quantity or
    // a search field must never register the order by accident.
    if ((event.nativeEvent as SubmitEvent).submitter?.dataset.submit !== 'order') return;
    if (locked || attempt.current) return;
    setShowErrors(true);
    if (!supplier) { setError('Elegí el proveedor del pedido.'); return; }
    if (!lines.length) { setError('Agregá al menos un producto.'); return; }
    if (invalid.length) {
      setError('Revisá cantidades y costos marcados: la cantidad va de 1 a 1.000.000 y el costo admite hasta dos decimales.');
      document.querySelector<HTMLInputElement>('#' + CSS.escape(uid) + ' [aria-invalid=true]')?.focus();
      return;
    }
    const operation: Attempt = { key: newKey(), input: {
      storeId: store.id, supplierId: supplier.id, ...(reference.trim() ? { reference: reference.trim() } : {}),
      items: rows.map((row) => ({ variantId: row.line.variant.id, quantity: row.quantity!, unitCost: row.cost ?? null })),
    } };
    attempt.current = operation;
    void record(operation);
  }

  return <section className="pu" aria-labelledby={uid + 'title'} id={uid}>
    <nav className="pu-breadcrumb" aria-label="Ruta"><button type="button" className="pu-link" disabled={locked} onClick={onClose}>Compras</button> / Nuevo pedido</nav>
    <header className="pu-head">
      <div><h2 id={uid + 'title'} tabIndex={-1} ref={heading}>Nuevo pedido</h2>
        <p>{store.name} · Registrá lo que le pediste al proveedor. El stock no cambia hasta que recibas la mercadería.</p></div>
    </header>
    {error && <p className={'pu-alert' + (phase === 'uncertain' ? ' warn' : '')} role="alert">{error}</p>}
    <form className="pu-layout pu-form" onSubmit={submit} noValidate>
      <div className="pu-main">
        <div className="pu-card">
          <div className="pu-panelhead"><div><h3>Proveedor y comprobante</h3><p>A quién le comprás y con qué referencia.</p></div></div>
          <div className="pu-panelbody pu-formgrid">
            <div className="wide pu-label"><span>Proveedor *</span>
              <SupplierPicker value={supplier} disabled={locked} onChange={(value) => { setSupplier(value); setError(''); }} />
              {showErrors && !supplier && <span className="pu-error">Elegí el proveedor.</span>}
            </div>
            <label>Referencia o comprobante
              <input value={reference} onChange={(event) => setReference(event.target.value)} maxLength={60} disabled={locked}
                placeholder="Ej. Nota de pedido 1234 o factura A 0001-00001234" />
              <span className="pu-hint">Opcional. Te ayuda a encontrar el pedido después.</span>
            </label>
            <div className="pu-label"><span>Destino</span><p style={{ margin: 0, fontWeight: 500 }}>{store.name}<br /><span className="pu-hint">La ubicación se elige al recibir.</span></p></div>
          </div>
        </div>
        <div className="pu-card">
          <div className="pu-panelhead"><div><h3>Productos</h3><p>Buscá por nombre o SKU, o escaneá el código. Si ya está en el pedido, suma una unidad.</p></div></div>
          <div className="pu-panelbody">
            <VariantFinder disabled={locked} chosen={new Set(lines.map((line) => line.variant.id))} onPick={pick}
              onCreateProduct={onCreateProduct ? () => { productHandoff.request(); onCreateProduct(); } : undefined} />
          </div>
          {lines.length > 0 ? <div className="pu-table-wrap">
            <table className="pu-table">
              <thead><tr><th scope="col">Variante</th><th scope="col" className="num">Cantidad</th><th scope="col">Costo unitario</th>
                <th scope="col" className="num">Subtotal</th><th scope="col"><span className="pos-sr-only">Quitar</span></th></tr></thead>
              <tbody>{rows.map(({ line, quantity, cost, subtotal }) => {
                const price = prices[line.variant.id];
                const sale = price?.amount ?? null;
                const margin = sale && cost && Number(sale) > 0 ? Math.round((Number(sale) - Number(cost)) / Number(sale) * 100) : null;
                const bad = showErrors && (quantity === undefined || cost === undefined);
                return <tr key={line.variant.id} className={(flash === line.variant.id ? 'flash' : '') + (bad ? ' invalid' : '')}>
                  <th scope="row">{line.product.name}<small>{itemLabel(line.variant)} · {line.variant.sku}</small>
                    <small>{price === undefined ? 'Consultando precio de venta…' : sale === null ? 'Sin precio de venta en esta sucursal'
                      : 'Precio de venta ' + money(price!.currency, BigInt(sale.replace('.', ''))) + (margin !== null ? ' · margen ' + margin + ' %' : '')}</small></th>
                  <td data-label="Cantidad" className="num"><input className="qty" inputMode="numeric" value={line.quantity} disabled={locked}
                    aria-label={'Cantidad de ' + line.variant.sku} aria-invalid={quantity === undefined || undefined}
                    onChange={(event) => update(line.variant.id, { quantity: event.target.value })} /></td>
                  <td data-label="Costo unitario"><input className="cost" inputMode="decimal" value={line.cost} disabled={locked} placeholder="Opcional"
                    aria-label={'Costo unitario de ' + line.variant.sku} aria-invalid={cost === undefined || undefined}
                    onChange={(event) => update(line.variant.id, { cost: event.target.value })} /></td>
                  <td data-label="Subtotal" className="num">{subtotal === null ? '—' : money(currency, subtotal)}</td>
                  <td className="pu-cell-actions"><button type="button" className="pu-icon" disabled={locked} aria-label={'Quitar ' + line.variant.sku}
                    onClick={() => setLines((current) => current.filter((item) => item.variant.id !== line.variant.id))}>×</button></td>
                </tr>;
              })}</tbody>
              <tfoot><tr><th scope="row">Total</th><td className="num" data-label="Unidades">{units.toLocaleString('es-AR')} u.</td><td />
                <td className="num" data-label="Total">{money(currency, total)}{withoutCost > 0 && <small>{withoutCost} sin costo</small>}</td><td /></tr></tfoot>
            </table>
          </div> : <div className="pu-empty" style={{ padding: '30px 20px', borderTop: '1px solid var(--p-line)' }}>
            <h3>Todavía no agregaste productos</h3><p>Escaneá un código o buscá por nombre para elegir la variante exacta.</p></div>}
        </div>
      </div>
      <aside className="pu-side" aria-label="Resumen del pedido">
        <div className="pu-card pu-sidebox">
          <h3>Resumen del pedido</h3>
          <ul className="pu-sum">
            <li><span>Proveedor</span><b>{supplier?.name ?? 'Sin elegir'}</b></li>
            <li><span>Renglones</span><b>{lines.length}</b></li>
            <li><span>Unidades</span><b>{units.toLocaleString('es-AR')}</b></li>
            <li className="total"><span>Total de costo</span><b>{money(currency, total)}</b></li>
          </ul>
          {withoutCost > 0 && lines.length > 0 && <p className="pu-hint">{withoutCost === 1 ? 'Un renglón' : withoutCost + ' renglones'} sin costo: el total es parcial. Podés informarlo al recibir.</p>}
          <div className="pu-note"><p>Registrar el pedido no modifica el stock ni los precios de venta. Las unidades ingresan cuando confirmás la recepción.</p></div>
          {phase === 'uncertain' ? <button type="button" className="pu-btn primary wide" onClick={() => { if (attempt.current) void record(attempt.current); }}>Reintentar el mismo registro</button>
            : <button type="submit" data-submit="order" className="pu-btn primary wide" disabled={locked}>{phase === 'saving' ? 'Registrando…' : 'Registrar pedido'}</button>}
          {discarding ? <div className="pu-note warn"><p>¿Descartar el pedido sin registrar?</p>
            <div className="pu-actions"><button type="button" className="pu-btn" onClick={() => setDiscarding(false)}>Seguir editando</button>
              <button type="button" className="pu-btn" onClick={onClose}>Descartar</button></div></div>
            : <button type="button" className="pu-btn wide" disabled={phase === 'saving'} onClick={() => (lines.length || supplier) && phase === 'edit' ? setDiscarding(true) : onClose()}>Cancelar</button>}
        </div>
      </aside>
    </form>
  </section>;
}

