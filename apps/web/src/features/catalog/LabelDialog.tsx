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
const sidePaddingMm = 3;
const maxCopies = 250;

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

function Label({ data, code, image, price }: { data: LabelData; code: string; image: BarcodeImage; price?: string }) {
  const attributes = variantLabel(data.variant);
  return (
    <div className="retail-label">
      <strong className="retail-label-product">{data.product.name}</strong>
      {attributes && <span className="retail-label-line">{attributes}</span>}
      {price && <span className="retail-label-line">{price}</span>}
      <img src={image.src} alt={`Código de barras ${code}`} style={{ width: `${image.widthMm}mm` }} />
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
  const [paper, setPaper] = useState(60);
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
    const text = counts[id] ?? initialCounts?.[id] ?? '1';
    const count = Number(text);
    const validCount = /^\d+$/.test(text) && count <= maxCopies;
    return { item, id, code, text, count, validCount, symbol: code ? barcodeImage(code, paper) : { error: 'Sin código imprimible.' } };
  }), [variants, codes, counts, paper]);
  const selected = rows.filter(row => !excluded.includes(row.id));
  const printing = selected.filter(row => row.validCount && row.count > 0);
  const total = printing.reduce((sum, row) => sum + row.count, 0);
  const valid = selected.every(row => row.validCount) && total > 0 && total <= maxCopies && printing.every(row => 'image' in row.symbol);
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
        <h2 id={titleId}>{additional.length ? 'Etiquetas del producto' : 'Etiqueta de variante'}</h2>
        <button ref={closeButton} type="button" className="button-secondary" onClick={onClose}>Cerrar</button>
      </header>
      <p className="label-identity"><strong>{data.product.name}</strong></p>
      {!preview && <>
        {countsHint && <p className="label-hint">{countsHint} Podés cambiar cada cantidad. Imprimir no modifica el stock.</p>}
        <label>Formato<select value={paper} onChange={event => setPaper(Number(event.target.value))}>
          <option value={60}>60 × 40 mm</option><option value={100}>100 × 40 mm</option>
        </select></label>
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
              <span><strong>{variantLabel(row.item.variant) || 'Sin atributos'}</strong><br />SKU {row.item.variant.sku}</span></label>
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
      <p role="status"><strong>{total} etiquetas</strong> · {printing.length} variantes · {paper} × 40 mm</p>
      {total > maxCopies && <p role="alert">Hasta {maxCopies} etiquetas por tirada.</p>}
      {preview && example && 'image' in example.symbol && <>
        <p>Imprimí al 100 %, sin encabezados ni pies de página. La impresión no modifica el stock.</p>
        <ul>{printing.map(row => <li key={row.id}>{row.item.variant.sku} · {row.count} copias</li>)}</ul>
        <div className={'label-preview' + (paper === 100 ? ' label-wide' : '')} aria-label="Vista previa de etiqueta">
          <Label data={example.item} code={example.code} image={example.symbol.image} price={reads[example.id]?.price} />
        </div>
      </>}
      <div className="actions">
        {preview ? <>
          <button type="button" className="button-secondary" onClick={() => showPreview(false)}>Volver a la selección</button>
          <button ref={stageButton} type="button" disabled={!valid} onClick={() => window.print()}>Imprimir o guardar PDF</button>
        </> : <button ref={stageButton} type="button" disabled={!valid} onClick={() => showPreview(true)}>Previsualizar etiquetas</button>}
      </div>
    </dialog>
    {preview && valid && createPortal(
      <div className={'label-print' + (paper === 100 ? ' label-wide' : '')} aria-hidden="true">
        {printing.flatMap(row => {
          const symbol = 'image' in row.symbol ? row.symbol.image : undefined;
          return symbol ? Array.from({ length: row.count }, (_, index) =>
            <Label key={row.id + ':' + index} data={row.item} code={row.code} image={symbol} price={reads[row.id]?.price} />) : [];
        })}
      </div>, document.body,
    )}
  </>;
}
