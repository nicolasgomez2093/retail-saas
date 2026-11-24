import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, type StockLocation, type Transfer, type TransferPage, type TransferStatus } from '../../shared/api/client';
import { InventoryVariantPicker, type Selected } from './InventoryVariantPicker';

const statusLabels: Record<TransferStatus, string> = {
  DRAFT: 'Borrador',
  DISPATCHED: 'En tránsito',
  RECEIVED: 'Recibida',
  CANCELLED: 'Cancelada',
};

type DraftLine = Selected & { units: string };

export function Transfers({ storeId, origins, revision, onStockChanged }: {
  storeId: string; origins: StockLocation[]; revision: number; onStockChanged: () => void;
}) {
  const [page, setPage] = useState<TransferPage | null>(null);
  const [detail, setDetail] = useState<Transfer | null>(null);
  const [creating, setCreating] = useState(false);
  const [receiving, setReceiving] = useState(false);
  const [originId, setOriginId] = useState('');
  const [lines, setLines] = useState<DraftLine[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const retry = useRef<{ fingerprint: string; key: string } | null>(null);

  useEffect(() => {
    let active = true;
    void api.transfers(storeId).then((result) => {
      if (active) setPage(result);
    }).catch((cause: unknown) => {
      if (active) setError(message(cause));
    });
    return () => { active = false; };
  }, [storeId, origins, revision]);

  const detailID = detail?.id;
  const detailStatus = detail?.status;
  const listedStatus = page?.items.find((item) => item.id === detailID)?.status;

  useEffect(() => {
    if (!detailID || !listedStatus || detailStatus === listedStatus) return;
    let active = true;
    void api.transfer(storeId, detailID).then((updated) => {
      if (!active) return;
      setDetail((current) => current?.id === detailID && current.status === detailStatus ? updated : current);
      setNotice('');
    }).catch((cause: unknown) => {
      if (active) setError(message(cause));
    });
    return () => { active = false; };
  }, [storeId, detailID, detailStatus, listedStatus]);

  function message(cause: unknown) {
    return cause instanceof Error ? cause.message : 'No pudimos actualizar la transferencia.';
  }

  async function perform(operation: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await operation();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  function show(result: Transfer) {
    setDetail(result);
    setPage((current) => current && {
      ...current,
      items: [result, ...current.items.filter((item) => item.id !== result.id)]
        .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt) || b.id.localeCompare(a.id)).slice(0, 50),
    });
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    if (!lines.length) {
      setError('Agregá al menos una variante a la transferencia.');
      return;
    }
    const items = lines.map((line) => ({ variantId: line.id, quantity: Number(line.units) }));
    if (items.some((line) => !Number.isInteger(line.quantity) || line.quantity < 1 || line.quantity > 2147483647)) {
      setError('Ingresá cantidades enteras de al menos una unidad.');
      return;
    }
    const data = { storeId, originLocationId: originId, destinationLocationId: String(form.get('destination')), items };
    const fingerprint = JSON.stringify(data);
    if (retry.current?.fingerprint !== fingerprint) retry.current = { fingerprint, key: crypto.randomUUID() };
    const key = retry.current.key;
    void perform(async () => {
      show(await api.createTransfer(data, key));
      retry.current = null;
      setCreating(false);
      setLines([]);
      setNotice('Borrador creado.');
    });
  }

  function action(kind: 'dispatch' | 'cancel') {
    if (!detail) return;
    void perform(async () => {
      show(await api.transferAction(detail.id, kind, storeId));
      if (kind === 'dispatch') onStockChanged();
      setNotice(kind === 'dispatch' ? 'Transferencia despachada.' : 'Transferencia cancelada.');
    });
  }

  function receive(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail) return;
    const form = new FormData(event.currentTarget);
    const items = detail.items.map((line) => ({ variantId: line.variantId, quantity: Number(form.get(line.variantId)) }));
    const reason = String(form.get('reason')).trim();
    if (items.some((line, index) => !Number.isInteger(line.quantity) || line.quantity < 0 || line.quantity > detail.items[index].quantity)) {
      setError('Verificá las cantidades recibidas.');
      return;
    }
    if (!reason && items.some((line, index) => line.quantity !== detail.items[index].quantity)) {
      setError('Indicá el motivo de la diferencia.');
      return;
    }
    void perform(async () => {
      show(await api.receiveTransfer(detail.id, storeId, items, reason));
      setReceiving(false);
      onStockChanged();
      setNotice('Recepción confirmada.');
    });
  }

  return (
    <section aria-label="Transferencias de stock" aria-busy={busy}>
      <h3>Transferencias</h3>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {page?.canTransfer && origins.length > 0 && page.destinations.length > 1 && !creating && (
        <button type="button" disabled={busy} onClick={() => {
          setCreating(true);
          setReceiving(false);
          setOriginId(origins[0].id);
          setError('');
          setNotice('');
        }}>Nueva transferencia</button>
      )}
      {creating && page && (
        <section aria-label="Preparar transferencia">
          <h4>Nuevo borrador</h4>
          <InventoryVariantPicker disabled={busy} onSelect={(variant) => setLines((current) =>
            current.length >= 100 || current.some((line) => line.id === variant.id) ? current : [...current, { ...variant, units: '1' }])} />
          <form className="inventory-form" aria-label="Nueva transferencia" onSubmit={create}>
            <label>Origen
              <select name="origin" value={originId} onChange={(event) => setOriginId(event.target.value)} disabled={busy} required>
                {origins.map((location) => <option key={location.id} value={location.id}>{location.name} ({location.code})</option>)}
              </select>
            </label>
            <label>Destino
              <select key={originId} name="destination" disabled={busy} defaultValue="" required>
                <option value="">Elegí un destino</option>
                {page.destinations.filter((location) => location.id !== originId).map((location) => (
                  <option key={location.id} value={location.id}>{location.storeName} · {location.name} ({location.code})</option>
                ))}
              </select>
            </label>
            {lines.map((line) => <div className="actions" key={line.id}>
              <label>Unidades de {line.sku}
                <input type="number" min={1} max={2147483647} step={1} value={line.units} required disabled={busy}
                  onChange={(event) => setLines((current) => current.map((item) => item.id === line.id ? { ...item, units: event.target.value } : item))} />
              </label>
              <button type="button" className="catalog-link" disabled={busy} onClick={() =>
                setLines((current) => current.filter((item) => item.id !== line.id))}>Quitar {line.sku}</button>
            </div>)}
            {lines.length === 100 && <p>El borrador ya tiene 100 variantes.</p>}
            <div className="actions">
              <button disabled={busy}>Guardar borrador</button>
              <button type="button" disabled={busy} onClick={() => { setCreating(false); setLines([]); }}>Cancelar formulario</button>
            </div>
          </form>
        </section>
      )}
      {page && page.items.length === 0 && <p>Todavía no hay transferencias para esta sucursal.</p>}
      {page && page.items.length > 0 && <ul className="catalog-items">{page.items.map((item) => (
        <li key={item.id}>
          <button type="button" className="catalog-link" disabled={busy} onClick={() => void perform(async () => {
            setDetail(await api.transfer(storeId, item.id));
            setReceiving(false);
            setCreating(false);
          })}>
            {item.origin.storeName} · {item.origin.name} → {item.destination.storeName} · {item.destination.name} ({statusLabels[item.status]})
          </button>
        </li>
      ))}</ul>}
      {detail && !creating && (
        <section aria-label="Detalle de transferencia">
          <h4>{statusLabels[detail.status]}</h4>
          <p>{detail.origin.storeName} · {detail.origin.name} → {detail.destination.storeName} · {detail.destination.name}</p>
          <ul className="catalog-items">
            {detail.items.map((line) => (
              <li key={line.variantId}>
                {line.sku} · Solicitadas {line.quantity}
                {line.received !== null && ` · Recibidas ${line.received} · Faltantes ${line.quantity - line.received}`}
              </li>
            ))}
          </ul>
          {detail.receiptReason && <p>Motivo: {detail.receiptReason}</p>}
          {page?.canTransfer && detail.status === 'DRAFT' && detail.origin.storeId === storeId && (
            <div className="actions">
              <button type="button" disabled={busy} onClick={() => action('dispatch')}>Despachar</button>
              <button type="button" disabled={busy} onClick={() => action('cancel')}>Cancelar borrador</button>
            </div>
          )}
          {page?.canTransfer && detail.status === 'DISPATCHED' && detail.destination.storeId === storeId && (
            receiving ? <form className="inventory-form" aria-label="Recibir transferencia" onSubmit={receive}>
              {detail.items.map((line) => <label key={line.variantId}>Unidades recibidas de {line.sku}
                <input name={line.variantId} type="number" min={0} max={line.quantity} step={1}
                  defaultValue={line.quantity} required disabled={busy} />
              </label>)}
              <label>Motivo de la diferencia<input name="reason" maxLength={300} disabled={busy} /></label>
              <div className="actions">
                <button disabled={busy}>Confirmar recepción</button>
                <button type="button" disabled={busy} onClick={() => setReceiving(false)}>Volver</button>
              </div>
            </form> : <button type="button" disabled={busy} onClick={() => setReceiving(true)}>Registrar recepción</button>
          )}
        </section>
      )}
    </section>
  );
}
