import { useEffect, useRef, useState, useId } from 'react';
import { createPortal } from 'react-dom';
import type { SaleTicket } from '../../shared/api/client';
import { posPrice } from './priceDisplay';
import { SalePaymentBreakdown } from './SalePayments';
import './ticket.css';

type PaperWidth = '58' | '80';

function TicketBody({ ticket, paper }: { ticket: SaleTicket; paper: PaperWidth }) {
  const { sale } = ticket;
  const datetime = new Intl.DateTimeFormat('es-AR', {
    dateStyle: 'short', timeStyle: 'short', timeZone: ticket.timezone,
  }).format(new Date(sale.createdAt));
  const display = (amount: string) => sale.currency + '\u00a0' + posPrice(amount);
  return (
    <article className="receipt" aria-label="Contenido del ticket" style={{ width: paper === '58' ? '52mm' : '74mm' }}>
      <header>
        <strong>{ticket.merchantName}</strong>
        <div>{ticket.storeName} ({ticket.storeCode})</div>
        {ticket.storeAddress && <div>{ticket.storeAddress}</div>}
        <p className="receipt-notice"><strong>COMPROBANTE NO FISCAL</strong><b>NO VÁLIDO COMO FACTURA</b></p>
        <h3>Venta N.º {sale.number}</h3>
        <time dateTime={sale.createdAt}>{datetime}</time>
      </header>
      <ul>
        {sale.items.map((line) => <li key={line.variantId}>
          <strong>{line.product}</strong>
          <div className="receipt-variant">{[line.sku, line.size, line.color].filter(Boolean).join(' · ')}</div>
          {!!line.discountPercent && <div>
            Descuento {line.discountPercent} %{line.listUnitPrice && <> · Lista {display(line.listUnitPrice)}</>}
          </div>}
          <dl className="receipt-line">
            <dt><span className="pos-sr-only">Cantidad por precio unitario: </span>{line.quantity} × {display(line.unitPrice)}</dt>
            <dd><span className="pos-sr-only">Subtotal: </span><span aria-hidden="true">= </span>{display(line.subtotal)}</dd>
          </dl>
        </li>)}
      </ul>
      <section className="receipt-payment" aria-label="Total y medios de pago">
        <dl>
          <dt>Total</dt><dd>{display(sale.total)}</dd>
          <SalePaymentBreakdown sale={sale} display={display} />
        </dl>
      </section>
    </article>
  );
}

export function TicketDialog({ ticket, onClose }: { ticket: SaleTicket; onClose: () => void }) {
  const instanceId = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const [paper, setPaper] = useState<PaperWidth>('80');

  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    return () => element?.close();
  }, []);

  return (
    <>
      <dialog ref={dialog} className="ticket-dialog" aria-labelledby={instanceId + "-ticket-title"} onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}>
        <div className="ticket-scroll">
          <h2 id={instanceId + "-ticket-title"}>Comprobante de venta</h2>
          <label>Ancho del papel
            <select name="ticketPaper" value={paper} onChange={(event) => setPaper(event.target.value === '58' ? '58' : '80')}>
              <option value="80">80 mm</option>
              <option value="58">58 mm</option>
            </select>
          </label>
          <p>Elegí el mismo ancho en la impresora. Usá escala del 100 % y desactivá encabezados y pies de página.</p>
          <div className="ticket-preview"><TicketBody ticket={ticket} paper={paper} /></div>
        </div>
        <div className="actions">
          <button type="button" onClick={() => window.print()}>Imprimir o guardar PDF</button>
          <button type="button" onClick={onClose}>Cerrar</button>
        </div>
      </dialog>
      {createPortal(
        <div className="ticket-print" aria-hidden="true"><TicketBody ticket={ticket} paper={paper} /></div>,
        document.body,
      )}
    </>
  );
}
