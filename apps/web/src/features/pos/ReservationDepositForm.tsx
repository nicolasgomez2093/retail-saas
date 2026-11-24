import { useRef, useState, type FormEvent } from 'react';
import { api, ApiError, type CashRegisters, type ReservationDepositInput, type SalePaymentMethod } from '../../shared/api/client';
import { decimalInput, minorUnits } from '../../shared/lib/decimal';
import { paymentLabels, paymentMethods } from './SalePayments';

type Attempt = { input: ReservationDepositInput; key: string };

export function ReservationDepositForm({ storeId, reservationId, registers, disabled, onPending, onSaved, onClose }: {
  storeId: string; reservationId: string; registers: CashRegisters; disabled: boolean;
  onPending: (pending: boolean) => void; onSaved: () => void; onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState('');
  const attempt = useRef<Attempt | null>(null);
  const sessions = registers.items.filter((register) => register.openSessionId);
  const locked = disabled || busy || uncertain;

  async function record(operation: Attempt) {
    setBusy(true);
    setError('');
    onPending(true);
    try {
      await api.addReservationDeposit(reservationId, operation.input, operation.key);
      attempt.current = null;
      onPending(false);
      onSaved();
    } catch (cause) {
      const ambiguous = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      setUncertain(ambiguous);
      if (ambiguous) {
        setError('El resultado de la seña es incierto. Reintentá la misma operación para comprobar si se registró.');
      } else {
        attempt.current = null;
        setError(cause.message);
        onPending(false);
      }
    } finally {
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (locked || attempt.current) return;
    const values = new FormData(event.currentTarget);
    try {
      const amount = decimalInput(values.get('amount'));
      if (minorUnits(amount) === 0n) throw new Error('Ingresá un importe mayor a cero.');
      const operation: Attempt = {
        key: crypto.randomUUID(),
        input: { storeId, cashSessionId: String(values.get('session')), method: values.get('method') as SalePaymentMethod, amount },
      };
      attempt.current = operation;
      void record(operation);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Verificá el importe.');
    }
  }

  return <section className="inventory-form" aria-label="Seña de la reserva">
    {error && <p role="alert">{error}</p>}
    {sessions.length === 0 ? <p>Abrí una caja para registrar la seña.</p> :
      <form aria-label="Registrar seña" className="inventory-form" onSubmit={submit}>
        <label>Caja de la seña
          <select name="session" required disabled={locked}>
            {sessions.map((register) => <option key={register.id} value={register.openSessionId ?? ''}>{register.name} ({register.code})</option>)}
          </select>
        </label>
        <label>Medio de la seña
          <select name="method" disabled={locked}>
            {paymentMethods.map((method) => <option key={method} value={method}>{paymentLabels[method]}</option>)}
          </select>
        </label>
        <label>Importe de la seña<input name="amount" inputMode="decimal" maxLength={19} required disabled={locked} /></label>
        <button disabled={locked}>Confirmar seña</button>
      </form>}
    <button type="button" disabled={busy || uncertain} onClick={onClose}>Cerrar registro de seña</button>
    {uncertain && <button type="button" disabled={disabled || busy} onClick={() => {
      if (attempt.current) void record(attempt.current);
    }}>Reintentar seña</button>}
  </section>;
}
