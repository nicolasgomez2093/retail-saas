import { Fragment } from 'react';
import type { Sale, SalePaymentMethod } from '../../shared/api/client';

export const paymentLabels: Record<SalePaymentMethod, string> = {
  cash: 'Efectivo', transfer: 'Transferencia', debit: 'Débito', credit: 'Crédito', qr: 'QR',
};
export const paymentMethods = ['cash', 'transfer', 'debit', 'credit', 'qr'] as const;

export function SalePaymentBreakdown({ sale, display }: { sale: Sale; display: (amount: string) => string }) {
  return <>
    {sale.deposit && <>
      <dt>Seña ({paymentLabels[sale.deposit.method]})</dt><dd>{display(sale.deposit.amount)}</dd>
    </>}
    {sale.payments.map((payment) => <Fragment key={payment.method}>
      <dt>{paymentLabels[payment.method]}</dt><dd>{display(payment.amount)}</dd>
    </Fragment>)}
    {sale.payments.some((payment) => payment.method === 'cash') && <>
      <dt>Efectivo recibido</dt><dd>{display(sale.cashReceived)}</dd>
      <dt>Vuelto</dt><dd>{display(sale.change)}</dd>
    </>}
    {sale.credit && <>
      <dt>Cliente</dt><dd>{sale.credit.customerName}</dd>
      <dt>Saldo fiado al vender</dt><dd>{display(sale.credit.originalAmount)}</dd>
    </>}
  </>;
}
