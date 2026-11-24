import { useEffect, useId, useRef, useState, type FormEvent } from 'react';

import { api, ApiError, type Supplier, type SupplierInput, type SupplierPage } from '../../shared/api/client';
import '../purchases/purchases.css';

const pageSize = 25;

export function Suppliers({ storeRevision, selectedId, onHistory }: {
  storeRevision: number; selectedId: string; onHistory: (id: string, name: string) => void;
}) {
  const uid = useId();
  const [draft, setDraft] = useState('');
  const [query, setQuery] = useState('');
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ key: string; page?: SupplierPage; error?: string; denied?: boolean } | null>(null);
  const [editing, setEditing] = useState<Supplier | 'new' | null>(null);
  const [toast, setToast] = useState('');
  const opener = useRef<HTMLElement | null>(null);
  const key = JSON.stringify([query, offset, revision, storeRevision]);
  const current = result?.key === key ? result : null;
  const page = current?.page;

  useEffect(() => {
    const handle = window.setTimeout(() => {
      setQuery(draft.trim());
      setOffset(0);
    }, 300);
    return () => window.clearTimeout(handle);
  }, [draft]);

  useEffect(() => {
    let active = true;
    void api.suppliers(query, offset).then((loaded) => {
      if (active) setResult({ key, page: loaded });
    }).catch((cause: unknown) => {
      if (active) setResult(cause instanceof ApiError && cause.status === 403 ? { key, denied: true } : { key, error: message(cause) });
    });
    return () => { active = false; };
  }, [key, query, offset]);

  useEffect(() => {
    if (!toast) return;
    const handle = window.setTimeout(() => setToast(''), 3200);
    return () => window.clearTimeout(handle);
  }, [toast]);

  if (current?.denied) return <p>No tenés acceso a los proveedores de este comercio.</p>;

  function open(target: Supplier | 'new', button: HTMLElement) {
    opener.current = button;
    setEditing(target);
  }

  return <section className="pu" aria-labelledby={uid + 'title'}>
    <header className="pu-head">
      <div><h2 id={uid + 'title'}>Proveedores</h2><p>Datos de contacto de quienes te venden mercadería.</p></div>
      <div className="pu-head-actions">
        <a className="pu-btn" href="#/compras/ordenes">◈ Ver pedidos</a>
        {page?.canManage && <button type="button" className="pu-btn primary" onClick={(event) => open('new', event.currentTarget)}>＋ Nuevo proveedor</button>}
      </div>
    </header>
    <div className="pu-card">
      <div className="pu-filters">
        <label className="pu-search"><span aria-hidden="true">⌕</span>
          <input type="search" value={draft} maxLength={160} aria-label="Buscar proveedor"
            placeholder="Buscar por nombre, CUIT, contacto, teléfono o correo…" onChange={(event) => setDraft(event.target.value)} />
        </label>
      </div>
      {!current && <p className="pu-status" role="status">Consultando proveedores…</p>}
      {current?.error && <div className="pu-panelbody">
        <p className="pu-alert" role="alert">{current.error}</p>
        <button type="button" className="pu-btn" onClick={() => setRevision((value) => value + 1)}>Reintentar</button>
      </div>}
      {page && (page.items.length === 0 ? <div className="pu-empty">
        <h3>{query ? 'No encontramos proveedores' : 'Todavía no hay proveedores'}</h3>
        <p>{query ? 'Probá con otro nombre, CUIT o teléfono.' : 'Registralo una vez y elegilo en cada pedido.'}</p>
        {page.canManage && !query && <button type="button" className="pu-btn primary" onClick={(event) => open('new', event.currentTarget)}>＋ Nuevo proveedor</button>}
      </div> : <div className="pu-table-wrap">
        <table className="pu-table">
          <thead><tr><th scope="col">Proveedor</th><th scope="col">CUIT</th><th scope="col">Teléfono</th><th scope="col">Correo</th><th scope="col">Estado</th><th scope="col"><span className="pos-sr-only">Acciones</span></th></tr></thead>
          <tbody>{page.items.map((item) => <tr key={item.id} aria-current={item.id === selectedId ? 'true' : undefined}>
            <th scope="row">{item.name}{item.contactName && <small>Contacto: {item.contactName}</small>}</th>
            <td data-label="CUIT">{item.taxId || '—'}</td>
            <td data-label="Teléfono">{item.phone || '—'}</td>
            <td data-label="Correo">{item.email || '—'}</td>
            <td data-label="Estado"><span className={'pu-pill' + (item.status === 'active' ? '' : ' muted')}>{item.status === 'active' ? 'Activo' : 'Inactivo'}</span></td>
            <td className="pu-cell-actions">
              {page.canManage && <button type="button" className="pu-btn" aria-label={'Editar ' + item.name} onClick={(event) => open(item, event.currentTarget)}>Editar</button>}
              {' '}<a className="pu-btn" href={'#/compras/ordenes?' + new URLSearchParams({ proveedor: item.id })}
                onClick={() => onHistory(item.id, item.name)} aria-label={'Ver pedidos de ' + item.name}>Pedidos →</a>
            </td>
          </tr>)}</tbody>
        </table>
      </div>)}
      {page && (offset > 0 || page.hasMore) && <nav className="pu-pager" aria-label="Páginas de proveedores">
        <button type="button" className="pu-btn" disabled={!offset} onClick={() => setOffset(offset - pageSize)}>← Anterior</button>
        <span>Página {offset / pageSize + 1}</span>
        <button type="button" className="pu-btn" disabled={!page.hasMore} onClick={() => setOffset(offset + pageSize)}>Siguiente →</button>
      </nav>}
    </div>
    {editing && <SupplierDialog supplier={editing === 'new' ? null : editing} returnTo={opener.current}
      onClose={() => setEditing(null)} onSaved={(saved, created) => {
        setEditing(null);
        if (created) { setDraft(''); setQuery(''); }
        setOffset(0);
        setRevision((value) => value + 1);
        setToast(created ? 'Proveedor ' + saved.name + ' registrado.' : 'Cambios de ' + saved.name + ' guardados.');
      }} />}
    {toast && <p className="pu-toast" role="status">{toast}</p>}
  </section>;
}

