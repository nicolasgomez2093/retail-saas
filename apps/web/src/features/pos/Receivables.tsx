import { useEffect, useState, type FormEvent } from 'react';
import { api, type CashRegisters, type Receivable, type ReceivablePage, type ReceivableStatus } from '../../shared/api/client';

import { CustomerPicker } from './CustomerPicker';
import { ReceivableCollections } from './ReceivableCollections';
import { paymentLabels } from './SalePayments';

const pageSize = 25;

export function Receivables({ active, showAccounts, storeId, revision, ticketBusy, registers, disabled, onTicket, onPending, onCollected }: {
  active: boolean; showAccounts: boolean; storeId: string; revision: number; ticketBusy: boolean; registers: CashRegisters; disabled: boolean;
  onTicket: (id: string, opener: HTMLButtonElement) => void; onPending: (pending: boolean) => void; onCollected: () => void;
}) {
  const [retry, setRetry] = useState(0);
  const [selected, setSelected] = useState<Receivable | null>(null);
  const [status, setStatus] = useState<ReceivableStatus>('pending');
  const [query, setQuery] = useState('');
  const [searchText, setSearchText] = useState('');
  const [customer, setCustomer] = useState<{ id: string; name: string } | null>(null);
  const customerId = customer?.id;
  const [locked, setLocked] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const [offset, setOffset] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: ReceivablePage; error?: string } | null>(null);
  const key = JSON.stringify([storeId, status, offset, revision, query, customerId, retry]);
  const current = result?.key === key ? result : null;

  useEffect(() => {
    if (!active || !showAccounts) return;
    let currentRequest = true;
    void api.receivables(storeId, { status, query, customerId }, offset).then((page) => {
      if (currentRequest) setResult({ key, page });
    }).catch((cause: unknown) => {
      if (currentRequest) setResult({ key, error: cause instanceof Error ? cause.message : 'No pudimos consultar los fiados.' });
    });
    return () => { currentRequest = false; };
  }, [key, offset, active, showAccounts, status, storeId, query, customerId]);

  function applySearch(value: string) {
    setQuery(value.trim());
    setOffset(0);
    setSelected(null);
    setExportError('');
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!locked) applySearch(searchText);
  }

  function viewCustomer(id: string, name: string) {
    setCustomer({ id, name });
    setStatus('pending');
    setSearchText('');
    applySearch('');
  }

  async function downloadCSV() {
    setExporting(true);
    setExportError('');
    try {
      const file = await api.downloadReceivablesCSV(storeId, { status, query, customerId });
      const url = URL.createObjectURL(file);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'fiados.csv';
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : 'No pudimos descargar los fiados.');
    } finally {
      setExporting(false);
    }
  }

  const dates = new Intl.DateTimeFormat('es-AR', {
    dateStyle: 'short', timeStyle: 'short', timeZone: registers.timezone,
  });

  return <div className="customers-view">
    <nav className="stock-nav" aria-label="Secciones de Clientes">
      <a href="#/clientes" aria-current={!showAccounts ? 'page' : undefined}>Directorio</a>
      <a href="#/finanzas/fiados" aria-current={showAccounts ? 'page' : undefined}>Cuentas corrientes</a>
    </nav>
    <div hidden={showAccounts} className="customers-directory-view">
      <h2>Directorio de clientes</h2>
      <CustomerPicker storeId={storeId} directoryActive={active && !showAccounts} directory
        disabled={locked || disabled} selection={null} label="Directorio"
        onSelect={(person) => {
          if (person) {
            viewCustomer(person.id, person.name);
            window.location.hash = '/finanzas/fiados';
          }
        }} />
    </div>
    <div hidden={!showAccounts}>
    <div className="customers-heading">
      <h2>Fiados</h2>
      <button className="button-secondary" type="button" disabled={exporting || locked || !current?.page} onClick={() => void downloadCSV()}>
        {exporting ? 'Preparando CSV…' : 'Descargar CSV'}
      </button>
    </div>
    <details className="customers-directory">
      <summary>Buscar o registrar un cliente</summary>
      <CustomerPicker storeId={storeId} disabled={locked} selection={null} label="Buscar por nombre o teléfono"
        onSelect={(person) => {
          if (person) viewCustomer(person.id, person.name);
        }} />
    </details>
    <div className="customers-filters">
      <form className="inventory-form" aria-label="Buscar fiados" onSubmit={search}
        onReset={(event) => {
          event.preventDefault();
          if (locked) return;
          setSearchText('');
          applySearch('');
          event.currentTarget.querySelector('input')?.focus();
        }}>
        <label>Cliente o número de venta<input name="q" type="search" value={searchText}
          onChange={(event) => setSearchText(event.target.value)} maxLength={120} disabled={locked} /></label>
        <button disabled={locked}>Buscar</button>
        {(query || searchText) && <button className="customers-clear" type="reset" disabled={locked}>Limpiar búsqueda</button>}
      </form>
      <label>Estado del fiado
        <select disabled={locked} value={status} onChange={(event) => {
          setStatus(event.target.value === 'settled' ? 'settled' : 'pending');
          setOffset(0);
          setSelected(null);
        }}>
          <option value="pending">Pendientes</option>
          <option value="settled">Saldados</option>
        </select>
      </label>
    </div>
    {customer && <div className="actions">
      <p role="status">Fiados de {customer.name}</p>
      <button className="button-secondary" type="button" disabled={locked} onClick={() => {
        setCustomer(null);
        setOffset(0);
        setSelected(null);
        setExportError('');
      }}>Quitar filtro de cliente</button>
    </div>}
    {current?.page && (current.page.summary.length > 0 || current.page.recentPayments.length > 0) && <details className="customers-report">
      <summary>{query ? 'Resumen de la búsqueda' : 'Resumen y últimos abonos'}</summary>
      {current.page.summary.map((total) => <div key={total.currency}><dl className="customers-totals">
        <dt>Saldo pendiente</dt><dd>{total.currency} {total.outstanding.replace('.', ',')}</dd>
        <dt>Abonos registrados</dt><dd>{total.currency} {total.collected.replace('.', ',')}</dd>
        <dt>Compensaciones de deuda</dt><dd>{total.currency} {total.debtCredits.replace('.', ',')}</dd>
        <dt>Fiados pendientes</dt><dd>{total.pendingCount.toLocaleString('es-AR')}</dd>
        <dt>Fiados saldados</dt><dd>{total.settledCount.toLocaleString('es-AR')}</dd>
      </dl>
        {!customer && total.topDebtors.length > 0 && <div className="inventory-table"><table>
          <caption>Hasta 10 clientes con mayor saldo pendiente ({total.currency})</caption>
          <thead><tr><th scope="col">Cliente</th><th scope="col">Fiados pendientes</th><th scope="col">Saldo pendiente</th></tr></thead>
          <tbody>{total.topDebtors.map((customer) => <tr key={customer.customerId}>
            <th scope="row"><button type="button" className="catalog-link" disabled={locked}
              onClick={() => viewCustomer(customer.customerId, customer.customerName)}>{customer.customerName}</button></th>
            <td>{customer.pendingCount.toLocaleString('es-AR')}</td>
            <td>{total.currency} {customer.outstanding.replace('.', ',')}</td>
          </tr>)}</tbody>
        </table></div>}
        {total.paymentsByMethod.length > 0 && <div className="inventory-table"><table>
          <caption>Abonos por medio de pago ({total.currency})</caption>
          <thead><tr><th scope="col">Medio</th><th scope="col">Abonos</th><th scope="col">Importe</th></tr></thead>
          <tbody>{total.paymentsByMethod.map((payment) => <tr key={payment.method}>
            <th scope="row">{paymentLabels[payment.method]}</th>
            <td>{payment.paymentCount.toLocaleString('es-AR')}</td>
            <td>{total.currency} {payment.amount.replace('.', ',')}</td>
          </tr>)}</tbody>
        </table></div>}
      </div>)}
    {current?.page && current.page.recentPayments.length > 0 && <div className="inventory-table"><table>
      <caption>Últimos 10 abonos</caption>
      <thead><tr><th scope="col">Fecha</th><th scope="col">Cliente</th><th scope="col">Venta</th>
        <th scope="col">Medio</th><th scope="col">Importe</th></tr></thead>
      <tbody>{current.page.recentPayments.map((payment) => <tr key={payment.id}>
        <td><time dateTime={payment.createdAt}>{dates.format(new Date(payment.createdAt))}</time></td>
        <th scope="row">{payment.customerName}</th>
        <td><button type="button" className="catalog-link" disabled={ticketBusy} onClick={(event) => onTicket(payment.saleId, event.currentTarget)}>
          Ver ticket de la venta {payment.saleNumber}
        </button></td>
        <td>{paymentLabels[payment.method]}</td>
        <td>{payment.currency} {payment.amount.replace('.', ',')}</td>
      </tr>)}</tbody>
    </table></div>}
    </details>}
    {exportError && <p role="alert">{exportError}</p>}
    {active && !current && <p role="status">Consultando fiados…</p>}
    {current?.error && <div>
      <p role="alert">{current.error}</p>
      <button type="button" onClick={() => setRetry((value) => value + 1)}>Reintentar</button>
    </div>}
    {current?.page?.items.length === 0 && <p>{query || customer ? 'No hay fiados que coincidan con los filtros.'
      : status === 'pending' ? 'No hay fiados pendientes.' : 'No hay fiados saldados.'}</p>}
    {current?.page && current.page.items.length > 0 && <ul className="catalog-items customers-list">
      {current.page.items.map((item) => <li key={item.id}>
        <p><strong>{item.customerName}</strong><small>{status === 'pending'
          ? 'Pendiente ' + item.currency + ' ' + item.outstandingAmount.replace('.', ',')
          : 'Saldado'}</small></p>
        <div className="actions">
          <button type="button" className="catalog-link" disabled={ticketBusy} onClick={(event) => onTicket(item.saleId, event.currentTarget)}>
            Ticket<span className="pos-sr-only"> de la venta {item.saleNumber}</span>
          </button>
          <button type="button" className="catalog-link" disabled={locked || disabled} onClick={() => setSelected(item)}>
            Ver abonos<span className="pos-sr-only"> de la venta {item.saleNumber}</span>
          </button>
        </div>
      </li>)}
    </ul>}
    {(offset > 0 || current?.page?.hasMore) && <nav className="actions" aria-label="Páginas de fiados">
      <button type="button" disabled={!current || offset === 0 || locked} onClick={() => setOffset(offset - pageSize)}>Anterior</button>
      <button type="button" disabled={!current?.page?.hasMore || locked} onClick={() => setOffset(offset + pageSize)}>Siguiente</button>
    </nav>}
    {selected && <ReceivableCollections key={selected.id} item={selected} storeId={storeId} registers={registers}
      disabled={disabled} revision={revision} onCollected={() => { setOffset(0); onCollected(); }} onPending={(pending) => {
        setLocked(pending);
        onPending(pending);
      }} />}
    </div>
  </div>;
}
