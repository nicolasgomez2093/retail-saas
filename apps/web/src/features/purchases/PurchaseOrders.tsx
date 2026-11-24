import { useEffect, useId, useMemo, useRef, useState } from 'react';

import { api, ApiError, type PurchaseOrder, type PurchaseOrderFilters, type PurchaseOrderPage, type PurchaseOrderStatusFilter, type PurchaseReceipt, type Store } from '../../shared/api/client';
import { NewPurchaseOrder } from './NewPurchaseOrder';
import { PurchaseReceiptForm, ReceiptResult } from './PurchaseReceiptForm';
import { PurchaseReceiptHistory } from './PurchaseReceiptHistory';
import { Modal, ReceiptLabels } from './PurchaseParts';
import { itemLabel, lineCents, money, moneyText, orderCode, orderedUnits, pendingUnits, receivedUnits, statusLabels, statusTone, sumCents } from './purchaseFormat';
import './purchases.css';

const pageSize = 25;
const emptyFilters: PurchaseOrderFilters = { status: '', query: '', fromDate: '', toDate: '' };

type Screen = { kind: 'list' } | { kind: 'new' } | { kind: 'receive'; order: PurchaseOrder } | { kind: 'received'; order: PurchaseOrder; receipt: PurchaseReceipt };

export function PurchaseOrders({ stores, pendingOnly, supplierId, supplierName, onReceived, onCreateProduct }: {
  stores: Store[]; pendingOnly: boolean; supplierId: string; supplierName: string; onReceived: () => void; onCreateProduct?: () => void;
}) {
  const store = stores[0];
  if (!store) return null;
  return <PurchaseOrdersForStore key={store.id} store={store} pendingOnly={pendingOnly} supplierId={supplierId} supplierName={supplierName}
    onReceived={onReceived} onCreateProduct={onCreateProduct} />;
}