type Draft = SupplierInput & { status: Supplier['status'] };

/**
 * Create or edit a supplier in the .modal of the approved HTML. From a
 * purchase, `onUseExisting` lets the person pick the supplier that already
 * has that name instead of creating a homonym.
 */
export function SupplierDialog({ supplier, initialName = '', returnTo, onClose, onSaved, onUseExisting }: {
  supplier: Supplier | null; initialName?: string; returnTo: HTMLElement | null;
  onClose: () => void; onSaved: (supplier: Supplier, created: boolean) => void; onUseExisting?: (existing: { id: string; name: string }) => void;
}) {
  const uid = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const nameInput = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<Draft>(() => ({
    name: supplier?.name ?? initialName, email: supplier?.email ?? '', phone: supplier?.phone ?? '',
    taxId: supplier?.taxId ?? '', contactName: supplier?.contactName ?? '', status: supplier?.status ?? 'active',
  }));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<{ field?: 'name' | 'taxId' | 'email'; text: string } | null>(null);
  const [homonym, setHomonym] = useState<{ id: string; name: string } | null>(null);
  const running = useRef(false);

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    element.showModal();
    nameInput.current?.focus();
    return () => {
      element.close();
      if (returnTo?.isConnected) returnTo.focus();
    };
  }, [returnTo]);

  async function save(allowDuplicateName = false) {
    if (running.current) return;
    const name = draft.name.trim();
    if (!name) {
      setError({ field: 'name', text: 'Escribí el nombre del proveedor.' });
      nameInput.current?.focus();
      return;
    }
    running.current = true;
    setSaving(true);
    setError(null);
    const input = { name, email: draft.email.trim(), phone: draft.phone.trim(), taxId: draft.taxId.trim(), contactName: draft.contactName.trim() };
    try {
      const saved = supplier
        ? await api.updateSupplier(supplier.id, { ...input, status: draft.status, ...(allowDuplicateName ? { allowDuplicateName } : {}) })
        : await api.createSupplier({ ...input, ...(allowDuplicateName ? { allowDuplicateName } : {}) });
      onSaved(saved, !supplier);
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'supplier_duplicate' && cause.field === 'name' && cause.existing) {
        setHomonym(cause.existing);
      } else if (cause instanceof ApiError && cause.code === 'supplier_duplicate') {
        setError({ field: 'taxId', text: cause.message.charAt(0).toUpperCase() + cause.message.slice(1) + '.' });
      } else if (cause instanceof ApiError && cause.status === 400) {
        setError({ field: draft.email.trim() ? 'email' : undefined, text: 'Revisá los datos: el correo debe ser válido y los textos no pueden superar el largo permitido.' });
      } else {
        setError({ text: message(cause) });
      }
    } finally {
      running.current = false;
      setSaving(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void save();
  }

  const set = (field: keyof Draft) => (event: { target: { value: string } }) => {
    setDraft((value) => ({ ...value, [field]: event.target.value }));
    if (field === 'name') setHomonym(null);
  };
  return <dialog ref={dialog} className="pu-dialog" aria-labelledby={uid + 'title'} onCancel={(event) => { event.preventDefault(); if (!saving) onClose(); }}>
    <header><h2 id={uid + 'title'}>{supplier ? 'Editar proveedor' : 'Nuevo proveedor'}</h2>
      <button type="button" className="pu-close" aria-label="Cerrar" disabled={saving} onClick={onClose}>×</button></header>
    <form id={uid + 'form'} className="pu-dialog-body" onSubmit={submit} noValidate>
      {error && !error.field && <p className="pu-alert" role="alert">{error.text}</p>}
      {homonym && <div className="pu-note warn" role="alert">
        <p>Ya existe un proveedor llamado <b>{homonym.name}</b>.</p>
        <div className="pu-actions">
          {onUseExisting && <button type="button" className="pu-btn primary" onClick={() => onUseExisting(homonym)}>Usar ese proveedor</button>}
          <button type="button" className="pu-btn" disabled={saving} onClick={() => void save(true)}>{supplier ? 'Guardar igual' : 'Crear otro con el mismo nombre'}</button>
        </div>
      </div>}
      <div className="pu-formgrid">
        <label className="wide">Nombre o razón social *
          <input ref={nameInput} value={draft.name} onChange={set('name')} maxLength={160} required disabled={saving}
            aria-invalid={error?.field === 'name' || undefined} aria-describedby={error?.field === 'name' ? uid + 'name-error' : undefined} />
          {error?.field === 'name' && <span id={uid + 'name-error'} className="pu-error">{error.text}</span>}
        </label>
        <label>Teléfono<input type="tel" value={draft.phone} onChange={set('phone')} maxLength={40} disabled={saving} autoComplete="off" /></label>
        <label>Correo electrónico<input type="email" value={draft.email} onChange={set('email')} maxLength={254} disabled={saving} autoComplete="off"
          aria-invalid={error?.field === 'email' || undefined} /></label>
        <label>Persona de contacto<input value={draft.contactName} onChange={set('contactName')} maxLength={120} disabled={saving} autoComplete="off" /></label>
        <label>CUIT o identificación fiscal<input value={draft.taxId} onChange={set('taxId')} maxLength={20} disabled={saving} autoComplete="off"
          aria-invalid={error?.field === 'taxId' || undefined} aria-describedby={error?.field === 'taxId' ? uid + 'tax-error' : undefined} />
          {error?.field === 'taxId' && <span id={uid + 'tax-error'} className="pu-error">{error.text}</span>}</label>
        {error?.field === 'email' && <p className="pu-error wide" role="alert">{error.text}</p>}
        {supplier && <label className="wide">Estado
          <select value={draft.status} onChange={(event) => setDraft((value) => ({ ...value, status: event.target.value as Supplier['status'] }))} disabled={saving}>
            <option value="active">Activo: se puede elegir en pedidos nuevos</option>
            <option value="inactive">Inactivo: queda en el historial, no en pedidos nuevos</option>
          </select>
        </label>}
      </div>
      {supplier && <p className="pu-hint" style={{ marginTop: 14 }}>Los pedidos ya registrados conservan el nombre con el que se hicieron.</p>}
    </form>
    <footer>
      <button type="button" className="pu-btn" disabled={saving} onClick={onClose}>Cancelar</button>
      <button type="submit" form={uid + 'form'} className="pu-btn primary" disabled={saving}>{saving ? 'Guardando…' : supplier ? 'Guardar cambios' : 'Registrar proveedor'}</button>
    </footer>
  </dialog>;
}

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No se pudo consultar o guardar el proveedor.';
}
