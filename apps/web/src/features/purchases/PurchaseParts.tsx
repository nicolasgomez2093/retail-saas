import { useEffect, useId, useRef, useState, type ReactNode } from 'react';

import { api, ApiError, type Product, type Supplier, type Variant } from '../../shared/api/client';
import { LabelDialog } from '../catalog/LabelDialog';
import { barcodePattern } from '../catalog/quickProduct';
import { SupplierDialog } from '../suppliers/Suppliers';
import { itemLabel } from './purchaseFormat';

export type Picked = { product: Product; variant: Variant };

/** Opens a native modal <dialog> on mount and gives focus back on unmount. */
export function useModal(returnTo: HTMLElement | null) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    if (!element.open) element.showModal();
    return () => {
      element.close();
      if (returnTo?.isConnected) returnTo.focus();
    };
  }, [returnTo]);
  return ref;
}

export function Modal({ title, returnTo, onClose, wide = false, busy = false, footer, children }: {
  title: ReactNode; returnTo: HTMLElement | null; onClose: () => void; wide?: boolean; busy?: boolean; footer?: ReactNode; children: ReactNode;
}) {
  const uid = useId();
  const ref = useModal(returnTo);
  return <dialog ref={ref} className={'pu-dialog' + (wide ? ' wide' : '')} aria-labelledby={uid}
    onCancel={(event) => { event.preventDefault(); if (!busy) onClose(); }}>
    <header><h2 id={uid}>{title}</h2><button type="button" className="pu-close" aria-label="Cerrar" disabled={busy} onClick={onClose}>×</button></header>
    <div className="pu-dialog-body">{children}</div>
    {footer && <footer>{footer}</footer>}
  </dialog>;
}

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No pudimos completar la búsqueda.';
}

/**
 * Finds the exact variant to buy or receive: a barcode read adds it directly,
 * a name or SKU lists products and their variants to choose from. Nothing is
 * added unless the code matches one variant or the person picks it.
 */
