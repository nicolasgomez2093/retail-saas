import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, type Customer, type CustomerPage } from '../../shared/api/client';

const pageSize = 25;

export function CustomerPicker({ storeId, disabled, selection, onSelect, label = 'Cliente del fiado', directory = false, directoryActive = false }: {
  directory?: boolean; directoryActive?: boolean;
  storeId: string; disabled: boolean; selection: Customer | null; label?: string; onSelect: (customer: Customer | null) => void;
}) {
  const [result, setResult] = useState<{ page: CustomerPage; query: string; offset: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const [profile, setProfile] = useState<Customer | null>(null);
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState('');
  const directoryCriteria = useRef({ query: '', offset: 0 });
  const searchVersion = useRef(0);
  const profileHeading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    if (profile?.id) profileHeading.current?.focus();
  }, [profile?.id]);

  useEffect(() => {
    if (!directoryActive) return;
    let active = true;
    const { query, offset } = directoryCriteria.current;
    const version = searchVersion.current;
    void api.customers(storeId, query, offset, true).then((page) => {
      if (active && searchVersion.current === version) {
        setResult({ page, query, offset });
        setError('');
      }
    }).catch(() => {
      if (active && searchVersion.current === version) setError('No pudimos consultar el directorio de clientes.');
    });
    return () => { active = false; };
  }, [directoryActive, storeId]);

  async function search(query: string, offset = 0) {
    searchVersion.current++;
    if (directory) directoryCriteria.current = { query, offset };
    setBusy(true);
    setError('');
    try {
      const page = await api.customers(storeId, query, offset, directory);
      setResult({ page, query, offset });
    } catch (cause) {
      setResult(null);
      setError(cause instanceof Error ? cause.message : 'No pudimos buscar clientes.');
    } finally {
      setBusy(false);
    }
  }

  const creating = useRef(false);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (creating.current || disabled || busy) return;
    creating.current = true;
    const form = event.currentTarget;
    const values = new FormData(form);
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const customer = await api.createCustomer(storeId, String(values.get('name')), String(values.get('phone')));
      if (!directory) onSelect(customer);
      form.reset();
      if (directory) {
        await search(customer.name);
        setNotice('Cliente registrado.');
      }
      else setResult(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos registrar el cliente.');
    } finally {
      creating.current = false;
      setBusy(false);
    }
  }

  async function update(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!profile || busy || disabled) return;
    const values = new FormData(event.currentTarget);
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const customer = await api.updateCustomer(storeId, profile.id, String(values.get('name')), String(values.get('phone')));
      setProfile(customer);
      setEditing(false);
      await search(result?.query ?? '', result?.offset ?? 0);
      setNotice('Cliente actualizado.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos actualizar el cliente.');
    } finally {
      setBusy(false);
    }
  }

  const summaries = new Map(result?.page.summaries?.map((entry) => [entry.customerId, entry]));
  const timezone = result?.page.timezone;
  const date = timezone ? new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeZone: timezone }) : undefined;

  return <fieldset className="inventory-form" disabled={disabled || busy}>
    <legend className={directory ? "pos-sr-only" : undefined}>{label}</legend>
    {selection && <p role="status">Cliente seleccionado: <strong>{selection.name}</strong>{selection.phone && ' · ' + selection.phone}</p>}
    <form aria-label="Buscar clientes" onSubmit={(event) => {
      event.preventDefault();
      void search(String(new FormData(event.currentTarget).get('q')));
    }}>
      <label>Nombre o teléfono<input name="q" maxLength={120} /></label>
      <button>Buscar clientes</button>
    </form>
    {result && (result.page.items.length === 0 ? <p>No hay clientes para esa búsqueda.</p> :
      <ul className="catalog-items">{result.page.items.map((item) => {
        const summary = summaries.get(item.id);
        return <li key={item.id}>
        {directory ? <>
          <span><strong>{item.name}</strong>{item.phone && <small>{item.phone}</small>}</span>
          {summary && <dl className="directory-customer-summary">
            <div><dt>Última compra identificada</dt><dd>{summary.lastPurchase && date
              ? <time dateTime={summary.lastPurchase}>{date.format(new Date(summary.lastPurchase))}</time> : 'Sin compras registradas'}</dd></div>
            <div><dt>Saldo pendiente</dt><dd>{summary.balances.length
              ? summary.balances.map(({ currency, amount }) => currency + ' ' + amount.replace('.', ',')).join(' · ')
              : 'Sin saldo pendiente'}</dd></div>
          </dl>}
          <button type="button" className="catalog-link" aria-label={'Ver ficha de ' + item.name} onClick={() => {
            setProfile(item);
            setEditing(false);
            setNotice('');
            setError('');
          }}>Ver ficha</button>
          <button type="button" className="catalog-link" aria-label={'Ver cuenta de ' + item.name} onClick={() => onSelect(item)}>Ver cuenta</button>
        </> : <button type="button" className="catalog-link" onClick={() => onSelect(item)}>
          {item.name}{item.phone && ' · ' + item.phone}
        </button>}
      </li>; })}</ul>)}
    {result && (result.offset > 0 || result.page.hasMore) && <nav className="actions" aria-label="Páginas de clientes">
      <button type="button" disabled={result.offset === 0} onClick={() => void search(result.query, result.offset - pageSize)}>Anterior</button>
      <button type="button" disabled={!result.page.hasMore} onClick={() => void search(result.query, result.offset + pageSize)}>Siguiente</button>
    </nav>}
    {directory && profile && <section className="feature-panel customer-profile" aria-label="Ficha de cliente">
      <h3 tabIndex={-1} ref={profileHeading}>{profile.name}</h3>
      {editing ? <form aria-label="Editar cliente" onSubmit={(event) => void update(event)}>
        <label>Nombre del cliente<input name="name" defaultValue={profile.name} required maxLength={120} /></label>
        <label>Teléfono<input name="phone" defaultValue={profile.phone} type="tel" maxLength={40} /></label>
        <div className="actions">
          <button>Guardar cambios</button>
          <button type="button" className="button-secondary" onClick={() => { setEditing(false); setError(''); }}>Cancelar</button>
        </div>
      </form> : <>
        <p>{profile.phone || 'Sin teléfono registrado'}</p>
        <div className="actions">
          <button type="button" className="button-secondary" onClick={() => setEditing(true)}>Editar cliente</button>
          <button type="button" className="button-secondary" onClick={() => onSelect(profile)}>Ver cuenta corriente</button>
          <button type="button" className="button-secondary" onClick={() => setProfile(null)}>Cerrar ficha</button>
        </div>
      </>}
    </section>}
    <details className="catalog-classifications">
      <summary>Nuevo cliente</summary>
      <form aria-label="Nuevo cliente" className="inventory-form" onSubmit={(event) => void create(event)}>
        <label>Nombre del cliente<input name="name" required maxLength={120} /></label>
        <label>Teléfono<input name="phone" type="tel" maxLength={40} /></label>
        <button>Registrar cliente</button>
      </form>
    </details>
    {notice && <p role="status">{notice}</p>}
    {error && <p role="alert">{error}</p>}
  </fieldset>;
}
