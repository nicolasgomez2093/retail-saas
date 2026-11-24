import { variantLabel } from './quickProduct';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError, type ExistenceDetail, type LocationPage, type PriceConfiguration, type ProductDetail, type ResolvedPrice, type Store, type Variant } from '../../shared/api/client';
import { Adjustments } from '../inventory/Adjustments';
import type { PriceAccess } from '../pricing/VariantPrice';
import { commercialStatus } from './commercialStatus';
import { posPrice } from '../pos/priceDisplay';
import { routeHref } from '../../app/routes';

type Props = {
  detail: ProductDetail; branch: Store; active: boolean; tab: string; scope: number;
  context: { scope: number }; revision: number; access: PriceAccess; recoveryScope: string;
  onBusy: (busy: boolean) => void; onChanged: () => void;
};
type Read<T> = { data?: T; error?: string; status?: number };
function readError(cause: unknown): Read<never> {
  const status = cause instanceof ApiError ? cause.status : undefined;
  return { status, error: status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' : status === 403
    ? 'No tenés permiso para esta operación.' : status === 404 ? 'Información no disponible para esta sucursal.'
    : 'No pudimos consultar este dato. Podés reintentar.' };
}
function money(value: string | null | undefined, currency = 'ARS') {
  return value == null ? 'Sin precio configurado' : currency + ' ' + posPrice(value);
}

export function CommercialProductPanels(props: Props) {
  return <>
    <div hidden={props.tab !== 'precios'}>
      <ProductPrices key={props.branch.id + ':' + props.scope} {...props} active={props.active && props.tab === 'precios'} />
    </div>
    <div hidden={props.tab !== 'mercaderia'}>
      <ProductStock key={props.branch.id + ':' + props.scope} {...props} active={props.active && props.tab === 'mercaderia'} />
    </div>
    <div hidden={props.tab !== 'resumen'}>
      <ProductSummary key={props.branch.id + ':' + props.scope} {...props} active={props.active && props.tab === 'resumen'} />
    </div>
  </>;
}

function ProductPrices({ detail, branch, active, access, revision, onChanged }: Props) {
  const [read, setRead] = useState<Read<PriceConfiguration>>();
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!active) return;
    let current = true;
    void api.priceLists().then(data => { if (current && access.isCurrent()) setRead({ data }); })
      .catch(cause => {
        if (!current || !access.isCurrent()) return;
        if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
        const failure = readError(cause);
        setRead(previous => failure.status === 401 || failure.status === 403 || failure.status === 404
          ? failure : { ...failure, data: previous?.data });
      });
    return () => { current = false; };
  }, [active, access, retry, revision]);
  const assignment = read?.data?.stores.find(store => store.id === branch.id);
  const list = read?.data?.lists.find(list => list.id === assignment?.listId);
  const shared = read?.data?.stores.filter(store => store.listId && store.listId === assignment?.listId) ?? [];
  return <section className="commercial-panel" aria-label="Configurar precios del producto">
    <header><h3>Precios por variante</h3><p>{branch.name} · {list?.name ?? 'Lista no disponible'} · {read?.data?.currency ?? 'Moneda pendiente'}</p></header>
    {read?.error && <p role="alert">{read.error} <button type="button" onClick={() => setRetry(value => value + 1)}>Reintentar configuración</button></p>}
    {!read && <p role="status">Consultando lista aplicable…</p>}
    {read?.data && !assignment?.listId && <p role="status">La sucursal no tiene una lista asignada. <a href={routeHref('configuracion/precios')}>Configurar listas de precios</a></p>}
    {shared.length > 1 && <p className="commercial-warning">Lista compartida por {shared.map(store => store.name).join(', ')}. Un cambio afecta a todas esas sucursales.</p>}
    {assignment?.listId && detail.variants.map(variant => <PriceEditor key={variant.id + ':' + assignment.listId}
      variant={variant} listId={assignment.listId!} currency={read?.data?.currency ?? ''} active={active} shared={shared.map(store => store.name)}
      access={access} configurationReady={!read?.error} onChanged={onChanged} />)}
    {!detail.variants.length && <p>Agregá variantes antes de configurar sus precios.</p>}
    <small>Cada fila se guarda por separado. El precio se valida de nuevo al confirmar una venta.</small>
  </section>;
}

