import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import JsBarcode from 'jsbarcode';
import { api, type Product, type Variant } from '../../shared/api/client';
import './labels.css';
import { posPrice } from '../pos/priceDisplay';
import { variantLabel } from './quickProduct';

type BarcodeImage = { src: string; widthMm: number };
type LabelData = { product: Product; variant: Variant };

const pixelsPerModule = 3;
const moduleWidthMm = 0.254;
const paperWidthMm = 60;

// Label rolls common on thermal label printers such as the X-Printer XP-470B (up to 108 mm wide).
export const labelFormats = [
  { id: '60x40', width: 60, height: 40, name: '60 × 40 mm' },
  { id: '50x30', width: 50, height: 30, name: '50 × 30 mm' },
  { id: '50x25', width: 50, height: 25, name: '50 × 25 mm' },
  { id: '40x30', width: 40, height: 30, name: '40 × 30 mm' },
  { id: '100x50', width: 100, height: 50, name: '100 × 50 mm' },
  { id: '100x40', width: 100, height: 40, name: '100 × 40 mm' },
] as const;
type LabelFormat = typeof labelFormats[number];
const formatKey = 'etiquetas:formato';
function savedFormat(): LabelFormat {
  try { return labelFormats.find(item => item.id === localStorage.getItem(formatKey)) ?? labelFormats[0]; } catch { return labelFormats[0]; }
}
function barcodeHeight(format: LabelFormat) {
  return format.height >= 40 ? 12 : format.height >= 30 ? 9 : 7;
}
const sidePaddingMm = 3;
const maxCopies = 500;

function barcodeImage(code: string, paper = paperWidthMm): { image: BarcodeImage } | { error: string } {
  try {
    const canvas = document.createElement('canvas');
    JsBarcode(canvas, code, {
      format: 'CODE128',
      width: pixelsPerModule,
      height: 144,
      margin: 0,
      marginLeft: pixelsPerModule * 10,
      marginRight: pixelsPerModule * 10,
      displayValue: false,
      background: '#ffffff',
      lineColor: '#000000',
    });
    const widthMm = canvas.width * moduleWidthMm / pixelsPerModule;
    if (widthMm > paper - 2 * sidePaddingMm) {
      return { error: 'Impresión bloqueada para este código. El símbolo necesita ' +
        (Math.ceil(widthMm * 10) / 10).toFixed(1).replace('.', ',') +
        ' mm con márgenes de lectura. Elegí un formato más ancho o un código alternativo de la variante.' };
    }
    return { image: { src: canvas.toDataURL('image/png'), widthMm } };
  } catch {
    return { error: 'No pudimos generar el código de barras para esta etiqueta.' };
  }
}

function Label({ data, code, image, price, format }: { data: LabelData; code: string; image: BarcodeImage; price?: string; format: LabelFormat }) {
  const attributes = variantLabel(data.variant);
  return (
    <div className="retail-label" data-compact={format.height < 30 || undefined} style={{ width: format.width + 'mm', height: format.height + 'mm' }}>
      <strong className="retail-label-product">{data.product.name}</strong>
      {attributes && <span className="retail-label-line">{attributes}</span>}
      {price && <span className="retail-label-line">{price}</span>}
      <img src={image.src} alt={`Código de barras ${code}`} style={{ width: `${image.widthMm}mm`, height: barcodeHeight(format) + 'mm' }} />
      <span className="retail-label-code">{code}</span>
    </div>
  );
}

