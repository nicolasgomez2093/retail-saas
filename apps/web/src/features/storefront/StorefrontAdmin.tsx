import { useEffect, useId, useMemo, useState, type FormEvent } from 'react';

import { api, ApiError, type StorefrontAdmin as Admin, type StorefrontAdminProduct, type StorefrontProductStatus, type StorefrontSettings } from '../../shared/api/client';
import { formatAmount } from '../catalog/quickProduct';
import type { ConfigurationAccess } from '../settings/Configuration';
import '../purchases/purchases.css';
import './storefront-admin.css';

const statusText: Record<StorefrontProductStatus, { label: string; tone: string; hint: string }> = {
  published: { label: 'Visible', tone: '', hint: 'Se ve en la tienda.' },
  sold_out: { label: 'Agotado', tone: 'warning', hint: 'Sin stock vendible en la sucursal.' },
  hidden: { label: 'Oculto', tone: 'muted', hint: 'Lo ocultaste de la tienda.' },
  no_photo: { label: 'Sin foto', tone: 'danger', hint: 'Se publica cuando tenga una foto.' },
  no_price: { label: 'Sin precio', tone: 'danger', hint: 'Cargale precio en la lista de la sucursal.' },
};

type Filter = 'all' | StorefrontProductStatus;
const filters: [Filter, string][] = [['all', 'Todos'], ['published', 'Visibles'], ['sold_out', 'Agotados'], ['hidden', 'Ocultos'], ['no_photo', 'Sin foto'], ['no_price', 'Sin precio']];

export function storefrontURL(slug: string) {
  return window.location.origin + '/tienda/' + slug;
}

function priceRange(currency: string, product: StorefrontAdminProduct) {
  if (!product.priceFrom || !product.priceTo) return '';
  const from = currency + ' ' + formatAmount(product.priceFrom);
  return product.priceFrom === product.priceTo ? from : 'Desde ' + from;
}