function PriceEditor({ variant, listId, currency, active, shared, access, onChanged, configurationReady }: {
  variant: Variant; listId: string; currency: string; active: boolean; shared: string[]; access: PriceAccess; onChanged: () => void; configurationReady: boolean;
}) {
  const [read, setRead] = useState<Read<{ amount: string | null }>>();
  const [value, setValue] = useState('');
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [denied, setDenied] = useState(false);
  const [retry, setRetry] = useState(0);
  const guard = useRef(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const writeId = useRef(0);
  useEffect(() => () => { writeId.current++; }, []);
  useEffect(() => {
    if (!active) return;
    let current = true;
    void api.priceItem(listId, variant.id).then(data => {
      if (!current || !access.isCurrent()) return;
      setRead({ data });
      if (!dirty) setValue(data.amount?.replace('.', ',') ?? '');
    }).catch(cause => {
      if (!current || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      setRead(readError(cause));
    });
    return () => { current = false; };
  }, [active, listId, variant.id, access, retry, dirty]);

  async function save() {
    if (guard.current || denied || !configurationReady || !access.isCurrent()) return;
    const normalized = value.trim().replace(',', '.');
    if (!/^\d+(\.\d{1,2})?$/.test(normalized)) { setError('Ingresá un precio válido, con hasta dos decimales. Cero es válido.'); return; }
    guard.current = true; setBusy(true); setError(''); setNotice('');
    const generation = ++writeId.current;
    try {
      const result = await api.setPriceItem(listId, variant.id, normalized);
      if (writeId.current !== generation || !access.isCurrent()) return;
      setRead({ data: result }); setDirty(false);
      setNotice('Precio guardado.'); dialog.current?.close(); onChanged();
    } catch (cause) {
      if (writeId.current !== generation || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      if (cause instanceof ApiError && cause.status === 403) setDenied(true);
      setError(cause instanceof ApiError && cause.status >= 400 && cause.status < 500
        ? cause.message : 'No se pudo confirmar el resultado. Consultá el precio actual antes de volver a guardar.');
    } finally {
      guard.current = false;
      if (writeId.current === generation) setBusy(false);
    }
  }
  return <div className="commercial-price-row">
    <div><strong>{variantLabel(variant) || 'Sin talle ni color'}</strong><small>SKU {variant.sku} · {variant.status === 'active' ? 'Activa' : 'Inactiva'}</small></div>
    <div className="commercial-price-current"><small>Precio actual</small><strong>{read?.data ? money(read.data.amount, currency) : read?.error ? 'No disponible' : 'Consultando…'}</strong>
      <small>{read?.data ? read.data.amount === null ? 'Pendiente de configurar' : 'Configurado' : 'Lectura de la lista'}</small></div>
    <label>Nuevo precio {currency}<input inputMode="decimal" aria-label={'Precio de ' + variant.sku} value={value} disabled={busy || denied}
      onChange={event => { setDirty(true); setValue(event.target.value); setError(''); setNotice(''); }} /></label>
    <button ref={opener} type="button" disabled={busy || denied || !configurationReady || !read?.data || !dirty} onClick={() => {
      if (!/^\d+(\.\d{1,2})?$/.test(value.trim().replace(',', '.'))) { setError('Ingresá un importe válido con hasta dos decimales.'); return; }
      dialog.current?.showModal();
    }}>{busy ? 'Guardando…' : 'Guardar precio'}</button>
    {(error || read?.error) && <p role="alert" className="commercial-full">{error || read?.error}</p>}
    {notice && <p role="status" className="commercial-full">{notice}</p>}
    {(read?.error || error) && <button type="button" disabled={busy} onClick={() => setRetry(current => current + 1)}>Consultar precio actual</button>}
    <dialog ref={dialog} className="commercial-dialog" aria-label={'Confirmar precio de ' + variant.sku}
      onCancel={event => { if (busy) event.preventDefault(); }} onClose={() => opener.current?.focus()}>
      <h3>Confirmar precio</h3><p>SKU {variant.sku} · {money(value.replace(',', '.'), currency)}</p>
      <p>Lista aplicable a {shared.join(', ')}.</p>
      <p>Este cambio reemplaza el precio actual de la lista. Revisá el importe antes de confirmar.</p>
      {error && <p role="alert">{error}</p>}
      <div className="actions"><button type="button" disabled={busy} onClick={() => dialog.current?.close()}>Volver a editar</button>
        <button type="button" disabled={busy || denied || !configurationReady} onClick={() => void save()}>Confirmar precio</button></div>
    </dialog>
  </div>;
}

function ProductStock({ detail, branch, active, context, scope, revision, recoveryScope, onBusy, onChanged, access }: Props) {
  const [read, setRead] = useState<Read<LocationPage>>();
  const [retry, setRetry] = useState(0);
  const [variantId, setVariantId] = useState('');
  const selectedVariantId = detail.variants.some(item => item.id === variantId) ? variantId : detail.variants[0]?.id ?? '';
  const [locationId, setLocationId] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!active) return;
    let current = true;
    void api.locations(branch.id).then(data => {
      if (!current || context.scope !== scope || !access.isCurrent()) return;
      setRead({ data }); setLocationId(old => data.items.some(item => item.id === old) ? old : data.items[0]?.id ?? '');
    }).catch(cause => {
      if (current && context.scope === scope && access.isCurrent()) {
        if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
        const failure = readError(cause);
        setRead(previous => failure.status === 401 || failure.status === 403 || failure.status === 404
          ? failure : { ...failure, data: previous?.data });
      }
    });
    return () => { current = false; };
  }, [active, branch.id, context, scope, retry, access]);
  return <section className="commercial-panel" aria-label="Mercadería del producto">
    <header><h3>Mercadería · {branch.name}</h3><p>Registrá una operación justificada. El producto no modifica saldos por sí solo.</p></header>
    {!detail.variants.length ? <p>Creá una variante para registrar mercadería.</p> : <>
      <label>Variante del producto<select value={selectedVariantId} disabled={busy} onChange={event => setVariantId(event.target.value)}>
        {detail.variants.map(variant => <option key={variant.id} value={variant.id}>{variant.sku} · {variantLabel(variant) || 'Sin atributos'}</option>)}
      </select></label>
      {read?.error && <p role="alert">{read.error} <button type="button" onClick={() => setRetry(value => value + 1)}>Reintentar ubicaciones</button></p>}
      {!read && <p role="status">Consultando ubicaciones…</p>}
      {read?.data && <Adjustments store={branch} locations={read.data} locationId={locationId} onLocation={setLocationId}
        linkedVariant={selectedVariantId} active={active} context={context} scope={scope} revision={revision}
        permissionFresh={active && !read.error && access.isCurrent()} recoveryScope={recoveryScope}
        onBusy={value => { setBusy(value); onBusy(value); }} onChanged={onChanged} />}
    </>}
  </section>;
}