export function VariantFinder({ disabled, chosen, onPick, onCreateProduct, label = 'Buscar o escanear producto' }: {
  disabled: boolean; chosen: Set<string>; onPick: (picked: Picked, via: 'scan' | 'click') => void;
  onCreateProduct?: () => void; label?: string;
}) {
  const uid = useId();
  const input = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<{ tone: '' | 'warn' | 'danger'; text: string } | null>(null);
  const [results, setResults] = useState<{ query: string; products: Product[]; hasMore: boolean } | null>(null);
  const [open, setOpen] = useState<Record<string, Variant[] | 'loading' | { error: string }>>({});

  async function expand(product: Product) {
    if (Array.isArray(open[product.id])) {
      setOpen((current) => Object.fromEntries(Object.entries(current).filter(([id]) => id !== product.id)));
      return;
    }
    setOpen((current) => ({ ...current, [product.id]: 'loading' }));
    try {
      const detail = await api.product(product.id);
      setOpen((current) => ({ ...current, [product.id]: detail.variants.filter((variant) => variant.status === 'active') }));
    } catch (cause) {
      setOpen((current) => ({ ...current, [product.id]: { error: errorText(cause) } }));
    }
  }

  // No nested <form>: the finder lives inside the order form, so Enter is
  // handled here and never submits (or reloads) the surrounding page.
  async function search() {
    const text = query.trim();
    if (!text || busy || disabled) return;
    setBusy(true);
    setFeedback(null);
    try {
      if (barcodePattern.test(text)) {
        try {
          const match = await api.barcode(text);
          if (match.variant.status !== 'active' || match.product.status !== 'active') {
            setFeedback({ tone: 'danger', text: match.product.name + ' · ' + itemLabel(match.variant) + ' está inactiva y no se puede comprar.' });
            return;
          }
          onPick({ product: match.product, variant: match.variant }, 'scan');
          setFeedback({ tone: '', text: '✓ ' + match.product.name + ' · ' + itemLabel(match.variant) + ' (' + match.variant.sku + ')' });
          setQuery('');
          setResults(null);
          return;
        } catch (cause) {
          if (!(cause instanceof ApiError) || cause.status !== 404) throw cause;
        }
      }
      const page = await api.products(text, 0);
      const products = page.items.filter((product) => product.status === 'active');
      setResults({ query: text, products, hasMore: page.hasMore });
      setOpen({});
      if (products.length === 1) void expand(products[0]);
      if (!products.length) setFeedback({ tone: 'warn', text: 'No encontramos «' + text + '» por nombre, SKU ni código de barras.' });
    } catch (cause) {
      setFeedback({ tone: 'danger', text: errorText(cause) });
    } finally {
      setBusy(false);
      input.current?.focus();
    }
  }

  return <div className="pu-finder">
    <div className="pu-finder-bar" role="search" aria-label={label}>
      <label className="pu-search"><span aria-hidden="true">⌕</span>
        <input ref={input} id={uid} value={query} disabled={disabled} maxLength={120} autoComplete="off" aria-label={label}
          placeholder="Nombre, SKU o código de barras (podés escanear)" onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void search(); } }} />
      </label>
      <button type="button" className="pu-btn" disabled={disabled || busy || !query.trim()} onClick={() => void search()}>{busy ? 'Buscando…' : 'Buscar'}</button>
      {onCreateProduct && <button type="button" className="pu-btn" disabled={disabled} onClick={onCreateProduct}>＋ Crear producto nuevo</button>}
    </div>
    {feedback && <p className={'pu-feedback ' + feedback.tone} role={feedback.tone === 'danger' ? 'alert' : 'status'}>{feedback.text}</p>}
    {results && results.products.length > 0 && <ul className="pu-results" aria-label={'Resultados para ' + results.query}>
      {results.products.slice(0, 8).map((product) => {
        const state = open[product.id];
        return <li key={product.id}>
          <div className="pu-result-head">
            <div><b>{product.name}</b><small>{Array.isArray(state) ? state.length + (state.length === 1 ? ' variante activa' : ' variantes activas') : 'Elegí la variante exacta'}</small></div>
            <button type="button" className="pu-link" aria-expanded={Array.isArray(state)} disabled={disabled} onClick={() => void expand(product)}>
              {Array.isArray(state) ? 'Ocultar variantes' : 'Ver variantes'}</button>
          </div>
          {state === 'loading' && <p className="pu-hint" style={{ padding: '0 16px 14px' }} role="status">Cargando variantes…</p>}
          {state && !Array.isArray(state) && state !== 'loading' && <p className="pu-error" style={{ padding: '0 16px 14px' }} role="alert">{state.error}</p>}
          {Array.isArray(state) && <div className="pu-variants" role="group" aria-label={'Variantes de ' + product.name}>
            {state.length === 0 && <span className="pu-hint">No tiene variantes activas.</span>}
            {state.map((variant) => <button key={variant.id} type="button" className="pu-chip" disabled={disabled}
              aria-pressed={chosen.has(variant.id)} onClick={() => {
                onPick({ product, variant }, 'click');
                setFeedback({ tone: '', text: '✓ ' + product.name + ' · ' + itemLabel(variant) });
              }}>{itemLabel(variant)}<small>{variant.sku}</small></button>)}
          </div>}
        </li>;
      })}
      {(results.hasMore || results.products.length > 8) && <li><p className="pu-hint" style={{ padding: '12px 16px' }}>Hay más resultados: escribí un nombre o SKU más preciso.</p></li>}
    </ul>}
  </div>;
}