export function StorefrontAdmin({ storeRevision, onAccessChange }: { storeRevision: number; onAccessChange: (access: ConfigurationAccess) => void }) {
  const uid = useId();
  const [data, setData] = useState<Admin | null>(null);
  const [form, setForm] = useState<StorefrontSettings | null>(null);
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<string[]>([]);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('all');

  useEffect(() => {
    let active = true;
    void api.storefront().then((result) => {
      if (!active) return;
      setData(result);
      setForm((current) => current ?? settingsOf(result));
      onAccessChange('allowed');
    }).catch((cause: unknown) => {
      if (!active) return;
      if (cause instanceof ApiError && cause.status === 403) onAccessChange('denied');
      else { onAccessChange('error'); setError(message(cause)); }
    });
    return () => { active = false; };
  }, [storeRevision, reload, onAccessChange]);

  const visibleProducts = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase('es-AR');
    return (data?.products ?? []).filter((product) => (filter === 'all' || product.status === filter) &&
      (!needle || product.name.toLocaleLowerCase('es-AR').includes(needle) || (product.category ?? '').toLocaleLowerCase('es-AR').includes(needle)));
  }, [data, query, filter]);

  if (!data || !form) return <section className="pu sf-admin" aria-busy={!error}>
    {error ? <p className="pu-alert" role="alert">{error} <button type="button" className="pu-link" onClick={() => { setError(''); setReload((n) => n + 1); }}>Reintentar</button></p>
      : <p role="status">Cargando la tienda online…</p>}
  </section>;

  const url = storefrontURL(data.slug);
  const dirty = JSON.stringify(form) !== JSON.stringify(settingsOf(data));
  const counts: Record<Filter, number> = {
    all: data.products.length, published: 0, sold_out: 0, hidden: 0, no_photo: 0, no_price: 0,
  };
  for (const product of data.products) counts[product.status]++;

  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy || !form) return;
    setBusy(true); setNotice(''); setError('');
    try {
      const result = await api.saveStorefront(form);
      setData(result);
      setForm(settingsOf(result));
      setNotice(result.enabled ? 'Cambios guardados. La tienda está publicada.' : 'Cambios guardados. La tienda no está publicada.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  async function toggle(product: StorefrontAdminProduct) {
    if (pending.includes(product.id)) return;
    setPending((current) => [...current, product.id]); setNotice(''); setError('');
    try {
      await api.setStorefrontHidden(product.id, !product.hidden);
      const fresh = await api.storefront();
      setData(fresh);
      setNotice(product.hidden ? '«' + product.name + '» vuelve a la tienda.' : '«' + product.name + '» quedó oculto de la tienda.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setPending((current) => current.filter((id) => id !== product.id));
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setError('No pudimos copiar el enlace. Seleccionalo y copialo a mano.');
    }
  }

  const set = <K extends keyof StorefrontSettings>(key: K, value: StorefrontSettings[K]) => {
    setForm((current) => current && { ...current, [key]: value });
    setNotice('');
  };

  return <section className="pu sf-admin" aria-label="Tienda online">
    <header className="pu-head sf-head">
      <div>
        <p>Publicá tu catálogo en una página para tus clientes. Arman el carrito y te mandan el pedido por WhatsApp.</p>
      </div>
    </header>

    <div className="sf-status" data-enabled={data.enabled || undefined}>
      <div className="sf-status-main">
        <span className="sf-status-dot" aria-hidden="true" />
        <div>
          <strong>{data.enabled ? 'Tu tienda está publicada' : 'Tu tienda todavía no está publicada'}</strong>
          <span>{data.enabled ? 'Cualquiera con el enlace puede ver tus productos y armar un pedido.'
            : 'Completá la sucursal y el WhatsApp, activá «Publicar la tienda» y guardá.'}</span>
        </div>
      </div>
      <div className="sf-link">
        <code title={url}>{url.replace(/^https?:\/\//, '')}</code>
        <button type="button" className="pu-btn" onClick={() => void copy()}>{copied ? '✓ Copiado' : 'Copiar enlace'}</button>
        {data.enabled && <a className="pu-btn primary" href={url} target="_blank" rel="noopener">Ver tienda ↗</a>}
      </div>
    </div>

    <form className="sf-card" onSubmit={(event) => void save(event)} aria-labelledby={uid + 'settings'}>
      <h3 id={uid + 'settings'}>Datos de la tienda</h3>
      <div className="pu-formgrid sf-grid">
        <label>Nombre que ven tus clientes
          <input value={form.title} maxLength={80} placeholder={data.commerceName} disabled={busy} onChange={(event) => set('title', event.target.value)} />
          <small className="pu-hint">Si lo dejás vacío se usa «{data.commerceName}».</small>
        </label>
        <label>WhatsApp del local
          <input value={form.whatsapp} inputMode="tel" autoComplete="tel" placeholder="Ej. 54 9 11 5555-6666" disabled={busy}
            onChange={(event) => set('whatsapp', event.target.value)} />
          <small className="pu-hint">Con código de país y de área, sin el 0 ni el 15. Podés usar cualquier número: ahí llegan los pedidos.
            {/^[1-9]\d{7,14}$/.test(form.whatsapp.replace(/\D/g, '')) && <> <a className="sf-try" href={'https://wa.me/' + form.whatsapp.replace(/\D/g, '')}
              target="_blank" rel="noopener">Probar este número ↗</a></>}</small>
        </label>
        <label>Sucursal que publica
          <select value={form.storeId ?? ''} disabled={busy} onChange={(event) => set('storeId', event.target.value || null)}>
            <option value="">Elegí una sucursal</option>
            {data.stores.map((store) => <option key={store.id} value={store.id}>{store.name}</option>)}
          </select>
          <small className="pu-hint">La tienda muestra los precios y el stock de esta sucursal.</small>
        </label>
        <label className="sf-wide">Mensaje de bienvenida
          <textarea value={form.welcome} maxLength={300} rows={2} placeholder="Ej. Envíos en el día en CABA · Retirá en el local" disabled={busy}
            onChange={(event) => set('welcome', event.target.value)} />
          <small className="pu-hint">{form.welcome.length}/300 · Aparece arriba del catálogo.</small>
        </label>
      </div>
      <Appearance data={data} accent={form.accent} busy={busy} onAccent={(value) => set('accent', value)}
        onLogo={(result) => { setData(result); setNotice(result.logo ? 'Logo actualizado.' : 'Quitaste el logo.'); }} />
      <div className="sf-switches">
        <label className="sf-switch">
          <input type="checkbox" role="switch" checked={form.showSoldOut} disabled={busy} onChange={(event) => set('showSoldOut', event.target.checked)} />
          <span><b>Mostrar productos agotados</b><small>Aparecen con la etiqueta «Agotado». Si lo apagás, se ocultan hasta que vuelva el stock.</small></span>
        </label>
        <label className="sf-switch sf-switch-main">
          <input type="checkbox" role="switch" checked={form.enabled} disabled={busy} onChange={(event) => set('enabled', event.target.checked)} />
          <span><b>Publicar la tienda</b><small>Apagado, el enlace muestra que la tienda no está disponible.</small></span>
        </label>
      </div>
      {error && <p className="pu-alert" role="alert">{error}</p>}
      {notice && <p className="pu-feedback" role="status">✓ {notice}</p>}
      <div className="sf-actions">
        {dirty && <button type="button" className="pu-btn" disabled={busy} onClick={() => { setForm(settingsOf(data)); setError(''); }}>Descartar</button>}
        <button type="submit" className="pu-btn primary" disabled={busy || !dirty}>{busy ? 'Guardando…' : 'Guardar cambios'}</button>
      </div>
    </form>

    <section className="sf-card" aria-labelledby={uid + 'products'}>
      <div className="sf-products-head">
        <div>
          <h3 id={uid + 'products'}>Productos en la tienda</h3>
          <p className="pu-hint">Se publica cada producto activo con foto y precio, con todos sus talles y colores en una sola ficha. Ocultá los que no quieras mostrar.</p>
        </div>
        <div className="sf-count" aria-label="Productos visibles en la tienda"><strong>{data.summary.visible}</strong><span>visibles</span></div>
      </div>
      <div className="sf-tools">
        <label className="pu-search sf-search">
          <span aria-hidden="true">⌕</span>
          <input type="search" value={query} placeholder="Buscar producto o categoría" aria-label="Buscar producto o categoría" onChange={(event) => setQuery(event.target.value)} />
        </label>
        <div className="sf-filters" role="group" aria-label="Filtrar por estado">
          {filters.filter(([id]) => id === 'all' || counts[id] > 0).map(([id, label]) =>
            <button key={id} type="button" aria-pressed={filter === id} onClick={() => setFilter(id)}>{label} <span>{counts[id]}</span></button>)}
        </div>
      </div>
      {data.summary.truncated > 0 && <p className="pu-note warn">Se muestran los primeros 2.000 productos. Usá el buscador para encontrar el resto.</p>}
      {visibleProducts.length === 0 ? <p className="pu-hint sf-empty">{data.products.length ? 'No hay productos con ese filtro.' : 'Todavía no cargaste productos activos.'}</p> :
        <ul className="sf-products">{visibleProducts.map((product) => {
          const status = statusText[product.status];
          const busyRow = pending.includes(product.id);
          return <li key={product.id} data-status={product.status}>
            <span className="sf-thumb">{product.imageId
              ? <img src={api.productPhotoURL(product.id, product.imageId)} alt="" loading="lazy" decoding="async" />
              : <span aria-hidden="true">Sin foto</span>}</span>
            <span className="sf-product-main">
              <b>{product.name}</b>
              <small>{[product.category, priceRange(data.currency, product), product.variants > 1 ? product.variants + ' variantes' : ''].filter(Boolean).join(' · ') || 'Sin precio en la sucursal'}</small>
            </span>
            <span className="sf-product-state">
              <span className={'pu-pill ' + status.tone}>{status.label}</span>
              <small>{status.hint}{product.status === 'no_photo' && <> <a href={'#/productos?q=' + encodeURIComponent(product.name)}>Cargar foto</a></>}</small>
            </span>
            <label className="sf-switch sf-row-switch" title={product.hidden ? 'Mostrar en la tienda' : 'Ocultar de la tienda'}>
              <input type="checkbox" role="switch" checked={!product.hidden} disabled={busyRow} onChange={() => void toggle(product)}
                aria-label={(product.hidden ? 'Mostrar ' : 'Ocultar ') + product.name + ' en la tienda'} />
              <span aria-hidden="true">{product.hidden ? 'Oculto' : 'En tienda'}</span>
            </label>
          </li>;
        })}</ul>}
    </section>
  </section>;
}

function settingsOf(data: Admin): StorefrontSettings {
  return { enabled: data.enabled, storeId: data.storeId ?? (data.stores.length === 1 ? data.stores[0].id : null), whatsapp: data.whatsapp,
    title: data.title, welcome: data.welcome, showSoldOut: data.showSoldOut, accent: data.accent };
}

function message(cause: unknown) {
  if (cause instanceof ApiError && cause.code === 'storefront_incomplete') return 'Para publicar la tienda elegí la sucursal y cargá el WhatsApp del local.';
  if (cause instanceof ApiError && cause.status === 400) return 'Revisá los datos: el WhatsApp va con código de país y de área (por ejemplo 54 9 11 5555-6666).';
  return cause instanceof Error ? cause.message : 'No pudimos completar la operación.';
}

const accents: [string, string][] = [
  ['', 'Verde (original)'], ['#1d4ed8', 'Azul'], ['#0f766e', 'Turquesa'], ['#7c3aed', 'Violeta'],
  ['#be185d', 'Fucsia'], ['#c2410c', 'Naranja'], ['#b91c1c', 'Rojo'], ['#111827', 'Negro'],
];

/** Logo (uploaded at once) and main color (saved with the form), with a small live preview of the page header. */
function Appearance({ data, accent, busy, onAccent, onLogo }: {
  data: Admin; accent: string; busy: boolean; onAccent: (value: string) => void; onLogo: (result: Admin) => void;
}) {
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const shown = accent || '#1f5c48';
  const name = data.title || data.commerceName;

  async function upload(file: File | undefined) {
    if (!file) return;
    if (!['image/png', 'image/jpeg'].includes(file.type)) { setError('Elegí una imagen PNG o JPG.'); return; }
    if (file.size > 4 * 1024 * 1024) { setError('El logo tiene que pesar hasta 4 MB.'); return; }
    setUploading(true); setError('');
    try { onLogo(await api.uploadStorefrontLogo(file)); } catch (cause) { setError(message(cause)); } finally { setUploading(false); }
  }
  async function remove() {
    setUploading(true); setError('');
    try { onLogo(await api.removeStorefrontLogo()); } catch (cause) { setError(message(cause)); } finally { setUploading(false); }
  }

  return <fieldset className="sf-appearance" disabled={busy || uploading}>
    <legend>Apariencia</legend>
    <div className="sf-appearance-grid">
      <div className="sf-logo-field">
        <span className="sf-field-label">Logo</span>
        <div className="sf-logo-row">
          <span className="sf-logo-box">{data.logo ? <img src={api.storefrontLogoURL(data.logo)} alt="Logo actual" /> : <small>Sin logo</small>}</span>
          <div className="sf-logo-actions">
            <label className="pu-btn sf-file">{uploading ? 'Subiendo…' : data.logo ? 'Cambiar logo' : 'Subir logo'}
              <input type="file" accept="image/png,image/jpeg" onChange={(event) => { void upload(event.target.files?.[0]); event.target.value = ''; }} />
            </label>
            {data.logo > 0 && <button type="button" className="pu-btn" onClick={() => void remove()}>Quitar</button>}
          </div>
        </div>
        <small className="pu-hint">PNG o JPG. Mejor cuadrado y con fondo transparente. Se guarda al elegirlo.</small>
      </div>
      <div className="sf-color-field">
        <span className="sf-field-label">Color principal</span>
        <div className="sf-swatches" role="radiogroup" aria-label="Color principal">
          {accents.map(([value, label]) => <button key={label} type="button" role="radio" aria-checked={accent === value} title={label} aria-label={label}
            style={{ background: value || '#1f5c48' }} onClick={() => onAccent(value)} />)}
          <label className="sf-custom-color" title="Otro color">
            <input type="color" aria-label="Elegir otro color" value={shown} onChange={(event) => onAccent(event.target.value)} />
            <span>Otro</span>
          </label>
        </div>
        <small className="pu-hint">Se usa en la portada y en los botones. Se guarda con «Guardar cambios».</small>
      </div>
    </div>
    <div className="sf-preview" aria-label="Vista previa" style={{ background: shown, color: accentText(shown) }}>
      {data.logo ? <span className="sf-preview-logo"><img src={api.storefrontLogoURL(data.logo)} alt="" /></span> : null}
      <strong>{name}</strong>
      <span className="sf-preview-button" style={{ background: accentText(shown), color: shown }}>Agregar al carrito</span>
    </div>
    {error && <p className="pu-alert" role="alert">{error}</p>}
  </fieldset>;
}

// Same rule as the public page: white or dark text, whichever reads better on the chosen color.
function accentText(hex: string) {
  const match = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  if (!match) return '#fff';
  const [r, g, b] = match.slice(1).map((part) => { const c = parseInt(part, 16) / 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; });
  const luminance = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  return 1.05 / (luminance + 0.05) >= (luminance + 0.05) / 0.0711 ? '#fff' : '#1c2321';
}
