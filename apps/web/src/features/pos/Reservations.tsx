import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, ApiError, type CashRegisters, type Customer, type Reservation, type ReservationInput, type ReservationStatus, type StockLocation } from '../../shared/api/client';
import { InventoryVariantPicker, type Selected } from '../inventory/InventoryVariantPicker';
import { CustomerPicker } from './CustomerPicker';
import { ReservationDepositForm } from './ReservationDepositForm';
import { paymentLabels } from './SalePayments';

const pageSize = 20;
const statusLabels: Record<ReservationStatus, string> = {
  ACTIVE: 'Activas', FULFILLED: 'Entregadas', CANCELLED: 'Canceladas', EXPIRED: 'Vencidas',
};
type ReserveLine = Selected & { quantity: string };
type Attempt = { input: ReservationInput; key: string };

export function Reservations({ storeId, locations, registers, revision, disabled, onPending, onChanged, onCheckout }: {
  storeId: string; locations: StockLocation[]; registers: CashRegisters; revision: number; disabled: boolean;
  onPending: (value: boolean) => void; onChanged: () => void;
  onCheckout: (reservation: Reservation) => void;
}) {
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(Date.now);
  const [status, setStatus] = useState<ReservationStatus>('ACTIVE');
  const [offset, setOffset] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: { items: Reservation[]; hasMore: boolean }; error?: string } | null>(null);
  const [location, setLocation] = useState('');
  const [customer, setCustomer] = useState<Customer | null>(null);
  const [lines, setLines] = useState<ReserveLine[]>([]);
  const [expires, setExpires] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [depositTarget, setDepositTarget] = useState<string | null>(null);
  const [depositLocked, setDepositLocked] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const attempt = useRef<Attempt | null>(null);
  const locationID = location || locations[0]?.id || '';
  const key = [storeId, status, offset, revision].join(':');
  const current = result?.key === key ? result : null;
  const locked = disabled || busy || uncertain || depositLocked;
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: registers.timezone });

  useEffect(() => {
    if (!open) return;
    let active = true;
    void api.reservations(storeId, status, offset).then((page) => {
      if (active) setResult({ key, page });
    }).catch((cause: unknown) => {
      if (active) setResult({ key, error: errorMessage(cause) });
    });
    return () => { active = false; };
  }, [key, offset, open, status, storeId]);

  useEffect(() => {
    if (!open) return;
    const timer = window.setInterval(() => setNow(Date.now()), 15000);
    return () => window.clearInterval(timer);
  }, [open]);

  function add(item: Selected) {
    if (lines.length >= 100 && !lines.some((line) => line.id === item.id)) {
      setError('La reserva admite hasta 100 variantes.');
      return;
    }
    setLines((current) => current.some((line) => line.id === item.id)
      ? current.map((line) => line.id === item.id
        ? { ...line, quantity: String(Math.min(2147483647, (Number(line.quantity) || 0) + 1)) } : line)
      : [...current, { ...item, quantity: '1' }]);
  }

  async function create(operation: Attempt) {
    setBusy(true);
    setError('');
    setNotice('');
    onPending(true);
    try {
      await api.createReservation(operation.input, operation.key);
      attempt.current = null;
      setUncertain(false);
      setCustomer(null);
      setLines([]);
      setExpires('');
      setOffset(0);
      setStatus('ACTIVE');
      setNotice('Reserva registrada.');
      onChanged();
      onPending(false);
    } catch (cause) {
      const ambiguous = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      setUncertain(ambiguous);
      if (ambiguous) {
        setError('No se pudo confirmar el resultado. Reintentá la misma reserva para comprobar si se registró.');
      } else {
        attempt.current = null;
        setError(errorMessage(cause));
        onPending(false);
      }
    } finally {
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (locked || attempt.current) return;
    if (!customer || !locationID || !lines.length) {
      setError('Elegí un cliente y una ubicación, y agregá al menos una variante.');
      return;
    }
    const due = expires ? new Date(expires) : null;
    if (due && (!Number.isFinite(due.getTime()) || due.getTime() <= Date.now())) {
      setError('Elegí una fecha de vencimiento futura.');
      return;
    }
    const operation = { key: crypto.randomUUID(), input: {
      storeId, locationId: locationID, customerId: customer.id, expiresAt: due?.toISOString(),
      items: lines.map((line) => ({ variantId: line.id, quantity: Number(line.quantity) })),
    } };
    attempt.current = operation;
    void create(operation);
  }

  async function release(id: string, action: 'cancel' | 'expire') {
    if (locked) return;
    setBusy(true);
    setNotice('');
    setError('');
    onPending(true);
    try {
      await api.releaseReservation(id, storeId, action);
      setDepositTarget(null);
      setNotice(action === 'expire' ? 'Vencimiento registrado.' : 'Reserva cancelada.');
      setOffset(0);
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
      onPending(false);
    }
  }

  return <details className="catalog-classifications" onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary>Reservas</summary>
    {error && <p role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    <details className="catalog-classifications">
      <summary>Nueva reserva</summary>
      <CustomerPicker storeId={storeId} selection={customer} onSelect={setCustomer} disabled={locked} label="Cliente de la reserva" />
      {locations.length > 0 && <InventoryVariantPicker disabled={locked} onSelect={add} />}
      <form aria-label="Registrar reserva" className="inventory-form" onSubmit={submit}>
        <label>Ubicación de la reserva
          <select value={locationID} disabled={locked} onChange={(event) => setLocation(event.target.value)}>
            {locations.map((item) => <option key={item.id} value={item.id}>{item.name} ({item.code})</option>)}
          </select>
        </label>
        {locations.length === 0 && <p>Agregá una ubicación vendible para reservar mercadería.</p>}
        <label>Vencimiento opcional (hora del dispositivo)
          <input type="datetime-local" value={expires} disabled={locked} onChange={(event) => setExpires(event.target.value)} />
        </label>
        {lines.length > 0 && <ul className="catalog-items">{lines.map((line) => <li key={line.id}>
          <p>{line.product} · {line.sku}</p>
          <div className="actions">
            <label>Unidades a reservar de {line.sku}
              <input type="number" value={line.quantity} min={1} max={2147483647} step={1} required disabled={locked}
                onChange={(event) => setLines((items) => items.map((item) =>
                  item.id === line.id ? { ...item, quantity: event.target.value } : item))} />
            </label>
            <button type="button" disabled={locked} onClick={() => setLines((items) => items.filter((item) => item.id !== line.id))}>
              Quitar {line.sku}
            </button>
          </div>
        </li>)}</ul>}
        <button disabled={locked || !locations.length}>Registrar reserva</button>
      </form>
      {uncertain && <button type="button" disabled={busy || disabled} onClick={() => {
        if (attempt.current) void create(attempt.current);
      }}>Reintentar reserva</button>}
    </details>
    <label>Estado de la reserva
      <select value={status} disabled={locked} onChange={(event) => {
        setStatus(event.target.value as ReservationStatus);
        setDepositTarget(null);
        setOffset(0);
      }}>
        {Object.entries(statusLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    {open && !current && <p role="status">Consultando reservas…</p>}
    {current?.error && <p role="alert">{current.error}</p>}
    {current?.page?.items.length === 0 && <p>No hay reservas para mostrar.</p>}
    {current?.page && <ul className="catalog-items">{current.page.items.map((item) => {
      const expired = item.expiresAt !== null && new Date(item.expiresAt).getTime() <= now;
      return <li key={item.id}>
        <p><strong>{item.customerName}</strong> · {item.locationName}
          {item.expiresAt && ' · Vence ' + dates.format(new Date(item.expiresAt))}</p>
        <ul>{item.items.map((line) => <li key={line.variantId}>{line.quantity} × {line.product} · {line.sku}</li>)}</ul>
        {item.deposit && <p>Seña {registers.currency} {item.deposit.amount.replace('.', ',')} · {paymentLabels[item.deposit.method]}
          {(item.status === 'CANCELLED' || item.status === 'EXPIRED') && ' · Pendiente de resolver'}
          {item.status === 'FULFILLED' && ' · Aplicada a la venta'}</p>}
        <div className="actions">
          {item.status === 'ACTIVE' && <>
            {!item.deposit && <button type="button" disabled={locked || expired} onClick={() => setDepositTarget(item.id)}>Registrar seña</button>}
            <button type="button" disabled={locked || expired} onClick={() => { setDepositTarget(null); onCheckout(item); }}>Llevar al carrito</button>
            <button type="button" disabled={locked} onClick={() => void release(item.id, 'cancel')}>Cancelar reserva</button>
            {expired && <button type="button" disabled={locked} onClick={() => void release(item.id, 'expire')}>Registrar vencimiento</button>}
          </>}
        </div>
        {depositTarget === item.id && <ReservationDepositForm storeId={storeId} reservationId={item.id} registers={registers}
          disabled={disabled || busy || uncertain} onPending={(pending) => { setDepositLocked(pending); onPending(pending); }}
          onSaved={() => { setDepositTarget(null); setNotice('Seña registrada.'); onChanged(); }} onClose={() => setDepositTarget(null)} />}
      </li>;
    })}</ul>}
    {(offset > 0 || current?.page?.hasMore) && <nav className="actions" aria-label="Páginas de reservas">
      <button type="button" disabled={locked || offset === 0} onClick={() => setOffset(offset - pageSize)}>Anterior</button>
      <button type="button" disabled={locked || !current?.page?.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente</button>
    </nav>}
  </details>;
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No pudimos completar la operación de reserva.';
}