function PurchaseOrdersForStore({ store, pendingOnly, supplierId, supplierName, onReceived, onCreateProduct }: {
  store: Store; pendingOnly: boolean; supplierId: string; supplierName: string; onReceived: () => void; onCreateProduct?: () => void;
}) {
  const uid = useId();
  const [screen, setScreen] = useState<Screen>({ kind: 'list' });
  const [search, setSearch] = useState('');
  const [filters, setFilters] = useState<PurchaseOrderFilters>(() => ({ ...emptyFilters, status: pendingOnly ? 'pending' : '' }));
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: PurchaseOrderPage; error?: string; denied?: boolean } | null>(null);
  const [overall, setOverall] = useState<PurchaseOrderPage['summary'] | null>(null);
  const [detail, setDetail] = useState<{ order: PurchaseOrder; opener: HTMLElement | null } | null>(null);
  const [toast, setToast] = useState('');
  const [exporting, setExporting] = useState(false);
  const listHeading = useRef<HTMLHeadingElement>(null);
  const applied = useMemo(() => ({ ...filters, supplierId }), [filters, supplierId]);
  const key = JSON.stringify([offset, revision, applied]);
  const current = result?.key === key ? result : null;
  const page = current?.page;
  const filtered = Boolean(applied.status || applied.query || applied.fromDate || applied.toDate || applied.supplierId);

  useEffect(() => { setFilters((value) => ({ ...value, status: pendingOnly ? 'pending' : value.status === 'pending' ? '' : value.status })); setOffset(0); }, [pendingOnly]);
  useEffect(() => {
    const handle = window.setTimeout(() => {
      setFilters((value) => value.query === search.trim() ? value : { ...value, query: search.trim() });
      setOffset(0);
    }, 300);
    return () => window.clearTimeout(handle);
  }, [search]);

  useEffect(() => {
    let active = true;
    void api.purchaseOrders(store.id, offset, applied).then((loaded) => {
      if (active) setResult({ key, page: loaded });
    }).catch((cause: unknown) => {
      if (active) setResult(cause instanceof ApiError && cause.status === 403 ? { key, denied: true } : { key, error: cause instanceof Error ? cause.message : 'No pudimos cargar los pedidos.' });
    });
    return () => { active = false; };
  }, [key, offset, store.id, applied]);

  // The metrics describe the whole branch, whatever filter the list uses.
  useEffect(() => {
    let active = true;
    void api.purchaseOrders(store.id, 0, emptyFilters).then((loaded) => { if (active) setOverall(loaded.summary); }).catch(() => undefined);
    return () => { active = false; };
  }, [store.id, revision]);

  useEffect(() => {
    if (!toast) return;
    const handle = window.setTimeout(() => setToast(''), 3500);
    return () => window.clearTimeout(handle);
  }, [toast]);

  function refresh() {
    setRevision((value) => value + 1);
  }
  function backToList(message = '') {
    setScreen({ kind: 'list' });
    if (message) setToast(message);
    refresh();
    requestAnimationFrame(() => listHeading.current?.focus());
  }

  async function downloadCSV() {
    setExporting(true);
    try {
      const file = await api.downloadPurchaseOrdersCSV(store.id, applied);
      const url = URL.createObjectURL(file);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'pedidos-de-compra.csv';
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (cause) {
      setToast(cause instanceof Error ? cause.message : 'No pudimos descargar los pedidos.');
    } finally {
      setExporting(false);
    }
  }

  if (current?.denied) return <p>No tenés acceso a los pedidos de esta sucursal.</p>;
  if (screen.kind === 'new') return <NewPurchaseOrder store={store} currency={page?.currency ?? 'ARS'} onClose={() => backToList()}
    onCreateProduct={onCreateProduct}
    onSaved={(order) => { backToList('Pedido ' + orderCode(order.number) + ' registrado. El stock no cambió.'); setDetail({ order, opener: null }); }} />;
  if (screen.kind === 'receive') return <PurchaseReceiptForm order={screen.order} onClose={() => backToList()}
    onSaved={(receipt) => { setScreen({ kind: 'received', order: screen.order, receipt }); onReceived(); refresh(); }} />;
  if (screen.kind === 'received') return <ReceiptResult order={screen.order} receipt={screen.receipt}
    onFinish={() => backToList()} onOpenOrder={() => {
      const id = screen.order.id;
      backToList();
      void api.purchaseOrder(store.id, id).then((order) => setDetail({ order, opener: null })).catch(() => undefined);
    }} />;

  const dates = page && new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeZone: page.timezone });
  const canManage = page?.canManage ?? false;
  return <section className="pu" aria-labelledby={uid + 'title'}>
    <header className="pu-head">
      <div><h2 id={uid + 'title'} ref={listHeading} tabIndex={-1}>Compras y proveedores</h2><p>Armá pedidos y registrá recepciones parciales.</p></div>
      <div className="pu-head-actions">
        <a className="pu-btn" href="#/compras/proveedores">♧ Proveedores</a>
        {canManage && <button type="button" className="pu-btn primary" onClick={() => setScreen({ kind: 'new' })}>＋ Nuevo pedido</button>}
      </div>
    </header>
    <div className="pu-metrics">
      <div className="pu-metric"><small>Pedidos</small><strong>{overall?.orderCount ?? '—'}</strong><span>{overall ? overall.orderedUnits + ' u. pedidas' : 'Consultando…'}</span></div>
      <div className="pu-metric"><small>Pendientes</small><strong>{overall?.pendingOrders ?? '—'}</strong><span>{overall ? overall.pendingUnits + ' u. por recibir' : ''}</span></div>
      <div className="pu-metric"><small>Recibidos</small><strong>{overall?.receivedOrders ?? '—'}</strong><span>{overall ? overall.receivedUnits + ' u. ingresadas' : ''}</span></div>
    </div>
    <div className="pu-card">
      <div className="pu-panelhead">
        <div><h3>Órdenes de compra</h3><p>Proveedores y estado de recepción</p></div>
        {page && page.items.length > 0 && <button type="button" className="pu-link" disabled={exporting} onClick={() => void downloadCSV()}>{exporting ? 'Preparando CSV…' : 'Descargar CSV'}</button>}
      </div>
      <div className="pu-filters">
        <label className="pu-search"><span aria-hidden="true">⌕</span>
          <input type="search" value={search} maxLength={120} aria-label="Buscar pedidos" placeholder="Buscar por proveedor, OC, comprobante, producto o SKU…"
            onChange={(event) => setSearch(event.target.value)} /></label>
        <select aria-label="Estado del pedido" value={filters.status} onChange={(event) => { setFilters({ ...filters, status: event.target.value as PurchaseOrderStatusFilter }); setOffset(0); }}>
          <option value="">Todos los estados</option>
          <option value="pending">Por recibir</option>
          {(Object.keys(statusLabels) as (keyof typeof statusLabels)[]).map((status) => <option key={status} value={status}>{statusLabels[status]}</option>)}
        </select>
        <input type="date" aria-label="Desde" max={filters.toDate || '9999-12-31'} value={filters.fromDate} onChange={(event) => { setFilters({ ...filters, fromDate: event.target.value }); setOffset(0); }} />
        <input type="date" aria-label="Hasta" min={filters.fromDate || undefined} max="9999-12-31" value={filters.toDate} onChange={(event) => { setFilters({ ...filters, toDate: event.target.value }); setOffset(0); }} />
      </div>
      {supplierId && <p className="pu-status" style={{ padding: '12px 20px', borderBottom: '1px solid var(--p-line)' }}>
        Pedidos de <b>{supplierName || page?.items[0]?.supplierName || 'este proveedor'}</b> · <a href="#/compras/ordenes">Ver todos</a></p>}
      {!current && <p className="pu-status" role="status">Consultando pedidos…</p>}
      {current?.error && <div className="pu-panelbody"><p className="pu-alert" role="alert">{current.error}</p>
        <button type="button" className="pu-btn" onClick={refresh}>Reintentar</button></div>}
      {page && (page.items.length === 0 ? <div className="pu-empty">
        <h3>{filtered ? 'No hay pedidos con esos filtros' : 'Todavía no hay pedidos'}</h3>
        <p>{filtered ? 'Probá con otro estado, fecha o búsqueda.' : 'Registrá lo que le pediste a un proveedor y recibilo cuando llegue.'}</p>
        {!filtered && canManage && <button type="button" className="pu-btn primary" onClick={() => setScreen({ kind: 'new' })}>＋ Nuevo pedido</button>}
      </div> : <ul className="pu-rows">{page.items.map((order) => {
        const pending = pendingUnits(order);
        return <li key={order.id} className="pu-row">
          <div><b>{orderCode(order.number)} · {order.supplierName}</b>
            <small>{dates?.format(new Date(order.orderedAt))} · {order.items.length} {order.items.length === 1 ? 'renglón' : 'renglones'}{order.reference ? ' · ' + order.reference : ''}</small></div>
          <span className={'pu-pill ' + statusTone[order.status]}>{statusLabels[order.status]}</span>
          <span>{orderedUnits(order)} u. pedidas{pending > 0 && <small>{pending} por recibir</small>}{order.status === 'CANCELLED' && receivedUnits(order) > 0 && <small>{receivedUnits(order)} recibidas</small>}</span>
          <button type="button" className="pu-btn" onClick={(event) => setDetail({ order, opener: event.currentTarget })} aria-label={'Ver pedido ' + orderCode(order.number)}>Ver pedido</button>
        </li>;
      })}</ul>)}
      {page && (offset > 0 || page.hasMore) && <nav className="pu-pager" aria-label="Páginas de pedidos">
        <button type="button" className="pu-btn" disabled={!offset} onClick={() => setOffset(offset - pageSize)}>← Anterior</button>
        <span>Página {offset / pageSize + 1}</span>
        <button type="button" className="pu-btn" disabled={!page.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente →</button>
      </nav>}
    </div>
    <div className="pu-note">Registrar un pedido no modifica el stock. Las unidades ingresan al confirmar la recepción, con un movimiento de inventario por cada variante.</div>
    {detail && <OrderDialog key={detail.order.id} order={detail.order} timezone={page?.timezone ?? 'America/Argentina/Buenos_Aires'} canManage={canManage}
      returnTo={detail.opener} revision={revision} onClose={() => setDetail(null)}
      onReceive={(order) => { setDetail(null); setScreen({ kind: 'receive', order }); }}
      onChanged={(order, message) => { setDetail({ order, opener: detail.opener }); setToast(message); refresh(); }} />}
    {toast && <p className="pu-toast" role="status">{toast}</p>}
  </section>;
}

