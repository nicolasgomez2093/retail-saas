import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { api, ApiError, type PriceAdjustInput, type PriceAdjustResult, type PriceConfiguration, type Product, type Store, type Variant } from '../../shared/api/client';
import { posPrice } from '../pos/priceDisplay';
import { LabelDialog } from './LabelDialog';
import { variantLabel } from './quickProduct';
import './bulk.css';

export type Selection = Map<string, string>;
export const maxSelection = 200;

/** Bar shown while products are selected in the catalog: what is selected and what can be done with it. */
export function BulkBar({ selected, onClear, onLabels, onPrices }: {
  selected: Selection; onClear: () => void; onLabels: (origin: HTMLElement) => void; onPrices: (origin: HTMLElement) => void;
}) {
  if (!selected.size) return null;
  const names = [...selected.values()];
  return <div className="bulk-bar" role="region" aria-label="Productos seleccionados">
    <div className="bulk-bar-text">
      <strong>{selected.size === 1 ? '1 producto seleccionado' : selected.size + ' productos seleccionados'}</strong>
      <small title={names.join(', ')}>{names.slice(0, 3).join(', ')}{names.length > 3 ? ' y ' + (names.length - 3) + ' más' : ''}</small>
    </div>
    <div className="bulk-bar-actions">
      <button type="button" onClick={(event) => onLabels(event.currentTarget)}>Imprimir etiquetas</button>
      <button type="button" onClick={(event) => onPrices(event.currentTarget)}>Cambiar precios</button>
      <button type="button" className="button-secondary" onClick={onClear}>Quitar selección</button>
    </div>
  </div>;
}

function Modal({ title, onClose, returnTo, children, wide = false }: {
  title: string; onClose: () => void; returnTo: HTMLElement | null; children: ReactNode; wide?: boolean;
}) {
  const titleId = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    return () => {
      element?.close();
      if (returnTo?.isConnected) returnTo.focus();
    };
  }, [returnTo]);
  return <dialog ref={dialog} className={'bulk-dialog' + (wide ? ' bulk-dialog-wide' : '')} aria-labelledby={titleId}
    onCancel={(event) => { event.preventDefault(); onClose(); }}>
    <header className="bulk-dialog-header"><h2 id={titleId}>{title}</h2>
      <button type="button" className="button-secondary" onClick={onClose}>Cerrar</button></header>
    {children}
  </dialog>;
}

/** Every active variant with a barcode of the selected products, in one label run. */
export function BulkLabels({ ids, storeId, returnTo, onClose }: { ids: string[]; storeId?: string; returnTo: HTMLElement | null; onClose: () => void }) {
  const [items, setItems] = useState<{ product: Product; variant: Variant }[] | null>(null);
  const [error, setError] = useState('');
  const key = ids.join(',');
  useEffect(() => {
    let current = true;
    void Promise.all(key.split(',').map((id) => api.product(id))).then((details) => {
      if (!current) return;
      setItems(details.filter((detail) => detail.product.status === 'active').flatMap((detail) =>
        detail.variants.filter((variant) => variant.status === 'active' && variant.barcodes.length).map((variant) => ({ product: detail.product, variant }))));
    }).catch((cause: unknown) => { if (current) setError(cause instanceof Error ? cause.message : 'No pudimos leer los productos.'); });
    return () => { current = false; };
  }, [key]);
  if (items && items.length) {
    return <LabelDialog data={items[0]} additional={items.slice(1)} storeId={storeId} returnTo={returnTo} onClose={onClose}
      countsHint={storeId ? "Sale una etiqueta por cada unidad en stock." : "Sale una etiqueta por variante."} />;
  }
  return <Modal title="Etiquetas de los productos" onClose={onClose} returnTo={returnTo}>
    {error ? <p role="alert">{error}</p> : !items ? <p role="status">Buscando las variantes…</p>
      : <p role="status">Los productos elegidos no tienen variantes activas con código de barras.</p>}
  </Modal>;
}

const rounds = [['', 'Sin redondear'], ['10', 'A $ 10'], ['100', 'A $ 100'], ['1000', 'A $ 1.000']] as const;

