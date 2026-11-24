import { useEffect, useState } from 'react';

import { api, type PurchaseOrder, type PurchaseReceiptPage } from '../../shared/api/client';
import { itemLabel, moneyText, reasonLabels } from './purchaseFormat';

const pageSize = 25;

/** Confirmed receipts are immutable: they are only listed, never edited. */
export function PurchaseReceiptHistory({ order, revision, timezone, onLabels }: {
  order: PurchaseOrder; revision: number; timezone: string; onLabels: (lines: { productId: string; variantId: string; count: number }[], button: HTMLButtonElement) => void;
}) {
  const [offset, setOffset] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: PurchaseReceiptPage; error?: string } | null>(null);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const key = [order.id, offset, revision, attempt].join(':');
  const current = result?.key === key ? result : null;

  useEffect(() => {
    let active = true;
    void api.purchaseReceipts(order.storeId, order.id, offset).then((page) => {
      if (active) setResult({ key, page });
    }).catch((cause: unknown) => {
      if (active) setResult({ key, error: cause instanceof Error ? cause.message : 'No pudimos cargar las recepciones.' });
    });
    return () => { active = false; };
  }, [key, offset, order.id, order.storeId]);

  async function downloadCSV() {
    setExporting(true);
    setExportError('');
    try {
      const file = await api.downloadPurchaseReceiptsCSV(order.storeId, order.id);
      const url = URL.createObjectURL(file);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'recepciones-de-compra.csv';
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : 'No pudimos descargar las recepciones.');
    } finally {
      setExporting(false);
    }
  }

  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });
  const products = new Map(order.items.map((item) => [item.id, item.productId]));
  const page = current?.page;
  return <section aria-label="Recepciones del pedido">
    <h3>Recepciones{page && page.summary.receiptCount > 0 ? ' (' + page.summary.receiptCount + ')' : ''}</h3>
    {!current && <p className="pu-hint" role="status">Consultando recepciones…</p>}
    {current?.error && <p className="pu-alert" role="alert">{current.error} <button type="button" className="pu-link" onClick={() => setAttempt((value) => value + 1)}>Reintentar</button></p>}
    {page && (page.items.length === 0 ? <p className="pu-hint">Todavía no se recibió mercadería de este pedido.</p> : <>
      {page.items.map((receipt, index) => {
        const accepted = receipt.items.reduce((total, item) => total + item.accepted, 0);
        const labelLines = receipt.items.filter((item) => item.accepted > 0)
          .map((item) => ({ productId: products.get(item.orderItemId) ?? '', variantId: item.variantId, count: item.accepted }));
        return <article key={receipt.id} className="pu-receipt" aria-label={'Recepción del ' + dates.format(new Date(receipt.receivedAt))}>
          <header>
            <div><b>Recepción {page.summary.receiptCount - offset - index} · ＋{accepted} u.</b>
              <small>{dates.format(new Date(receipt.receivedAt))} · {receipt.locationName}{receipt.note ? ' · ' + receipt.note : ''}</small></div>
            {labelLines.length > 0 && <button type="button" className="pu-btn" onClick={(event) => onLabels(labelLines, event.currentTarget)}>🖨 Etiquetas</button>}
          </header>
          <ul>{receipt.items.map((item) => <li key={item.orderItemId}>
            <div>{item.product} · {itemLabel(item)}<br /><span>{item.sku}{item.accepted ? ' · ' + moneyText(order.currency, item.unitCost) : ''}</span></div>
            <div style={{ textAlign: 'right' }}>
              {item.accepted > 0 && <b>＋{item.accepted}{item.stockBefore !== null && <span> · stock {item.stockBefore} → {item.stockAfter}</span>}</b>}
              {item.rejected > 0 && <><br /><span>{item.rejected} {(item.rejectionReason ? reasonLabels[item.rejectionReason] : 'No aceptadas').toLowerCase()} · no ingresaron</span></>}
            </div>
          </li>)}</ul>
        </article>;
      })}
      {page.summary.rejectedUnits > 0 && <p className="pu-hint" style={{ marginTop: 10 }}>{page.summary.rejectedUnits} unidades no aceptadas en total: quedaron registradas pero no ingresaron al stock.</p>}
      <div className="pu-actions" style={{ marginTop: 12 }}>
        {(offset > 0 || page.hasMore) && <>
          <button type="button" className="pu-btn" disabled={offset === 0} onClick={() => setOffset(offset - pageSize)}>← Anteriores</button>
          <button type="button" className="pu-btn" disabled={!page.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguientes →</button>
        </>}
        <button type="button" className="pu-link" disabled={exporting} onClick={() => void downloadCSV()}>{exporting ? 'Preparando CSV…' : 'Descargar recepciones (CSV)'}</button>
      </div>
      {exportError && <p className="pu-error" role="alert">{exportError}</p>}
    </>)}
  </section>;
}
