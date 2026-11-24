import '../purchases/purchases.css';
import './existences.css';
import { LabelDialog } from '../catalog/LabelDialog';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { routeHref } from '../../app/routes';
import { api, ApiError, type ExistenceAccess, type ExistenceDetail, type ExistencePage, type ExistenceRow, type LocationPage, type StockLocation, type Store, type Term } from '../../shared/api/client';
import { BarcodeScanner } from '../catalog/BarcodeScanner';
import { existenceDefaults, existenceSearch, existencesHref, isExistencesHash, lastExistencesPage, readExistencesHash, type ExistenceQuery } from './existencesQuery';

type Context = { scope: number };
type Result<T> = { key: string; data?: T; error?: string; status?: number };

function useExistenceRead<T>(
  key: string, enabled: boolean, context: Context, scope: number,
  read: (signal: AbortSignal) => Promise<T>, currentIntent: () => boolean,
) {
  const [attempt, setAttempt] = useState(0);
  const revision = useRef(0);
  const request = key + ':' + scope + ':' + attempt;
  const [result, setResult] = useState<Result<T>>();
  useEffect(() => {
    if (!enabled || context.scope !== scope || !currentIntent()) return;
    const controller = new AbortController();
    const generation = ++revision.current;
    const current = () => !controller.signal.aborted && generation === revision.current && context.scope === scope && currentIntent();
    void read(controller.signal).then(data => {
      if (current()) setResult({ key: request, data });
    }).catch((cause: unknown) => {
      if (!current()) return;
      const status = cause instanceof ApiError ? cause.status : undefined;
      const error = status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' :
        status === 403 ? 'No tenés permiso para realizar esta consulta.' :
        status === 404 ? 'La sucursal, ubicación o variante ya no está disponible para esta consulta.' :
        cause instanceof Error ? cause.message : 'No se pudo completar la consulta.';
      setResult({ key: request, status, error });
    });
    return () => controller.abort();
  }, [request, enabled, context, scope, read, currentIntent]);
  return { result: enabled && result?.key === request ? result : undefined,
    retry: () => { revision.current++; setAttempt(value => value + 1); } };
}

const onlyExistences = () => isExistencesHash(window.location.hash);
const readCategories = (signal: AbortSignal) => api.classificationTerms('categories', signal);
const readBrands = (signal: AbortSignal) => api.classificationTerms('brands', signal);

function closeDisclosureOnEscape(event: KeyboardEvent<HTMLDetailsElement>) {
  if (event.key !== 'Escape' || !event.currentTarget.open) return;
  event.preventDefault();
  event.currentTarget.open = false;
  event.currentTarget.querySelector('summary')?.focus();
}

function OperationLinks({ access, hasLocations, location, sku, variantId }: { access: ExistenceAccess; hasLocations: boolean; location?: string; sku?: string; variantId?: string }) {
  const params = location ? new URLSearchParams({ ubicacion: location }) : undefined;
  const movementParams = new URLSearchParams(params);
  const adjustmentParams = new URLSearchParams(params);
  if (variantId) {
    movementParams.set('variante', variantId);
    adjustmentParams.set('variante', variantId);
  }
  if (!hasLocations && !access.canManageLocations && !access.canCatalog && !variantId) return null;
  return <details className="existences-operations" onKeyDown={closeDisclosureOnEscape}>
    <summary>Acciones</summary>
    <nav className="existences-links" aria-label="Operaciones disponibles">
      {(hasLocations || variantId) && <a href={routeHref('stock/movimientos', movementParams)}>Movimientos</a>}
      {(access.canManageLocations || hasLocations && (access.canAdjust || access.canManageMinimums)) &&
        <a href={routeHref('stock/ajustes', adjustmentParams)}>Ajustes y mínimos</a>}
      {hasLocations && access.canAdjust && <a href={routeHref('stock/conteos', params)}>Conteos</a>}
      {hasLocations && access.canTransfer && <a href={routeHref('stock/transferencias')}>Transferencias</a>}
      {access.canCatalog && <a href={routeHref('productos', sku ? new URLSearchParams({ q: sku }) : undefined)}>Catálogo</a>}
    </nav>
  </details>;
}

