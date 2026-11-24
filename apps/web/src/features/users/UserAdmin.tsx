import { useEffect, useState, type FormEvent } from 'react';
import type { ConfigurationAccess } from '../settings/Configuration';
import { api, ApiError, type Directory, type Staff, type StaffChanges, type Store } from '../../shared/api/client';

const roleLabels: Record<string, string> = {
  STORE_MANAGER: 'Encargado de sucursal',
  CASHIER: 'Cajero',
  INVENTORY_OPERATOR: 'Operador de inventario',
};

function StoreSelection({ stores, current = [], optional = false }: {
  stores: Store[];
  current?: string[];
  optional?: boolean;
}) {
  const [editing, setEditing] = useState(!optional);
  return (
    <fieldset>
      <legend>Sucursales</legend>
      {optional && (
        <label className="checkbox">
          <input type="checkbox" name="replaceStores" checked={editing} onChange={(event) => setEditing(event.target.checked)} />
          Cambiar asignaciones
        </label>
      )}
      {editing && stores.map((store) => (
        <label className="checkbox" key={store.id}>
          <input type="checkbox" name="storeIds" value={store.id} defaultChecked={current.includes(store.id)} />
          {store.name} ({store.code})
        </label>
      ))}
    </fieldset>
  );
}

export function UserAdmin({ storeRevision, onAccessChange }: { storeRevision: number; onAccessChange: (access: ConfigurationAccess) => void }) {
  const [reload, setReload] = useState(0);
  const [directory, setDirectory] = useState<Directory | null>(null);
  const [selected, setSelected] = useState<Staff | 'new' | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  function message(cause: unknown) {
    return cause instanceof Error ? cause.message : 'No pudimos completar la operación.';
  }

  useEffect(() => {
    let active = true;
    void api.users().then((result) => {
      if (!active) return;
      setDirectory(result);
      onAccessChange('allowed');
    }).catch((cause: unknown) => {
      if (!active) return;
      if (cause instanceof ApiError && cause.status === 403) {
        setDirectory(null);
        onAccessChange('denied');
      } else {
        onAccessChange('error');
        setError(message(cause));
      }
    });
    return () => { active = false; };
  }, [storeRevision, reload, onAccessChange]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !selected) return;
    const form = new FormData(event.currentTarget);
    const name = String(form.get('name')).trim();
    const storeIds = form.getAll('storeIds').map(String);
    const replaceStores = selected === 'new' || form.has('replaceStores');
    setNotice('');
    if (replaceStores && storeIds.length === 0) {
      setError('Seleccioná al menos una sucursal.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      let saved: Staff;
      if (selected === 'new') {
        saved = await api.createUser({
          name, storeIds,
          email: String(form.get('email')),
          password: String(form.get('password')),
          role: String(form.get('role')),
        });
      } else {
        const changes: StaffChanges = { name, status: form.get('status') === 'inactive' ? 'inactive' : 'active' };
        if (replaceStores) changes.storeIds = storeIds;
        const role = form.get('replacementRole');
        if (typeof role === 'string' && role) changes.role = role;
        saved = await api.updateUser(selected.id, changes);
      }
      setSelected(null);
      setNotice(selected === 'new' ? 'Usuario creado.' : 'Usuario actualizado.');
      setDirectory((current) => current && {
        ...current,
        items: [...current.items.filter((user) => user.id !== saved.id), saved]
          .sort((left, right) => left.name.localeCompare(right.name, 'es')),
      });
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  if (!directory) return error ? <div>
    <p role="alert">{error}</p>
    <button type="button" onClick={() => { setError(''); setReload((value) => value + 1); }}>Reintentar</button>
  </div> : <p role="status">Cargando usuarios…</p>;
  const existing = selected && selected !== 'new' ? selected : null;

  return (
    <section aria-labelledby="users-title">
      <h2 id="users-title">Usuarios</h2>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {selected ? (
        <form key={existing?.id ?? 'new'} onSubmit={(event) => void submit(event)} aria-label={existing ? 'Editar usuario' : 'Nuevo usuario'}>
          <h3>{existing ? 'Editar usuario' : 'Nuevo usuario'}</h3>
          <label>Nombre<input name="name" defaultValue={existing?.name} required maxLength={160} /></label>
          {existing ? (
            <label>Estado
              <select name="status" defaultValue={existing.status}>
                <option value="active">Activo</option>
                <option value="inactive">Inactivo</option>
              </select>
            </label>
          ) : (
            <>
              <label>Correo electrónico<input name="email" type="email" required maxLength={254} /></label>
              <label>Contraseña inicial<input name="password" type="password" autoComplete="new-password" minLength={12} required /></label>
              <label>Rol
                <select name="role">{directory.roles.map((role) => <option key={role} value={role}>{roleLabels[role]}</option>)}</select>
              </label>
            </>
          )}
          {existing && directory.canChangeRoles && (
            <label>Cambiar rol
              <select name="replacementRole">
                <option value="">Conservar roles actuales</option>
                {directory.roles.map((role) => <option key={role} value={role}>{roleLabels[role]}</option>)}
              </select>
            </label>
          )}
          <StoreSelection stores={directory.stores} optional={Boolean(existing)} current={existing?.storeIds} />
          <div className="actions">
            <button disabled={busy}>Guardar</button>
            <button type="button" disabled={busy} onClick={() => { setSelected(null); setError(''); }}>Cancelar</button>
          </div>
        </form>
      ) : (
        <>
          {directory.stores.length > 0 && <button type="button" onClick={() => { setNotice(''); setSelected('new'); }}>Nuevo usuario</button>}
          {directory.items.length === 0 ? <p>No hay usuarios para administrar.</p> : (
            <ul className="users-list">{directory.items.map((user) => (
              <li key={user.id}>
                <strong>{user.name}</strong>
                <p>{user.email} · {user.status === 'active' ? 'Activo' : 'Inactivo'}</p>
                <p>{user.roles.map((role) => roleLabels[role] ?? role).join(', ')}</p>
                <p>{directory.stores.filter((store) => user.storeIds.includes(store.id)).map((store) => store.name).join(', ')}</p>
                <button type="button" aria-label={`Editar a ${user.name}`} onClick={() => { setNotice(''); setSelected(user); }}>Editar</button>
              </li>
            ))}</ul>
          )}
        </>
      )}
    </section>
  );
}