/** «Ver pedido»: the .modal of the approved HTML with lines, receipts and actions. */
function OrderDialog({ order: initial, timezone, canManage, returnTo, revision, onClose, onReceive, onChanged }: {
  order: PurchaseOrder; timezone: string; canManage: boolean; returnTo: HTMLElement | null; revision: number;
  onClose: () => void; onReceive: (order: PurchaseOrder) => void; onChanged: (order: PurchaseOrder, message: string) => void;
}) {
  const [order, setOrder] = useState(initial);
  const [cancelling, setCancelling] = useState(false);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [labels, setLabels] = useState<{ lines: { productId: string; variantId: string; count: number }[]; button: HTMLButtonElement } | null>(null);
  const reasonInput = useRef<HTMLTextAreaElement>(null);

  // Always show the current state: another person may have received goods.
  useEffect(() => {
    let active = true;
    void api.purchaseOrder(initial.storeId, initial.id).then((fresh) => { if (active) setOrder(fresh); }).catch(() => undefined);
    return () => { active = false; };
  }, [initial.id, initial.storeId]);
  useEffect(() => { if (cancelling) reasonInput.current?.focus(); }, [cancelling]);

  async function cancel() {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      const cancelled = await api.cancelPurchaseOrder(order.id, order.storeId, reason.trim());
      setOrder(cancelled);
      setCancelling(false);
      onChanged(cancelled, 'Pedido ' + orderCode(order.number) + ' cancelado.');
    } catch (cause) {
      setError(cause instanceof ApiError && cause.code === 'purchase_order_closed' ? 'El pedido ya está recibido completo y no se puede cancelar.'
        : cause instanceof Error ? cause.message : 'No pudimos cancelar el pedido.');
    } finally {
      setBusy(false);
    }
  }

  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });
  const open = order.status === 'OPEN' || order.status === 'PARTIALLY_RECEIVED';
  const total = sumCents(order.items.map((item) => lineCents(item.quantity, item.unitCost)));
  const missingCost = order.items.filter((item) => item.unitCost === null).length;
  return <>
    <Modal wide title={'Pedido ' + orderCode(order.number)} returnTo={returnTo} onClose={onClose} busy={busy}
      footer={cancelling ? <>
        <button type="button" className="pu-btn" disabled={busy} onClick={() => setCancelling(false)}>Volver</button>
        <button type="button" className="pu-btn primary" disabled={busy} onClick={() => void cancel()}>{busy ? 'Cancelando…' : 'Confirmar cancelación'}</button>
      </> : <>
        {canManage && open && <button type="button" className="pu-link danger spacer" onClick={() => setCancelling(true)}>Cancelar pedido</button>}
        <button type="button" className={'pu-btn' + (canManage && open ? '' : ' primary')} onClick={onClose}>Cerrar</button>
        {canManage && open && <button type="button" className="pu-btn primary" onClick={() => onReceive(order)}>Recibir mercadería</button>}
      </>}>
      <p>Proveedor: <b>{order.supplierName}</b> · <span className={'pu-pill ' + statusTone[order.status]}>{statusLabels[order.status]}</span></p>
      <p className="pu-hint">{dates.format(new Date(order.orderedAt))}{order.reference ? ' · Comprobante ' + order.reference : ''}</p>
      {error && <p className="pu-alert" role="alert">{error}</p>}
      {order.status === 'CANCELLED' && <div className="pu-note warn"><p>Cancelado el {order.cancelledAt ? dates.format(new Date(order.cancelledAt)) : ''}
        {order.cancelReason ? ' · ' + order.cancelReason : ''}. Lo ya recibido quedó en stock; lo pendiente no se espera más.</p></div>}
      {cancelling ? <div>
        <label className="pu-label">Motivo de la cancelación
          <textarea ref={reasonInput} value={reason} maxLength={300} disabled={busy} onChange={(event) => setReason(event.target.value)}
            placeholder="Opcional. Ej. el proveedor no tiene más unidades" /></label>
        <div className="pu-note warn"><p>Las {pendingUnits(order)} unidades pendientes dejan de esperarse. {receivedUnits(order) > 0 ? 'Las ' + receivedUnits(order) + ' ya recibidas quedan en stock y en el historial.' : 'No se recibió nada todavía.'} Esta acción no se deshace.</p></div>
      </div> : <>
        <div>{order.items.map((item) => {
          const percent = item.quantity ? Math.min(100, Math.round(item.received / item.quantity * 100)) : 0;
          return <div key={item.id} className="pu-statrow">
            <div style={{ minWidth: 0, flex: 1 }}><b>{item.product}</b>
              <span className="sub">{itemLabel(item)} · {item.sku} · pedido {item.quantity} u. · recibido {item.received} u.
                {item.rejected ? ' · ' + item.rejected + ' no aceptadas' : ''}</span>
              <div className="pu-progress" aria-hidden="true"><span style={{ width: percent + '%' }} /></div></div>
            <div style={{ textAlign: 'right' }}>
              {item.received >= item.quantity ? <span className="pu-pill">Completo</span>
                : order.status === 'CANCELLED' ? <span className="pu-pill muted">{item.quantity - item.received} sin recibir</span>
                  : <span className="pu-pill warning">{item.quantity - item.received} pendientes</span>}
              <span className="sub">{moneyText(order.currency, item.unitCost)}{item.unitCost ? ' c/u' : ''}</span>
            </div>
          </div>;
        })}</div>
        <div className="pu-statrow"><b>Total</b><b>{orderedUnits(order)} u. · {money(order.currency, total)}{missingCost ? ' (parcial)' : ''}</b></div>
        <PurchaseReceiptHistory order={order} revision={revision} timezone={timezone}
          onLabels={(lines, button) => setLabels({ lines, button })} />
      </>}
    </Modal>
    {labels && <ReceiptLabels storeId={order.storeId} lines={labels.lines} returnTo={labels.button} onClose={() => setLabels(null)} />}
  </>;
}