function FilterForm({ filters, categories, brands, locations, categoryError, brandError, onApply }: {
  filters: ExistenceQuery; categories?: Term[]; brands?: Term[]; locations?: StockLocation[]; categoryError?: boolean; brandError?: boolean;
  onApply: (filters: ExistenceQuery) => void;
}) {
  const [draft, setDraft] = useState(filters);
  useEffect(() => setDraft(filters), [filters]);
  const advancedCount = [draft.categoryId, draft.brandId, draft.producto, draft.variante, draft.bajo].filter(Boolean).length;
  const update = (field: keyof ExistenceQuery, value: string) => setDraft(old => ({ ...old, [field]: value }));
  function submit(event: FormEvent) {
    event.preventDefault();
    onApply({ ...draft, page: '1', detalle: '' });
  }
  function term(field: 'categoryId' | 'brandId', label: string, items?: Term[]) {
    return <label>{label}<select value={draft[field]} onChange={event => update(field, event.target.value)}>
      <option value="">{(field === 'categoryId' ? categoryError : brandError) ? label + ' no disponible' : items ? items.length ? 'Todas' : field === 'categoryId' ? 'Sin categorías' : 'Sin marcas' : 'Consultando…'}</option>
      {draft[field] && !items?.some(item => item.id === draft[field]) && <option value={draft[field]}>Filtro actual</option>}
      {items?.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
    </select></label>;
  }
  return <form aria-label="Filtros de existencias" className="existences-filters" onSubmit={submit}>
    <label className="existences-search">Producto o SKU
      <input name="q" type="search" value={draft.q} onChange={event => update('q', event.target.value)} placeholder="Nombre o SKU" />
    </label>
    <label>Ubicación
      <select name="ubicacion" value={draft.ubicacion} onChange={event => update('ubicacion', event.target.value)}>
        <option value="">Todas</option>
        {draft.ubicacion && !locations?.some(item => item.id === draft.ubicacion) && <option value={draft.ubicacion}>Ubicación seleccionada</option>}
        {locations?.map(item => <option key={item.id} value={item.id}>{item.name} · {item.isSellable ? 'Vendible' : 'No vendible'}</option>)}
      </select>
    </label>
    <label>Disponibilidad<select value={draft.saldo} onChange={event => update('saldo', event.target.value)}>
      <option value="">Todas</option><option value="positive">Con stock vendible</option>
      <option value="zero">Vendible cero</option><option value="unrecorded">Sin saldo registrado</option>
    </select></label>
    <details className="existences-extra" onKeyDown={closeDisclosureOnEscape}>
      <summary>Más filtros{advancedCount ? ` (${advancedCount})` : ''}</summary>
      <div className="existences-filters">
        {term('categoryId', 'Categoría', categories)}
        {term('brandId', 'Marca', brands)}
        {(['producto', 'variante'] as const).map(field => <label key={field}>Estado {field === 'producto' ? 'del producto' : 'de la variante'}
          <select value={draft[field]} onChange={event => update(field, event.target.value)}>
            <option value="">Todos</option><option value="active">Activo</option><option value="inactive">Inactivo</option>
          </select>
        </label>)}
        <label className="existences-check"><input type="checkbox" name="lowOnly" checked={draft.bajo === '1'} onChange={event => update('bajo', event.target.checked ? '1' : '')} />
          Solo alertas de mínimo
        </label>
      </div>
    </details>
    <button className="existences-apply">Aplicar filtros</button>
  </form>;
}

function Summary({ summary }: Pick<ExistencePage, 'summary'>) {
  const items: [string, number][] = [
    ['Productos con stock físico', summary.productsWithStock], ['Variantes con vendible', summary.variantsSellable],
    ['Vendible en cero', summary.variantsZero], ['Variantes sin saldo', summary.variantsUnrecorded],
    ['Alertas de mínimo', summary.variantsLow],
  ];
  return <dl className="existences-summary" aria-label="Resumen de los resultados filtrados">
    {items.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value.toLocaleString('es-AR')}</dd></div>)}
  </dl>;
}

function Quantity({ value }: { value: number | null }) {
  return value === null ? <span>Sin saldo registrado</span> : <>{value.toLocaleString('es-AR')}</>;
}

// One state per row, like the approved design: inactive first, then stock level.
function StockState({ row }: { row: ExistenceRow }) {
  if (row.variantStatus === 'inactive') return <span className="pu-pill muted">Inactiva</span>;
  if (row.sellable === null || row.sellable === undefined) return <span className="pu-pill muted">Sin dato</span>;
  if (row.sellable <= 0) return <span className="pu-pill danger">Agotado</span>;
  if (row.lowStock) return <span className="pu-pill warning">Stock bajo<span className="pos-sr-only">: alerta de mínimo</span></span>;
  return <span className="pu-pill">Disponible</span>;
}

