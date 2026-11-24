import { useEffect, useId, useRef, useState } from 'react';

import { api, ApiError, type PurchaseOrder, type SupplierPayment, type SupplierPaymentMethod } from '../../shared/api/client';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { formatAmount } from '../catalog/quickProduct';
import { newKey, parseCost } from './purchaseFormat';

export const paymentMethodLabels: Record<SupplierPaymentMethod, string> = {
  cash: 'Efectivo', transfer: 'Transferencia', check: 'Cheque', card: 'Tarjeta', other: 'Otro',
};
const referenceHints: Record<SupplierPaymentMethod, string> = {
  cash: 'Ej. recibo 0001-2345', transfer: 'Ej. N° de operación', check: 'Ej. Cheque 004512 · Banco Nación', card: 'Ej. últimos 4 dígitos', other: 'Ej. nota de crédito',
};

const amountText = (currency: string, value: string) => currency + ' ' + formatAmount(value.replace(/^-/, ''));
export const dayText = (day: string) => day.split('-').reverse().join('/');
const todayIn = (timezone: string) => new Intl.DateTimeFormat('en-CA', { timeZone: timezone }).format(new Date());

/** Short payment state for lists: what is still owed, or that it is settled. */
export function paymentSummary(order: Pick<PurchaseOrder, 'currency' | 'payable' | 'paid' | 'balance' | 'paymentStatus'>) {
  switch (order.paymentStatus) {
    case 'unknown': return { tone: 'muted', label: 'Sin total', detail: Number(order.paid) > 0 ? 'Pagado ' + amountText(order.currency, order.paid) : 'Faltan costos' };
    case 'none': return { tone: 'muted', label: 'Nada que pagar', detail: '' };
    case 'unpaid': return { tone: 'warning', label: 'Sin pagos', detail: 'Saldo ' + amountText(order.currency, order.balance ?? '0') };
    case 'partial': return { tone: 'warning', label: 'Pago parcial', detail: 'Saldo ' + amountText(order.currency, order.balance ?? '0') };
    case 'paid': return { tone: '', label: 'Pagado', detail: amountText(order.currency, order.paid) };
    case 'credit': return { tone: '', label: 'Saldo a favor', detail: amountText(order.currency, order.balance ?? '0') };
  }
}

/**
 * «Pagos al proveedor» inside Ver pedido: total, paid and balance always in
 * view, the payments made (voided ones stay visible, crossed out) and the
 * form to record a new one. Payments never change stock or the cash register.
 */
