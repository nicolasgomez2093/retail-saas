import { useEffect, useState, type FormEvent } from 'react';
import type { ConfigurationAccess } from '../settings/Configuration';
import { api, ApiError, type PriceConfiguration, type Product, type Variant } from '../../shared/api/client';

export function PricingAdmin({ storeRevision, onSaved, onAccessChange }: { storeRevision: number; onSaved: () => void; onAccessChange: (access: ConfigurationAccess) => void }) {
  const [reload, setReload] = useState(0);
  const [configuration, setConfiguration] = useState<PriceConfiguration | null>(null);
  const [listId, setListId] = useState('');
  const [results, setResults] = useState<Product[]>([]);
  const [searchHasMore, setSearchHasMore] = useState(false);
  const [searched, setSearched] = useState(false);
  const [variants, setVariants] = useState<Variant[]>([]);
  const [variantId, setVariantId] = useState('');
  const [editor, setEditor] = useState<{ key: string; value: string; error?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const editorKey = listId && variantId ? listId + ':' + variantId : '';

  useEffect(() => {
    let active = true;
    void api.priceLists().then((result) => {
      if (!active) return;
      setConfiguration(result);
      onAccessChange('allowed');
    }).catch((cause: unknown) => {
      if (!active) return;
      if (cause instanceof ApiError && cause.status === 403) {
        setConfiguration(null);
        onAccessChange('denied');
      } else {
        onAccessChange('error');
        setError(message(cause));
      }
    });
    return () => { active = false; };
  }, [storeRevision, reload, onAccessChange]);

  useEffect(() => {
    if (!editorKey) return;
    let active = true;
    void api.priceItem(listId, variantId).then(({ amount }) => {
      if (active) setEditor({ key: editorKey, value: amount?.replace('.', ',') ?? '' });
    }).catch((cause: unknown) => {
      if (active) setEditor({ key: editorKey, value: '', error: message(cause) });
    });
    return () => { active = false; };
  }, [editorKey, listId, variantId]);

  function message(cause: unknown) {
    return cause instanceof Error ? cause.message : 'No pudimos completar la operación.';
  }

  async function run(operation: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await operation();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const name = String(new FormData(form).get('name')).trim();
    void run(async () => {
      const list = await api.createPriceList(name);
      setConfiguration((current) => current && {
        ...current, lists: [...current.lists, list].sort((a, b) => a.name.localeCompare(b.name, 'es')),
      });
      setListId(list.id);
      form.reset();
      setNotice('Lista creada.');
    });
  }

  function assign(event: FormEvent<HTMLFormElement>, storeId: string) {
    event.preventDefault();
    const selected = String(new FormData(event.currentTarget).get('listId')) || null;
    void run(async () => {
      await api.assignPriceList(storeId, selected);
      setConfiguration((current) => current && {
        ...current, stores: current.stores.map((store) => store.id === storeId ? { ...store, listId: selected } : store),
      });
      setNotice('Asignación guardada.');
      onSaved();
    });
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = String(new FormData(event.currentTarget).get('query')).trim();
    void run(async () => {
      const page = await api.products(query, 0);
      setResults(page.items);
      setSearchHasMore(page.hasMore);
      setSearched(true);
      setVariants([]);
      setVariantId('');
    });
  }

  function choose(productId: string) {
    void run(async () => {
      const detail = await api.product(productId);
      setVariants(detail.variants);
      setVariantId(detail.variants.length === 1 ? detail.variants[0].id : '');
      if (!detail.variants.length) setNotice('El producto todavía no tiene variantes.');
    });
  }

  function savePrice(amount: string | null) {
    void run(async () => {
      const { amount: saved } = await api.setPriceItem(listId, variantId, amount);
      setEditor({ key: editorKey, value: saved?.replace('.', ',') ?? '' });
      setNotice(saved === null ? 'Precio quitado de la lista.' : 'Precio guardado.');
      onSaved();
    });
  }

  if (!configuration) return error ? <div>
    <p role="alert">{error}</p>
    <button type="button" onClick={() => { setError(''); setReload((value) => value + 1); }}>Reintentar</button>
  </div> : <p role="status">Cargando listas de precios…</p>;
  const current = editor?.key === editorKey ? editor : null;
  return (
    <section aria-labelledby="prices-title" aria-busy={busy}>
      <h2 id="prices-title">Listas de precios</h2>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <form aria-label="Nueva lista de precios" onSubmit={create}>
        <label>Nombre de la lista<input name="name" maxLength={120} required /></label>
        <button disabled={busy}>Crear lista</button>
      </form>
      <h3>Asignación por sucursal</h3>
      {configuration.stores.length === 0 && <p>No hay sucursales activas.</p>}
      {configuration.stores.map((store) => (
        <form key={store.id} onSubmit={(event) => assign(event, store.id)} aria-label={`Asignar lista a ${store.name}`} className="price-assignment">
          <label>{store.name} ({store.code})
            <select key={store.listId ?? ''} name="listId" defaultValue={store.listId ?? ''} disabled={busy}>
              <option value="">Sin lista asignada</option>
              {configuration.lists.map((list) => <option key={list.id} value={list.id}>{list.name}</option>)}
            </select>
          </label>
          <button disabled={busy}>Guardar asignación</button>
        </form>
      ))}
      {configuration.lists.length > 0 && (
        <>
          <h3>Precio por variante</h3>
          <label>Lista para editar
            <select name="editPriceList" value={listId} disabled={busy} onChange={(event) => { setEditor(null); setListId(event.target.value); }}>
              <option value="">Seleccioná una lista</option>
              {configuration.lists.map((list) => <option key={list.id} value={list.id}>{list.name}</option>)}
            </select>
          </label>
          <form aria-label="Buscar variante para precios" onSubmit={search} className="catalog-search">
            <label>Nombre del producto o SKU<input name="query" maxLength={160} required /></label>
            <button disabled={busy}>Buscar variante</button>
          </form>
          {searched && results.length === 0 && <p>No se encontraron productos.</p>}
          {searchHasMore && <p>Acotá la búsqueda para ver los demás productos.</p>}
          {results.length > 0 && <ul className="catalog-items">{results.map((product) => (
            <li key={product.id}>
              <button type="button" className="catalog-link" disabled={busy} onClick={() => choose(product.id)}>{product.name}</button>
            </li>
          ))}</ul>}
          {variants.length > 0 && <label>Variante
            <select name="variantId" value={variantId} disabled={busy} onChange={(event) => { setEditor(null); setVariantId(event.target.value); }}>
              <option value="">Seleccioná una variante</option>
              {variants.map((variant) => (
                <option key={variant.id} value={variant.id}>{[variant.sku, variant.size, variant.color].filter(Boolean).join(' · ')}{variant.status === 'inactive' && ' (inactiva)'}</option>
              ))}
            </select>
          </label>}
          {editorKey && !current && <p role="status">Consultando precio…</p>}
          {current?.error && <p role="alert">{current.error}</p>}
          {current && !current.error && (
            <form className="price-assignment" aria-label="Editar precio" onSubmit={(event) => {
              event.preventDefault();
              savePrice(current.value.trim().replace(',', '.'));
            }}>
              <label>Importe en {configuration.currency}
                <input name="amount" inputMode="decimal" value={current.value} placeholder="Sin precio"
                  required maxLength={19} disabled={busy}
                  onChange={(event) => setEditor({ ...current, value: event.target.value })} />
              </label>
              <div className="actions">
                <button disabled={busy}>Guardar precio</button>
                <button type="button" disabled={busy} onClick={() => savePrice(null)}>Quitar precio</button>
              </div>
            </form>
          )}
        </>
      )}
    </section>
  );
}