function StockTable({ rows, filters }: { rows: ExistenceRow[]; filters: ExistenceQuery }) {
  const [collapsed, setCollapsed] = useState<string[]>([]);
  const groups = new Map<string, ExistenceRow[]>();
  for (const row of rows) {
    const group = groups.get(row.productId);
    if (group) group.push(row); else groups.set(row.productId, [row]);
  }
  return <div className="existences-table">
    <table role="table">
      <thead role="rowgroup"><tr role="row">
        {['Variante / SKU', 'Físico', 'Reservado', 'Vendible', 'Ubicaciones', 'Estado', 'Detalle'].map(label =>
          <th key={label} role="columnheader" scope="col" aria-label={label === 'Ubicaciones' ? 'Ubicaciones con saldo registrado' : undefined}>{label}</th>)}
      </tr></thead>
      {[...groups].map(([id, entries]) => {
        const product = entries[0];
        const expanded = !collapsed.includes(id);
        return <tbody key={id} role="rowgroup">
          <tr className="existences-group" role="row">
            <th role="rowheader" colSpan={7} scope="rowgroup">
              {entries.length > 1 ? <button type="button" className="catalog-link" aria-expanded={expanded}
                onClick={() => setCollapsed(old => expanded ? [...old, id] : old.filter(item => item !== id))}>
                {product.product}<span className="pos-sr-only"> · variantes en esta página</span>
              </button> : <strong>{product.product}</strong>}
              {product.productStatus === 'inactive' ? <small>Producto inactivo</small>
                : <small className="pos-sr-only">Producto activo</small>}
              <span className="existences-group-count" aria-hidden="true">{entries.length === 1 ? '1 variante' : entries.length + ' variantes'}</span>
            </th>
          </tr>
          {expanded && entries.map(row => row.variantId ? <tr key={row.variantId} className="existences-variant" role="row">
            <th role="rowheader" scope="row">{row.sku}<small>Talle {row.size || '—'} · Color {row.color || '—'}</small></th>
            <td role="cell" data-label="Físico">{row.onHand ?? '—'}</td>
            <td role="cell" data-label="Reservado">{row.reserved ?? '—'}</td>
            <td role="cell" data-label="Vendible" className="existences-amount"><Quantity value={row.sellable} /></td>
            <td role="cell" data-label="Ubicaciones con saldo">{row.recordedLocations}</td>
            <td role="cell" className="existences-variant-state" data-label="Estado"><StockState row={row} /></td>
            <td role="cell" className="existences-variant-action"><a id={'existencia-' + row.variantId} className="existences-detail-link" href={existencesHref({ ...filters, detalle: row.variantId })}
              aria-label={'Ver ubicaciones de ' + row.sku}>Ver detalle</a></td>
          </tr> : <tr key={id + '-empty'} className="existences-no-variants" role="row"><td role="cell" colSpan={7}>Producto sin variantes</td></tr>)}
        </tbody>;
      })}
    </table>
  </div>;
}

type DetailLocation = ExistenceDetail['locations'][number];

function hasUnits(location: DetailLocation) {
  return [location.onHand, location.reserved, location.sellable].some(value => value !== null && value !== 0);
}

function LocationList({ locations, compact = false, adjustVariant }: { locations: DetailLocation[]; compact?: boolean; adjustVariant?: string }) {
  return <ul className="existences-locations">{locations.map(location => <li key={location.id}>
    <div className="existences-location-heading">
      <div><strong>{location.name}</strong><small>{location.code}</small></div>
      <span className="existences-location-status" data-sellable={location.active && location.isSellable}>
        {location.active ? location.isSellable ? 'Vendible' : 'No vendible' : 'Inactiva'}
      </span>
    </div>
    {!compact && <>
      <dl>
        <div><dt>Físico</dt><dd>{location.onHand ?? '—'}</dd></div>
        <div><dt>Reservado</dt><dd>{location.reserved ?? '—'}</dd></div>
        <div><dt>Vendible</dt><dd><Quantity value={location.sellable} /></dd></div>
        <div><dt>Mínimo</dt><dd>{location.minimum ?? '—'}</dd></div>
      </dl>
      {location.lowStock && <small className="inventory-low-stock">Alerta de mínimo</small>}
    </>}
    {adjustVariant && location.active && <a className="existences-detail-link" href={routeHref('stock/ajustes',
      new URLSearchParams({ ubicacion: location.id, variante: adjustVariant }))}>Ajustar en esta ubicación</a>}
  </li>)}</ul>;
}