export function SupplierPayments({ order, timezone, canManage, onChanged }: {
  order: PurchaseOrder; timezone: string; canManage: boolean; onChanged: (order: PurchaseOrder, message: string) => void;
}) {
  const uid = useId();
  const [form, setForm] = useState(false);
  const [voiding, setVoiding] = useState<SupplierPayment | null>(null);
  const [notice, setNotice] = useState('');
  const opener = useRef<HTMLButtonElement>(null);
  const balance = order.balance === null ? null : minorUnits(order.balance);
  const payable = order.payable === null ? null : minorUnits(order.payable);
  const paid = minorUnits(order.paid);
  const percent = payable && payable > 0n ? Math.min(100, Number(paid * 100n / payable)) : 0;
  const canPay = canManage && (balance === null || balance > 0n);
  const active = order.payments.filter((payment) => !payment.voidedAt);
  const voided = order.payments.filter((payment) => payment.voidedAt);

  return <section className="pu-pay" aria-labelledby={uid + 'title'}>
    <div className="pu-pay-head">
      <h3 id={uid + 'title'}>Pagos al proveedor</h3>
      {canPay && !form && <button ref={opener} type="button" className="pu-btn primary" onClick={() => { setVoiding(null); setNotice(''); setForm(true); }}>＋ Registrar pago</button>}
    </div>
    <dl className="pu-pay-totals">
      <div><dt>Total del pedido</dt><dd>{order.payable === null ? 'Sin total' : amountText(order.currency, order.payable)}</dd></div>
      <div><dt>Pagado</dt><dd>{amountText(order.currency, order.paid)}</dd></div>
      <div data-tone={balance === null ? 'muted' : balance > 0n ? 'warning' : 'ok'}>
        <dt>{balance !== null && balance < 0n ? 'Saldo a favor' : 'Saldo'}</dt>
        <dd>{balance === null ? '—' : balance === 0n ? (paid > 0n ? 'Pagado ✓' : amountText(order.currency, '0.00')) : amountText(order.currency, order.balance!)}</dd>
      </div>
    </dl>
    {payable !== null && payable > 0n && <div className="pu-progress pu-pay-progress" role="img" aria-label={percent + ' % pagado'}><span style={{ width: percent + '%' }} /></div>}
    {order.payable === null && <div className="pu-note warn"><p>Hay renglones sin costo, así que no podemos calcular el saldo. Los pagos se registran igual.</p></div>}
    {order.status === 'CANCELLED' && order.payable !== null && <p className="pu-hint">Pedido cancelado: el total cuenta solo las unidades que se recibieron.</p>}

    {form && <PaymentForm order={order} timezone={timezone} onCancel={() => { setForm(false); requestAnimationFrame(() => opener.current?.focus()); }}
      onSaved={(updated, message) => { setForm(false); setNotice(message); onChanged(updated, message); }} />}
    {notice && !form && <p className="pu-feedback" role="status">✓ {notice}</p>}

    {order.payments.length === 0 ? !form && <p className="pu-hint pu-pay-empty">Todavía no registraste pagos de este pedido.</p> :
      <ul className="pu-pay-list" aria-label="Pagos registrados">
        {[...active, ...voided].map((payment) => <li key={payment.id} data-voided={payment.voidedAt ? true : undefined}>
          <div className="pu-pay-main">
            <b>{paymentMethodLabels[payment.method]}{payment.reference ? ' · ' + payment.reference : ''}</b>
            <small>Pagado el {dayText(payment.paidOn)}
              {payment.checkDueOn ? ' · se cobra el ' + dayText(payment.checkDueOn) : ''}
              {payment.note ? ' · ' + payment.note : ''}</small>
            {payment.voidedAt && <small className="pu-pay-void">Anulado{payment.voidReason ? ': ' + payment.voidReason : ''}</small>}
          </div>
          <div className="pu-pay-side">
            <b className="pu-pay-amount">{amountText(order.currency, payment.amount)}</b>
            {payment.checkDueOn && !payment.voidedAt && payment.checkDueOn > todayIn(timezone) && <span className="pu-pill warning">Cheque a cobrar</span>}
            {canManage && !payment.voidedAt && voiding?.id !== payment.id &&
              <button type="button" className="pu-link danger" onClick={() => { setForm(false); setVoiding(payment); }}
                aria-label={'Anular pago de ' + amountText(order.currency, payment.amount) + ' del ' + dayText(payment.paidOn)}>Anular</button>}
          </div>
          {voiding?.id === payment.id && <VoidForm order={order} payment={payment} onCancel={() => setVoiding(null)}
            onSaved={(updated, message) => { setVoiding(null); setNotice(message); onChanged(updated, message); }} />}
        </li>)}
      </ul>}
  </section>;
}