/** Choose an active supplier, or create one without leaving the purchase. */
export function SupplierPicker({ value, disabled, onChange }: { value: Supplier | null; disabled: boolean; onChange: (supplier: Supplier | null) => void }) {
  const uid = useId();
  const [query, setQuery] = useState('');
  const [options, setOptions] = useState<{ query: string; items: Supplier[]; canManage: boolean; error?: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const creator = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (value) return;
    let active = true;
    const handle = window.setTimeout(() => {
      void api.suppliers(query.trim(), 0).then((page) => {
        if (active) setOptions({ query, items: page.items.filter((item) => item.status === 'active'), canManage: page.canManage });
      }).catch((cause: unknown) => {
        if (active) setOptions({ query, items: [], canManage: false, error: errorText(cause) });
      });
    }, 250);
    return () => { active = false; window.clearTimeout(handle); };
  }, [query, value]);

  if (value) return <div className="pu-supplier">
    <div><b>{value.name}</b><small>{[value.taxId && 'CUIT ' + value.taxId, value.contactName, value.phone, value.email].filter(Boolean).join(' · ') || 'Sin datos de contacto'}</small></div>
    <button type="button" className="pu-link" disabled={disabled} onClick={() => onChange(null)}>Cambiar</button>
  </div>;

  return <div>
    <div className="pu-finder-bar">
      <label className="pu-search"><span aria-hidden="true">⌕</span>
        <input id={uid} value={query} disabled={disabled} maxLength={160} autoComplete="off" aria-label="Buscar proveedor"
          placeholder="Buscar proveedor por nombre, CUIT o teléfono" onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') event.preventDefault(); }} />
      </label>
      {options?.canManage && <button ref={creator} type="button" className="pu-btn" disabled={disabled} onClick={() => setCreating(true)}>＋ Nuevo proveedor</button>}
    </div>
    {options?.error && <p className="pu-error" role="alert">{options.error}</p>}
    {options && !options.error && (options.items.length ? <ul className="pu-options" aria-label="Proveedores activos">
      {options.items.map((item) => <li key={item.id}><button type="button" disabled={disabled} onClick={() => onChange(item)}>
        <b>{item.name}</b><small>{[item.taxId && 'CUIT ' + item.taxId, item.contactName, item.phone].filter(Boolean).join(' · ') || 'Sin datos de contacto'}</small>
      </button></li>)}
    </ul> : <p className="pu-hint" style={{ marginTop: 8 }}>{query.trim() ? 'No hay proveedores activos con ese dato.' : 'Todavía no hay proveedores activos.'}
      {options.canManage && ' Registralo con «Nuevo proveedor».'}</p>)}
    {!options && <p className="pu-hint" style={{ marginTop: 8 }} role="status">Consultando proveedores…</p>}
    {creating && <SupplierDialog supplier={null} initialName={query.trim()} returnTo={creator.current}
      onClose={() => setCreating(false)} onSaved={(supplier) => { setCreating(false); onChange(supplier); }}
      onUseExisting={(existing) => {
        setCreating(false);
        void api.suppliers(existing.name, 0).then((page) => {
          const found = page.items.find((item) => item.id === existing.id);
          if (found?.status === 'active') onChange(found);
          else setQuery(existing.name);
        });
      }} />}
  </div>;
}

/**
 * Prepares labels for received units with the shared label dialog. Codes are
 * read from the catalog as they are (never regenerated) and the suggested
 * copies are the accepted units, which the person can change.
 */
export function ReceiptLabels({ storeId, lines, returnTo, onClose }: {
  storeId: string; lines: { productId: string; variantId: string; count: number }[]; returnTo: HTMLElement | null; onClose: () => void;
}) {
  const [data, setData] = useState<{ items: Picked[] } | { error: string } | null>(null);
  const linesKey = JSON.stringify(lines);
  useEffect(() => {
    let active = true;
    const wanted: typeof lines = JSON.parse(linesKey);
    void Promise.all([...new Set(wanted.map((line) => line.productId))].map((id) => api.product(id))).then((details) => {
      const items = wanted.flatMap((line) => {
        const detail = details.find((item) => item.product.id === line.productId);
        const variant = detail?.variants.find((item) => item.id === line.variantId);
        return detail && variant ? [{ product: detail.product, variant }] : [];
      });
      if (active) setData(items.length ? { items } : { error: 'No encontramos las variantes de esta recepción en el catálogo.' });
    }).catch((cause: unknown) => {
      if (active) setData({ error: errorText(cause) });
    });
    return () => { active = false; };
  }, [linesKey]);

  if (!data) return <p className="pu-toast" role="status">Preparando etiquetas…</p>;
  if ('error' in data) return <Modal title="Etiquetas" returnTo={returnTo} onClose={onClose}
    footer={<button type="button" className="pu-btn primary" onClick={onClose}>Cerrar</button>}>
    <p className="pu-alert" role="alert">{data.error}</p>
  </Modal>;
  return <LabelDialog data={data.items[0]} additional={data.items.slice(1)} storeId={storeId} returnTo={returnTo} onClose={onClose}
    initialCounts={Object.fromEntries(lines.map((line) => [line.variantId, String(line.count)]))}
    countsHint="Sugerimos una etiqueta por unidad aceptada en esta recepción. Imprimir no modifica el stock." />;
}