function StockDetail({ data, branch }: { data: ExistenceDetail; branch: Store }) {
  const { item, locations } = data;
  const [label, setLabel] = useState<Parameters<typeof LabelDialog>[0]['data']>();
  const [labelBusy, setLabelBusy] = useState(false);
  const [labelError, setLabelError] = useState('');
  const labelOrigin = useRef<HTMLButtonElement>(null);
  const labelRequest = useRef(0);
  useLayoutEffect(() => () => { labelRequest.current++; }, []);

  async function openLabel(opener: HTMLButtonElement) {
    if (labelBusy || !data.access.canCatalog || !item.variantId) return;
    const request = ++labelRequest.current;
    labelOrigin.current = opener;
    setLabelBusy(true);
    setLabelError('');
    try {
      const detail = await api.product(item.productId);
      if (request !== labelRequest.current) return;
      const variant = detail.variants.find(entry => entry.id === item.variantId && entry.status === 'active');
      if (!variant || detail.product.status !== 'active') {
        setLabelError('La variante ya no está disponible para etiquetar.');
        return;
      }
      setLabel({ product: detail.product, variant });
    } catch {
      if (request === labelRequest.current) setLabelError('No pudimos consultar la variante para etiquetar.');
    } finally {
      if (request === labelRequest.current) setLabelBusy(false);
    }
  }

  const adjustVariant = data.access.canAdjust && item.productStatus === 'active' && item.variantStatus === 'active' ? item.variantId ?? undefined : undefined;
  const withUnits: DetailLocation[] = [];
  const withRecords: DetailLocation[] = [];
  const unrecorded: DetailLocation[] = [];
  for (const location of locations) {
    if (hasUnits(location)) withUnits.push(location);
    else if (location.onHand !== null || location.reserved !== null || location.sellable !== null ||
      location.minimum !== null || location.lowStock || !location.active) withRecords.push(location);
    else unrecorded.push(location);
  }
  withUnits.sort((a, b) => Number((b.sellable ?? 0) > 0) - Number((a.sellable ?? 0) > 0));
  const visible = [...withUnits, ...withRecords];
  return <>
    <div className="existences-detail-header">
      <div>
        <h2>{item.product}</h2>
        <p><strong>{item.sku}</strong> · Talle {item.size || '—'} · Color {item.color || '—'}</p>
        <p className="existences-muted">Producto {item.productStatus === 'active' ? 'activo' : 'inactivo'} · Variante {item.variantStatus === 'active' ? 'activa' : 'inactiva'}</p>
      </div>
      <p className="existences-detail-amount"><strong>Vendible en {branch.name}</strong> <Quantity value={item.sellable} /></p>
    </div>
    {data.access.canCatalog && item.variantId && item.productStatus === 'active' && item.variantStatus === 'active' &&
      <button type="button" className="button-secondary" disabled={labelBusy} onClick={event => void openLabel(event.currentTarget)}>
        {labelBusy ? 'Consultando variante…' : 'Preparar etiquetas'}
      </button>}
    {labelError && <p role="alert">{labelError}</p>}
    {label && <LabelDialog data={label} storeId={branch.id} returnTo={labelOrigin.current} onClose={() => setLabel(undefined)} />}
    <section className="existences-breakdown" aria-labelledby="existences-breakdown-title">
      <h3 id="existences-breakdown-title">Desglose por ubicación</h3>
      {locations.length === 0 && <p className="existences-notice">La sucursal no tiene ubicaciones de inventario disponibles para esta variante.</p>}
      {visible.length > 0 && <LocationList locations={visible} adjustVariant={adjustVariant} />}
      {unrecorded.length > 0 && <details className="existences-unrecorded" onKeyDown={closeDisclosureOnEscape}>
        <summary>Ubicaciones sin saldo registrado ({unrecorded.length})</summary>
        <LocationList locations={unrecorded} compact adjustVariant={adjustVariant} />
      </details>}
    </section>
  </>;
}

