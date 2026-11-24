import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, ApiError, type CashRegisters, type Receivable, type ReceivablePaymentInput, type ReceivablePaymentPage, type SalePaymentMethod } from '../../shared/api/client';
import { decimalInput, minorUnits } from '../../shared/lib/decimal';
import { paymentLabels, paymentMethods } from './SalePayments';

const pageSize = 25;
type Attempt = { input: ReceivablePaymentInput; key: string };

export function ReceivableCollections({ item, storeId, registers, revision, disabled, onPending, onCollected }: {
  item: Receivable; storeId: string; registers: CashRegisters; revision: number; disabled: boolean;
  onPending: (locked: boolean) => void; onCollected: () => void;
}) {
  const [offset, setOffset] = useState(0);
  const [refresh, setRefresh] = useState(0);
  const [history, setHistory] = useState<{ key: string; page?: ReceivablePaymentPage; error?: string } | null>(null);
  const [amount, setAmount] = useState('');
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const attempt = useRef<Attempt | null>(null);
  const key = [item.id, offset, revision, refresh].join(':');
  const current = history?.key === key ? history : null;
  const sessions = registers.items.filter((register) => register.openSessionId);
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: registers.timezone });
  const display = (value: string) => item.currency + ' ' + value.replace('.', ',');

  useEffect(() => {
    let active = true;
    void api.receivablePayments(storeId, item.id, offset).then((page) => {
      if (active) setHistory({ key, page });
    }).catch((cause: unknown) => {
      if (active) setHistory({ key, error: cause instanceof Error ? cause.message : 'No pudimos cargar los abonos.' });
    });
    return () => { active = false; };
  }, [item.id, key, offset, storeId]);

  async function record(operation: Attempt) {
    setBusy(true);
    setError('');
    setNotice('');
    onPending(true);
    try {
      await api.addReceivablePayment(item.id, operation.input, operation.key);
      attempt.current = null;
      setUncertain(false);
      setAmount('');
      setNotice('Abono registrado.');
      setOffset(0);
      onCollected();
      onPending(false);
    } catch (cause) {
      const ambiguous = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      setUncertain(ambiguous);
      if (ambiguous) {
        setError('El resultado del abono es incierto. Reintentá el mismo abono para comprobar si se registró.');
      } else {
        attempt.current = null;
        setError(cause.message);
        setRefresh((value) => value + 1);
        onPending(false);
      }
    } finally {
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (disabled || attempt.current || !current?.page) return;
    try {
      const values = new FormData(event.currentTarget);
      const received = decimalInput(amount);
      if (minorUnits(received) === 0n) throw new Error('Ingresá un importe mayor a cero.');
      if (minorUnits(received) > minorUnits(current.page.outstandingAmount)) throw new Error('El abono supera el saldo pendiente.');
      const operation: Attempt = {
        key: crypto.randomUUID(),
        input: { storeId, cashSessionId: String(values.get('session')), method: values.get('method') as SalePaymentMethod, amount: received },
      };
      attempt.current = operation;
      void record(operation);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Verificá el importe.');
    }
  }

  return <section className="inventory-form" aria-label={'Abonos de la venta ' + item.saleNumber}>
    <h3>Abonos de la venta {item.saleNumber}</h3>
    <p>{item.customerName} · Saldo inicial {display(item.originalAmount)}</p>
    {!current && <p role="status">Consultando abonos…</p>}
    {current?.error && <p role="alert">{current.error}</p>}
    {error && <p role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {current?.page && <>
      <p>Saldo pendiente <strong>{display(current.page.outstandingAmount)}</strong></p>
      {current.page.outstandingAmount !== '0.00' && (sessions.length === 0
        ? <p>Abrí una caja en esta sucursal para registrar un abono.</p>
        : <form className="inventory-form" aria-label="Registrar abono" onSubmit={submit}>
          <label>Caja del abono
            <select name="session" disabled={disabled || busy || uncertain} required>
              {sessions.map((session) => <option key={session.id} value={session.openSessionId ?? ''}>{session.name} ({session.code})</option>)}
            </select>
          </label>
          <label>Medio del abono
            <select name="method" disabled={disabled || busy || uncertain}>
              {paymentMethods.map((method) => <option key={method} value={method}>{paymentLabels[method]}</option>)}
            </select>
          </label>
          <label>Importe del abono
            <input inputMode="decimal" required maxLength={19} value={amount} disabled={disabled || busy || uncertain}
              onChange={(event) => setAmount(event.target.value)} />
          </label>
          <button disabled={disabled || busy || uncertain}>Registrar abono</button>
        </form>)}
      {current.page.items.length === 0 ? <p>Todavía no hay abonos.</p> : <ul className="catalog-items">
        {current.page.items.map((payment) => <li key={payment.id}>
          {dates.format(new Date(payment.createdAt))} · {paymentLabels[payment.method]} · {display(payment.amount)}
          {' · '}Saldo después del abono {display(payment.balanceAfter)}
        </li>)}
      </ul>}
      {current.page.debtCredits.length > 0 && <>
        <h4>Compensaciones por devoluciones</h4>
        <ul className="catalog-items">{current.page.debtCredits.map((credit) => <li key={credit.id}>
          {dates.format(new Date(credit.createdAt))} · {display(credit.amount)} · Saldo después del ajuste {display(credit.balanceAfter)}
        </li>)}</ul>
      </>}
    </>}
    {uncertain && <button type="button" disabled={busy || disabled} onClick={() => {
      if (attempt.current) void record(attempt.current);
    }}>Reintentar abono</button>}
    {(offset > 0 || current?.page?.hasMore) && <nav className="actions" aria-label="Páginas de movimientos del fiado">
      <button type="button" disabled={!current || offset === 0 || busy || uncertain} onClick={() => setOffset(offset - pageSize)}>Anterior</button>
      <button type="button" disabled={!current?.page?.hasMore || busy || uncertain} onClick={() => setOffset(offset + pageSize)}>Siguiente</button>
    </nav>}
  </section>;
}