function ProductSummary({ detail, branch, active, access, revision }: Props) {
  const [read, setRead] = useState<Read<{ variant: Variant; price: Read<ResolvedPrice>; stock: Read<ExistenceDetail> }[]>>();
  const [page, setPage] = useState(0);
  const [retry, setRetry] = useState(0);
  const ids = detail.variants.slice(page * 8, page * 8 + 8);
  const signature = ids.map(item => item.id + ':' + item.revision).join(',');
  useEffect(() => {
    if (!active) return;
    let current = true; setRead(undefined);
    void Promise.all(ids.map(async variant => {
      const [price, stock] = await Promise.all([
        api.resolvePrice(branch.id, variant.id).then((data): Read<ResolvedPrice> => ({ data })).catch(readError),
        api.existence(branch.id, variant.id).then((data): Read<ExistenceDetail> => ({ data })).catch(readError),
      ]);
      return { variant, price, stock };
    })).then(data => {
      if (!current || !access.isCurrent()) return;
      if (data.some(item => item.price.status === 401 || item.stock.status === 401)) { access.onSessionExpired(); return; }
      setRead({ data });
    });
    return () => { current = false; };
    // The signature captures the visible variant set, with at most eight rows per read.
  }, [active, signature, branch.id, access, revision, retry]);
  return <section className="commercial-panel" aria-label="Preparación por variante">
    <header><h3>Preparación por variante</h3><p>{branch.name} · consulta actual, sujeta a validación en Vender</p></header>
    <div className="commercial-summary-head" aria-hidden="true"><span>Variante / SKU</span><span>Estado</span><span>Precio</span><span>Vendible</span><span>Preparación</span></div>
    {!detail.variants.length ? <p>El producto todavía no tiene variantes.</p> : !read ? <p role="status">Consultando precio y disponibilidad…</p> : read.data?.map(({ variant, price, stock }) => {
      const state = commercialStatus({
        active: detail.product.status === 'active' && variant.status === 'active',
        price: price.data?.amount, stock: stock.data?.item.sellable,
        error: Boolean(price.error || stock.error),
      });
      return <div key={variant.id} className="commercial-summary-row" aria-label={detail.product.name + ' · ' + variant.sku}>
        <div className="commercial-summary-identity"><strong>{variantLabel(variant) || 'Sin talle ni color'}</strong><small>SKU {variant.sku}</small></div>
        <span data-label="Estado" className={'products-status ' + variant.status}>{variant.status === 'active' ? 'Activa' : 'Inactiva'}</span>
        <span data-label="Precio">{price.data ? money(price.data.amount, price.data.currency) : price.error}</span>
        <span data-label="Stock vendible">{stock.data ? stock.data.item.sellable === null ? 'Sin saldo registrado' : stock.data.item.sellable + ' u.' : stock.error}</span>
        <span data-label="Preparación" className={'commercial-readiness ' + state.tone}>{state.label}</span>
      </div>;
    })}
    <div className="actions"><button type="button" onClick={() => setRetry(value => value + 1)}>Actualizar consulta</button>
      <a className="primary-link" href={routeHref('vender')}>Consultar en Vender →</a></div>
    {(page > 0 || detail.variants.length > 8) && <nav aria-label="Variantes del resumen" className="actions">
      <button type="button" disabled={page === 0} onClick={() => setPage(page - 1)}>Anterior</button><span>Página {page + 1}</span>
      <button type="button" disabled={(page + 1) * 8 >= detail.variants.length} onClick={() => setPage(page + 1)}>Siguiente</button></nav>}
    <small>“Lista para vender” describe las lecturas actuales. El backend vuelve a validar permisos, caja, precio y disponibilidad al confirmar la venta.</small>
  </section>;
}
