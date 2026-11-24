import { useEffect, useState, type FormEvent } from 'react';
import type { ConfigurationAccess } from '../settings/Configuration';
import { api, ApiError, type ManagedStore } from '../../shared/api/client';

export function StoreAdmin({ onSaved, onAccessChange }: { onSaved: () => void; onAccessChange: (access: ConfigurationAccess) => void }) {
  const [reload, setReload] = useState(0);
  const [stores, setStores] = useState<ManagedStore[] | null>(null);
  const [selected, setSelected] = useState<ManagedStore | 'new' | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  useEffect(() => {
    let active = true;
    void api.managedStores().then((result) => {
      if (!active) return;
      setStores(result.items);
      onAccessChange('allowed');
    }).catch((cause: unknown) => {
      if (!active) return;
      if (cause instanceof ApiError && cause.status === 403) {
        setStores(null);
        onAccessChange('denied');
      } else {
        onAccessChange('error');
        setError(cause instanceof Error ? cause.message : 'No pudimos cargar las sucursales.');
      }
    });
    return () => { active = false; };
  }, [reload, onAccessChange]);

  function openForm(store: ManagedStore | 'new' | null) {
    setError('');
    setNotice('');
    setSelected(store);
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !selected) return;
    const form = new FormData(event.currentTarget);
    const data = { code: String(form.get('code')), name: String(form.get('name')), address: String(form.get('address')) };
    setBusy(true);
    setError('');
    try {
      const saved = selected === 'new'
        ? await api.createStore(data)
        : await api.updateStore(selected.id, { ...data, status: form.get('status') === 'inactive' ? 'inactive' : 'active' });
      setStores((current) => [...(current ?? []).filter((store) => store.id !== saved.id), saved]
        .sort((left, right) => left.code.localeCompare(right.code, 'es')));
      setSelected(null);
      setNotice(selected === 'new' ? 'Sucursal creada.' : 'Sucursal actualizada.');
      onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos guardar la sucursal.');
    } finally {
      setBusy(false);
    }
  }

  if (!stores) return error ? <div>
    <p role="alert">{error}</p>
    <button type="button" onClick={() => { setError(''); setReload((value) => value + 1); }}>Reintentar</button>
  </div> : <p role="status">Cargando sucursales…</p>;
  const existing = selected && selected !== 'new' ? selected : null;

  return (
    <section aria-labelledby="store-admin-title">
      <h2 id="store-admin-title">Administrar sucursales</h2>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {selected ? (
        <form key={existing?.id ?? 'new'} onSubmit={(event) => void save(event)} aria-label={existing ? 'Editar sucursal' : 'Nueva sucursal'}>
          <label>Código<input name="code" defaultValue={existing?.code} required maxLength={40} /></label>
          <label>Nombre<input name="name" defaultValue={existing?.name} required maxLength={160} /></label>
          <label>Domicilio<input name="address" defaultValue={existing?.address} maxLength={300} /></label>
          {existing && (
            <label>Estado
              <select name="status" defaultValue={existing.status}>
                <option value="active">Activa</option>
                <option value="inactive">Inactiva</option>
              </select>
            </label>
          )}
          <div className="actions">
            <button disabled={busy}>Guardar</button>
            <button type="button" disabled={busy} onClick={() => openForm(null)}>Cancelar</button>
          </div>
        </form>
      ) : (
        <>
          <button type="button" onClick={() => openForm('new')}>Nueva sucursal</button>
          <ul className="users-list">{stores.map((store) => (
            <li key={store.id}>
              <strong>{store.name}</strong>
              <p>{store.code} · {store.status === 'active' ? 'Activa' : 'Inactiva'}</p>
              {store.address && <p>{store.address}</p>}
              <button type="button" aria-label={`Editar sucursal ${store.name}`} onClick={() => openForm(store)}>Editar</button>
            </li>
          ))}</ul>
        </>
      )}
    </section>
  );
}