function PaymentForm({ order, timezone, onCancel, onSaved }: {
  order: PurchaseOrder; timezone: string; onCancel: () => void; onSaved: (order: PurchaseOrder, message: string) => void;
}) {
  const uid = useId();
  const today = todayIn(timezone);
  const [amount, setAmount] = useState(order.balance && minorUnits(order.balance) > 0n ? formatAmount(order.balance) : '');
  const [method, setMethod] = useState<SupplierPaymentMethod>('transfer');
  const [paidOn, setPaidOn] = useState(today);
  const [checkDueOn, setCheckDueOn] = useState('');
  const [reference, setReference] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const key = useRef(newKey());
  const first = useRef<HTMLInputElement>(null);
  useEffect(() => { first.current?.focus(); first.current?.select(); }, []);

  const parsed = parseCost(amount);
  const cents = typeof parsed === 'string' ? minorUnits(parsed) : null;
  const balance = order.balance === null ? null : minorUnits(order.balance);
  const remaining = cents !== null && balance !== null ? balance - cents : null;
  const issue = parsed === undefined ? 'Ingresá un importe válido, por ejemplo 150000 o 150.000,50.'
    : cents === null || cents <= 0n ? 'Ingresá el importe pagado.'
      : balance !== null && cents > balance ? 'El importe supera el saldo del pedido (' + amountText(order.currency, order.balance!) + ').'
        : !paidOn || paidOn > today ? 'La fecha de pago no puede ser futura.'
          : method === 'check' && checkDueOn && checkDueOn < paidOn ? 'La fecha de cobro no puede ser anterior a la del pago.' : '';

  async function submit() {
    if (busy) return;
    if (issue) { setError(issue); return; }
    setBusy(true);
    setError('');
    try {
      const updated = await api.registerSupplierPayment(order.id, {
        storeId: order.storeId, method, amount: parsed as string, paidOn,
        ...(method === 'check' && checkDueOn ? { checkDueOn } : {}), reference: reference.trim(), note: note.trim(),
      }, key.current);
      const left = updated.balance === null ? '' : minorUnits(updated.balance) > 0n ? ' Saldo: ' + amountText(updated.currency, updated.balance) + '.' : ' El pedido quedó pagado.';
      onSaved(updated, 'Pago de ' + amountText(updated.currency, parsed as string) + ' registrado.' + left);
    } catch (cause) {
      setError(cause instanceof ApiError && cause.code === 'purchase_payment_exceeds' ? 'El importe supera el saldo actual del pedido. Actualizá y revisá los pagos.'
        : cause instanceof Error ? cause.message : 'No pudimos registrar el pago.');
    } finally {
      setBusy(false);
    }
  }

  return <div className="pu-pay-form" role="group" aria-labelledby={uid + 'title'}
    onKeyDown={(event) => { if (event.key === 'Enter' && event.target instanceof HTMLInputElement) { event.preventDefault(); void submit(); } }}>
    <h4 id={uid + 'title'}>Nuevo pago</h4>
    <div className="pu-formgrid">
      <label>Importe
        <span className="pu-pay-amount-field"><span aria-hidden="true">{order.currency}</span>
          <input ref={first} value={amount} inputMode="decimal" autoComplete="off" disabled={busy} aria-describedby={uid + 'remaining'}
            onChange={(event) => { setAmount(event.target.value); setError(''); }} /></span>
        <small id={uid + 'remaining'} className="pu-hint">{remaining === null ? (balance === null ? 'Sin total calculado para este pedido.' : '')
          : remaining > 0n ? 'Queda un saldo de ' + amountText(order.currency, decimalText(remaining)) : remaining === 0n ? 'Salda el pedido completo.' : ''}</small>
      </label>
      <label>Medio de pago
        <select value={method} disabled={busy} onChange={(event) => { setMethod(event.target.value as SupplierPaymentMethod); setError(''); }}>
          {(Object.keys(paymentMethodLabels) as SupplierPaymentMethod[]).map((value) => <option key={value} value={value}>{paymentMethodLabels[value]}</option>)}
        </select>
      </label>
      <label>Fecha de pago
        <input type="date" value={paidOn} max={today} disabled={busy} onChange={(event) => { setPaidOn(event.target.value); setError(''); }} />
      </label>
      {method === 'check' ? <label>Fecha de cobro del cheque
        <input type="date" value={checkDueOn} min={paidOn || undefined} max="9999-12-31" disabled={busy} onChange={(event) => { setCheckDueOn(event.target.value); setError(''); }} />
        <small className="pu-hint">Opcional. Completala si es un cheque diferido.</small>
      </label> : <span aria-hidden="true" />}
      <label>{method === 'check' ? 'N° de cheque y banco' : 'Comprobante'}
        <input value={reference} maxLength={80} disabled={busy} placeholder={referenceHints[method]} onChange={(event) => setReference(event.target.value)} />
      </label>
      <label>Nota
        <input value={note} maxLength={300} disabled={busy} placeholder="Opcional" onChange={(event) => setNote(event.target.value)} />
      </label>
    </div>
    {error && <p className="pu-alert" role="alert">{error}</p>}
    <div className="pu-pay-actions">
      <button type="button" className="pu-btn" disabled={busy} onClick={onCancel}>Cancelar</button>
      <button type="button" className="pu-btn primary" disabled={busy} onClick={() => void submit()}>{busy ? 'Registrando…' : 'Registrar pago'}</button>
    </div>
    <p className="pu-hint">Registrar un pago no mueve la caja. Si sale efectivo de la caja, cargalo también como retiro en Caja.</p>
  </div>;
}

function VoidForm({ order, payment, onCancel, onSaved }: {
  order: PurchaseOrder; payment: SupplierPayment; onCancel: () => void; onSaved: (order: PurchaseOrder, message: string) => void;
}) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { input.current?.focus(); }, []);
  async function submit() {
    if (busy) return;
    if (!reason.trim()) { setError('Contanos por qué se anula, por ejemplo «cheque rechazado».'); return; }
    setBusy(true);
    setError('');
    try {
      const updated = await api.voidSupplierPayment(order.id, payment.id, order.storeId, reason.trim());
      onSaved(updated, 'Pago de ' + amountText(order.currency, payment.amount) + ' anulado.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos anular el pago.');
    } finally {
      setBusy(false);
    }
  }
  return <div className="pu-pay-voidform">
    <label className="pu-label">Motivo de la anulación
      <input ref={input} value={reason} maxLength={300} disabled={busy} placeholder="Ej. cheque rechazado, cargado dos veces"
        onChange={(event) => { setReason(event.target.value); setError(''); }}
        onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void submit(); } if (event.key === 'Escape') { event.stopPropagation(); onCancel(); } }} />
    </label>
    {error && <p className="pu-alert" role="alert">{error}</p>}
    <p className="pu-hint">El pago queda en el historial tachado y deja de sumar.</p>
    <div className="pu-pay-actions">
      <button type="button" className="pu-btn" disabled={busy} onClick={onCancel}>Volver</button>
      <button type="button" className="pu-btn danger" disabled={busy} onClick={() => void submit()}>{busy ? 'Anulando…' : 'Anular pago'}</button>
    </div>
  </div>;
}