export function Existences({ branch, hash, stockRevision, locationRevision, context, scope }: {
  branch: Store; hash: string; stockRevision: number; locationRevision: number; context: Context; scope: number;
}) {
  const { values, errors } = useMemo(() => readExistencesHash(hash), [hash]);
  const query = existenceSearch(values, false);
  const valid = errors.length === 0;
  const listIntent = useCallback(() => {
    const raw = window.location.hash;
    if (!isExistencesHash(raw)) return false;
    const parsed = readExistencesHash(raw);
    return parsed.errors.length === 0 && existenceSearch(parsed.values, false) === query;
  }, [query]);
  const detailIntent = useCallback(() => listIntent() && readExistencesHash(window.location.hash).values.detalle === values.detalle, [listIntent, values.detalle]);
  const loadList = useCallback((signal: AbortSignal) => api.existences(branch.id, query, signal), [branch.id, query]);
  const loadDetail = useCallback((signal: AbortSignal) => api.existence(branch.id, values.detalle, signal), [branch.id, values.detalle]);
  const loadLocations = useCallback((signal: AbortSignal) => api.locations(branch.id, signal), [branch.id]);
  const list = useExistenceRead(branch.id + ':' + stockRevision + ':' + locationRevision + ':' + query,
    valid && !values.detalle, context, scope, loadList, listIntent);
  const detail = useExistenceRead(branch.id + ':' + stockRevision + ':' + locationRevision + ':' + values.detalle,
    valid && Boolean(values.detalle), context, scope, loadDetail, detailIntent);
  const locations = useExistenceRead<LocationPage>(branch.id + ':' + locationRevision, valid && !values.detalle, context, scope, loadLocations, onlyExistences);
  const categories = useExistenceRead('categories', valid && !values.detalle, context, scope, readCategories, onlyExistences);
  const brands = useExistenceRead('brands', valid && !values.detalle, context, scope, readBrands, onlyExistences);
  const [scanExpired, setScanExpired] = useState<number>();
  const scanAccess = useMemo(() => ({
    isCurrent: () => context.scope === scope && listIntent() && !readExistencesHash(window.location.hash).values.detalle,
    onSessionExpired: () => setScanExpired(scope),
  }), [context, scope, listIntent]);
  const data = list.result?.data;
  const backLink = useRef<HTMLAnchorElement>(null);
  const returnTo = useRef('');
  const page = Number(values.page);
  const filterActive = Boolean(query && query !== 'page=' + page);
  const viewDetail = valid && Boolean(values.detalle);
  const expired = scanExpired === scope || [list, detail, locations, categories, brands].some(request => request.result?.status === 401);
  const filterValues = useMemo(() => ({ ...values, detalle: '' }), [values]);
  useLayoutEffect(() => {
    if (values.detalle && !expired) {
      returnTo.current = values.detalle;
      backLink.current?.focus();
    }
  }, [values.detalle, expired]);
  useLayoutEffect(() => {
    if (!values.detalle && data && returnTo.current) {
      if (document.activeElement === document.body) document.getElementById('existencia-' + returnTo.current)?.focus();
      returnTo.current = '';
    }
  }, [data, values.detalle]);
  function navigate(next: ExistenceQuery) { window.location.hash = existencesHref(next); }
  // A location filter belongs to one branch. When the branch changes or the
  // location is removed, drop the filter instead of showing a lasting 404.
  const branchLocations = locations.result?.data?.items;
  useEffect(() => {
    if (!values.ubicacion || !branchLocations || branchLocations.some(item => item.id === values.ubicacion)) return;
    window.history.replaceState(null, '', existencesHref({ ...values, ubicacion: '', page: '1' }));
    window.dispatchEvent(new HashChangeEvent('hashchange'));
  }, [values, branchLocations]);
  const noLocations = branchLocations?.length === 0;
  const auxiliaryFailures = ([locations, categories, brands] as const).filter(resource => resource.result?.error && resource.result.status !== 401);

  const detailData = detail.result?.data;
  const reading = viewDetail ? !detail.result : !list.result;
  return <section className="existences" aria-label="Consulta de existencias">
    {valid && !expired && <div className="existences-toolbar">
      {viewDetail && <a ref={backLink} className="existences-detail-link existences-back" href={existencesHref({ ...values, detalle: '' })}>← Volver al listado</a>}
      {!viewDetail && data?.access.canCatalog && <BarcodeScanner key={scope} access={scanAccess}>{match => <>
        <strong>{match.product.name} · {match.variant.sku}</strong>{' '}
        <a className="existences-detail-link" href={existencesHref({ ...values, detalle: match.variant.id })}>Ver stock del código en {branch.name}</a>
      </>}</BarcodeScanner>}
      {viewDetail ? detailData && <OperationLinks access={detailData.access} hasLocations={detailData.locations.some(location => location.active)}
        sku={detailData.item.sku} variantId={detailData.item.variantId ?? undefined} /> : data && <OperationLinks access={data.access} hasLocations={Boolean(locations.result?.data?.items.length)}
        location={values.ubicacion} />}
      <button type="button" className="button-secondary existences-refresh" aria-label="Actualizar existencias" aria-disabled={reading}
        onClick={() => { if (!reading) (viewDetail ? detail.retry : list.retry)(); }}>Actualizar</button>
    </div>}
    {expired ? <p className="existences-notice" role="alert">La sesión venció. Volvé a iniciar sesión para consultar existencias.</p> : <>
      {!valid && <div className="existences-notice" role="alert"><strong>Corregí los filtros de la dirección.</strong>
        <ul>{errors.map(error => <li key={error}>{error}</li>)}</ul>
        <button type="button" onClick={() => navigate(existenceDefaults)}>Restablecer filtros de Existencias</button>
      </div>}
      {viewDetail ? <>
        {!detail.result && <p className="existences-notice" role="status">Consultando la variante…</p>}
        {detail.result?.error && <div className="existences-notice" role="alert">
          <p>{detail.result.error}</p><button type="button" className="button-secondary" onClick={detail.retry}>Reintentar detalle</button>
        </div>}
        {detailData && <StockDetail key={branch.id + ":" + detailData.item.variantId} data={detailData} branch={branch} />}
      </> : <>
        {noLocations ? <div className="existences-empty" role="status">
          <span aria-hidden="true">▤</span>
          <h3>{branch.name} todavía no tiene ubicaciones de stock</h3>
          <p>Creá una ubicación (por ejemplo, Salón o Depósito) para registrar mercadería y consultar existencias.</p>
          <a className="primary-link" href={routeHref('stock/ajustes')}>Crear ubicación</a>
        </div> : <>
        <FilterForm filters={filterValues} locations={locations.result?.data?.items}
          categories={categories.result?.data?.items} brands={brands.result?.data?.items} categoryError={Boolean(categories.result?.error)} brandError={Boolean(brands.result?.error)} onApply={navigate} />
        {auxiliaryFailures.length > 0 && <p className="auxiliary-status" role="status">Algunos filtros no están disponibles por ahora; podés consultar igual.
          <button type="button" onClick={() => auxiliaryFailures.forEach(resource => resource.retry())}>Reintentar</button></p>}
        {valid && !list.result && <p className="existences-notice" role="status">Consultando existencias…</p>}
        {list.result?.error && !(list.result.status === 404 && values.ubicacion) && <div className="existences-notice" role="alert"><p>{list.result.error}</p>
          <button type="button" onClick={list.retry}>Reintentar existencias</button>
        </div>}
        {data && <>
          <Summary summary={data.summary} />
          <p className="existences-scope">Resumen de los resultados filtrados. El vendible excluye reservas y stock en ubicaciones o artículos no habilitados.</p>
          {data.items.length ? <StockTable rows={data.items} filters={values} /> :
            <p className="existences-notice" role="status">{page > 1 ? 'No hay resultados en esta página.' :
              filterActive ? 'No hay coincidencias con la búsqueda y los filtros.' : 'Todavía no hay productos para consultar.'}</p>}
          {page > 1 || data.hasMore ? <nav className="catalog-pagination" aria-label="Páginas de existencias">
            {page > 1 && <a href={existencesHref({ ...values, page: String(page - 1) })}>Anterior</a>}
            <span aria-current="page">Página {page}</span>
            {data.hasMore && page < lastExistencesPage && <a href={existencesHref({ ...values, page: String(page + 1) })}>Siguiente</a>}
            {page > 1 && data.items.length === 0 && <a href={existencesHref({ ...values, page: '1' })}>Volver a la página 1</a>}
          </nav> : null}
        </>}
        </>}
      </>}
    </>}
  </section>;
}