/** Raise or lower, in one step, the prices of the selected products in a price list. Always previews before writing. */
export function BulkPrices({ ids, branch, returnTo, onClose, onApplied }: {
  ids: string[]; branch?: Store; returnTo: HTMLElement | null; onClose: () => void; onApplied: () => void;
}) {
  const [config, setConfig] = useState<PriceConfiguration | null>(null);
  const [listId, setListId] = useState('');
  const [direction, setDirection] = useState<'up' | 'down'>('up');
  const [mode, setMode] = useState<'percent' | 'amount'>('percent');
  const [value, setValue] = useState('');
  const [round, setRound] = useState('');
  const [preview, setPreview] = useState<PriceAdjustResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState<PriceAdjustResult | null>(null);

  useEffect(() => {
    let current = true;
    void api.priceLists().then((data) => {
      if (!current) return;
      setConfig(data);
      const assigned = data.stores.find((store) => store.id === branch?.id)?.listId;
      setListId(assigned ?? data.lists[0]?.id ?? '');
    }).catch((cause: unknown) => {
      if (!current) return;
      setError(cause instanceof ApiError && cause.status === 403 ? 'No tenés permiso para cambiar precios.' : cause instanceof Error ? cause.message : 'No pudimos leer las listas de precios.');
    });
    return () => { current = false; };
  }, [branch?.id]);

  const input = (apply: boolean): PriceAdjustInput => ({ productIds: ids, mode, direction, value: value.trim().replace(',', '.'), round, apply });
  // Any change to the form invalidates the preview: what is applied is always what was seen.
  const change = <T,>(set: (value: T) => void) => (next: T) => { set(next); setPreview(null); setError(''); };

  async function run(apply: boolean) {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      const result = await api.adjustPrices(listId, input(apply));
      if (apply) { setDone(result); onApplied(); } else setPreview(result);
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 400 ? 'Revisá el valor: tiene que ser un número mayor a cero (los porcentajes de baja, menores a 100).'
        : cause instanceof Error ? cause.message : 'No pudimos calcular los precios.');
    } finally {
      setBusy(false);
    }
  }

  const listName = config?.lists.find((list) => list.id === listId)?.name ?? '';
  const changed = preview?.items.filter((item) => item.before !== item.after) ?? [];
  const money = (amount: string) => (preview?.currency ?? config?.currency ?? '') + ' ' + posPrice(amount);

  if (done) {
    return <Modal title="Precios actualizados" onClose={onClose} returnTo={returnTo}>
      <p className="bulk-done" role="status">✓ Se actualizaron {done.items.filter((item) => item.before !== item.after).length} precios de la lista «{listName}».</p>
      <div className="actions"><button type="button" onClick={onClose}>Listo</button></div>
    </Modal>;
  }

  return <Modal title="Cambiar precios" onClose={onClose} returnTo={returnTo} wide>
    <p className="bulk-intro">{ids.length === 1 ? '1 producto' : ids.length + ' productos'} seleccionados. Se cambian todos sus talles y colores.</p>
    {!config && !error && <p role="status">Cargando listas de precios…</p>}
    {config && config.lists.length === 0 && <p role="status">Todavía no hay listas de precios.</p>}
    {config && config.lists.length > 0 && <form className="bulk-price-form" onSubmit={(event: FormEvent) => { event.preventDefault(); void run(false); }}>
      {config.lists.length > 1 && <label>Lista de precios
        <select value={listId} onChange={(event) => change(setListId)(event.target.value)}>
          {config.lists.map((list) => <option key={list.id} value={list.id}>{list.name}</option>)}
        </select>
      </label>}
      <div className="bulk-segment" role="group" aria-label="Subir o bajar">
        <button type="button" aria-pressed={direction === 'up'} onClick={() => change(setDirection)('up')}>Subir</button>
        <button type="button" aria-pressed={direction === 'down'} onClick={() => change(setDirection)('down')}>Bajar</button>
      </div>
      <div className="bulk-amount">
        <input aria-label="Valor del cambio" inputMode="decimal" maxLength={12} required placeholder={mode === 'percent' ? 'Ej. 10' : 'Ej. 1000'}
          value={value} onChange={(event) => change(setValue)(event.target.value)} />
        <div className="bulk-segment" role="group" aria-label="En porcentaje o en pesos">
          <button type="button" aria-pressed={mode === 'percent'} onClick={() => change(setMode)('percent')}>%</button>
          <button type="button" aria-pressed={mode === 'amount'} onClick={() => change(setMode)('amount')}>$</button>
        </div>
      </div>
      <label>Redondeo
        <select value={round} onChange={(event) => change(setRound)(event.target.value)}>
          {rounds.map(([id, name]) => <option key={id} value={id}>{name}</option>)}
        </select>
      </label>
      <button type="submit" className="button-secondary" disabled={busy || !value.trim() || !listId}>Ver cómo quedan</button>
    </form>}
    {error && <p role="alert">{error}</p>}
    {preview && <section className="bulk-preview" aria-label="Vista previa de precios">
      <p><strong>{changed.length === 1 ? '1 precio cambia' : changed.length + ' precios cambian'}</strong> en «{listName}».
        {preview.withoutPrice > 0 && ' ' + (preview.withoutPrice === 1 ? '1 variante no tiene' : preview.withoutPrice + ' variantes no tienen') + ' precio en esta lista y queda igual.'}</p>
      {preview.items.length > 0 && <div className="bulk-table"><table>
        <thead><tr><th scope="col">Producto</th><th scope="col">Antes</th><th scope="col">Después</th></tr></thead>
        <tbody>{preview.items.map((item) => <tr key={item.variantId}>
          <th scope="row">{item.productName}<small>{variantLabel(item) || item.sku}</small></th>
          <td>{money(item.before)}</td><td><strong>{money(item.after)}</strong></td>
        </tr>)}</tbody>
      </table></div>}
      <div className="actions">
        <button type="button" disabled={busy || changed.length === 0} onClick={() => void run(true)}>
          {busy ? 'Aplicando…' : 'Aplicar ' + (changed.length === 1 ? '1 precio' : changed.length + ' precios')}</button>
        <button type="button" className="button-secondary" disabled={busy} onClick={onClose}>Cancelar</button>
      </div>
    </section>}
  </Modal>;
}