export function LabelDialog({ data, returnTo, onClose, additional = [], storeId, initialCounts, countsHint }: {
  data: LabelData; returnTo: HTMLElement | null; onClose: () => void; additional?: LabelData[]; storeId?: string;
  /** Suggested copies per variant, for example the units entered when the product was created. */
  initialCounts?: Record<string, string>; countsHint?: string;
}) {
  const titleId = useId();
  const closeButton = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const variants = useMemo(() => [data, ...additional], [data, additional]);
  const [codes, setCodes] = useState<Record<string, string>>({});
  const [counts, setCounts] = useState<Record<string, string>>({});
  const [excluded, setExcluded] = useState<string[]>([]);
  const [format, setFormat] = useState<LabelFormat>(savedFormat);
  const paper = format.width;
  const products = new Set(variants.map(item => item.product.id)).size;
  // Commerce choice in Configuración; until it is read, labels keep the price as they always did.
  const [showPrice, setShowPrice] = useState(true);
  useEffect(() => {
    let current = true;
    void api.labelSettings().then(settings => { if (current) setShowPrice(settings.showPrice); }).catch(() => undefined);
    return () => { current = false; };
  }, []);
  const [preview, setPreview] = useState(false);
  const stageButton = useRef<HTMLButtonElement>(null);
  const changedStage = useRef(false);
  function showPreview(value: boolean) {
    changedStage.current = true;
    setPreview(value);
  }
  useEffect(() => {
    if (changedStage.current) stageButton.current?.focus();
  }, [preview]);
  const [reads, setReads] = useState<Record<string, { stock?: number | null; price?: string; priceRead: boolean }>>({});
  const variantsKey = JSON.stringify(variants.map(item => item.variant.id));
  useEffect(() => {
    if (!storeId) return;
    let current = true;
    const ids: string[] = JSON.parse(variantsKey);
    void Promise.all(ids.map(async id => {
      const [stock, price] = await Promise.allSettled([api.existence(storeId, id), api.resolvePrice(storeId, id)]);
      return [id, {
        stock: stock.status === 'fulfilled' ? stock.value.item.sellable : undefined,
        price: price.status === 'fulfilled' && price.value.amount !== null ? price.value.currency + ' ' + posPrice(price.value.amount) : undefined,
        priceRead: price.status === 'fulfilled',
      }] as const;
    })).then(result => { if (current) setReads(Object.fromEntries(result)); });
    return () => { current = false; };
  }, [storeId, variantsKey]);

  const rows = useMemo(() => variants.map(item => {
    const id = item.variant.id;
    const code = codes[id] ?? item.variant.barcodes[0] ?? '';
    // By default, one label per unit in stock of the branch; without a branch (or before reading it), one per variant.
    const stock = reads[id]?.stock;
    const text = counts[id] ?? initialCounts?.[id] ?? (typeof stock === 'number' ? String(Math.max(0, stock)) : stock === null ? '0' : '1');
    const count = Number(text);
    const validCount = /^\d+$/.test(text) && count <= maxCopies;
    return { item, id, code, text, count, validCount, symbol: code ? barcodeImage(code, paper) : { error: 'Sin código imprimible.' } };
  }), [variants, codes, counts, paper, reads, initialCounts]);
  const selected = rows.filter(row => !excluded.includes(row.id));
  const printing = selected.filter(row => row.validCount && row.count > 0);
  const total = printing.reduce((sum, row) => sum + row.count, 0);
  const stockPending = Boolean(storeId && !initialCounts) && variants.some(item => !(item.variant.id in reads));
  const valid = !stockPending && selected.every(row => row.validCount) && total > 0 && total <= maxCopies && printing.every(row => 'image' in row.symbol);
  const example = printing.find(row => 'image' in row.symbol);
  const hasStock = selected.some(row => typeof reads[row.id]?.stock === 'number');

  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    closeButton.current?.focus();
    return () => {
      element?.close();
      if (returnTo?.isConnected && returnTo.getClientRects().length) returnTo.focus();
    };
  }, [returnTo]);

  return <>
    <dialog ref={dialog} className="label-dialog label-batch-dialog" aria-labelledby={titleId} onCancel={event => {
      event.preventDefault();
      if (preview) showPreview(false); else onClose();
    }}>
      <header className="label-dialog-header">
        <h2 id={titleId}>{products > 1 ? 'Etiquetas de ' + products + ' productos' : additional.length ? 'Etiquetas del producto' : 'Etiqueta de variante'}</h2>
        <button ref={closeButton} type="button" className="button-secondary" onClick={onClose}>Cerrar</button>
      </header>
      {products === 1 && <p className="label-identity"><strong>{data.product.name}</strong></p>}
      <p className="label-hint">{showPrice ? 'Las etiquetas llevan precio.' : 'Las etiquetas salen sin precio.'} Lo podés cambiar en Configuración › Etiquetas.</p>
      {!preview && <>
        {(countsHint || (storeId && !initialCounts)) && <p className="label-hint">
          {countsHint ?? 'Sale una etiqueta por cada unidad en stock.'} Podés cambiar cada cantidad. Imprimir no modifica el stock.</p>}
        {stockPending && <p role="status" className="label-hint">Leyendo el stock de la sucursal…</p>}
        <label>Tamaño de etiqueta<select value={format.id} onChange={event => {
          const next = labelFormats.find(item => item.id === event.target.value) ?? labelFormats[0];
          setFormat(next);
          try { localStorage.setItem(formatKey, next.id); } catch { /* the choice lasts for this dialog */ }
        }}>
          {labelFormats.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select></label>
        <p className="label-hint">Usá el mismo tamaño que el rollo de la impresora y que el papel configurado en su driver.</p>
        <div className="actions">
          <button type="button" className="button-secondary" onClick={() => setExcluded([])}>Seleccionar todas</button>
          <button type="button" className="button-secondary" onClick={() => setExcluded(rows.map(row => row.id))}>Desmarcar todas</button>
          <button type="button" className="button-secondary" onClick={() => setCounts(Object.fromEntries(rows.map(row => [row.id, '1'])))}>Una por variante</button>
          {initialCounts && <button type="button" className="button-secondary" onClick={() => setCounts({})}>Usar unidades ingresadas</button>}
          {storeId && <button type="button" className="button-secondary" disabled={!hasStock} onClick={() => setCounts(current => ({
            ...current, ...Object.fromEntries(selected.flatMap(row => {
              const stock = reads[row.id]?.stock;
              return typeof stock === 'number' ? [[row.id, String(Math.max(0, stock))]] : [];
            })),
          }))}>Usar stock vendible</button>}
        </div>
        <ul className="label-batch-rows">{rows.map(row => {
          const selected = !excluded.includes(row.id);
          const read = reads[row.id];
          return <li key={row.id} className="commercial-label-row">
            <label className="checkbox"><input type="checkbox" checked={selected} onChange={event => setExcluded(current =>
              event.target.checked ? current.filter(id => id !== row.id) : [...current, row.id])} />
              <span>{products > 1 && <>{row.item.product.name}<br /></>}<strong>{variantLabel(row.item.variant) || 'Sin atributos'}</strong><br />SKU {row.item.variant.sku}</span></label>
            <label>Código<select name="labelCode" value={row.code} onChange={event => setCodes(current => ({ ...current, [row.id]: event.target.value }))}>
              {!row.code && <option value="">Sin código</option>}
              {row.item.variant.barcodes.map(code => <option key={code}>{code}</option>)}
            </select></label>
            <label>Copias<input name="labelCopies" aria-label={'Copias de ' + row.item.variant.sku} type="number" min={0} max={maxCopies} step={1} value={row.text}
              aria-invalid={selected && !row.validCount} onChange={event => setCounts(current => ({ ...current, [row.id]: event.target.value }))} /></label>
            {storeId && <small>{read?.price ?? (read?.priceRead ? 'Sin precio' : 'Precio no verificado')} · {typeof read?.stock === 'number' ? read.stock + ' vendibles' : read?.stock === null ? 'Sin saldo' : 'Stock no verificado'}</small>}
            {selected && !row.validCount && <p role="alert">Ingresá entre 0 y {maxCopies} copias enteras.</p>}
            {selected && row.count > 0 && 'error' in row.symbol && <p role="status">{row.symbol.error}</p>}
          </li>;
        })}</ul>
      </>}
      <p role="status"><strong>{total} etiquetas</strong> · {printing.length} variantes · {format.name}</p>
      {total > maxCopies && <p role="alert">Hasta {maxCopies} etiquetas por tirada.</p>}
      {preview && example && 'image' in example.symbol && <>
        <div className="label-preview" aria-label="Vista previa de etiqueta">
          <Label data={example.item} code={example.code} image={example.symbol.image} price={showPrice ? reads[example.id]?.price : undefined} format={format} />
        </div>
        <p>Imprimí al 100 %, sin encabezados ni pies de página. La impresión no modifica el stock.</p>
        <details className="label-run">
          <summary>Ver qué se imprime ({printing.length === 1 ? '1 variante' : printing.length + ' variantes'})</summary>
          <ul>{printing.map(row => <li key={row.id}>{products > 1 ? row.item.product.name + ' · ' : ''}{variantLabel(row.item.variant) || row.item.variant.sku}
            {' · '}{row.count === 1 ? '1 copia' : row.count + ' copias'}</li>)}</ul>
        </details>
      </>}
      <div className="actions">
        {preview ? <>
          <button type="button" className="button-secondary" onClick={() => showPreview(false)}>Volver a la selección</button>
          <button ref={stageButton} type="button" disabled={!valid} onClick={() => window.print()}>Imprimir o guardar PDF</button>
        </> : <button ref={stageButton} type="button" disabled={!valid} onClick={() => showPreview(true)}>Previsualizar etiquetas</button>}
      </div>
    </dialog>
    {preview && valid && createPortal(
      <div className="label-print" aria-hidden="true">
        <style>{`@page retail-label { size: ${format.width}mm ${format.height}mm; margin: 0; }`}</style>
        {printing.flatMap(row => {
          const symbol = 'image' in row.symbol ? row.symbol.image : undefined;
          return symbol ? Array.from({ length: row.count }, (_, index) =>
            <Label key={row.id + ':' + index} data={row.item} code={row.code} image={symbol} price={showPrice ? reads[row.id]?.price : undefined} format={format} />) : [];
        })}
      </div>, document.body,
    )}
  </>;
}
