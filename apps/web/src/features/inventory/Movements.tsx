import { useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { api, ApiError, type HistoryMovement, type MovementHistory, type MovementHistoryDetail, type MovementLocation, type Store } from '../../shared/api/client';
import { routeHref } from '../../app/routes';
import { existenceDefaults, existencesHref } from './existencesQuery';
import { isMovementHash, movementDefaults, movementHref, movementKinds, movementSearch, readMovementHash, type MovementQuery } from './movementQuery';

type Context = { scope: number; movements: number };
type Reading = { key: string; list?: MovementHistory; detail?: MovementHistoryDetail; error?: string; status?: number };

function kind(item: HistoryMovement) {
  return movementKinds[item.type as keyof typeof movementKinds] ?? 'Movimiento de inventario';
}

function Timestamp({ value, zone, detailed }: { value: string; zone: string; detailed?: boolean }) {
  const date = new Date(value);
  return <time dateTime={value}>
    <span>{new Intl.DateTimeFormat('es-AR', { day: '2-digit', month: '2-digit', year: 'numeric', timeZone: zone }).format(date)}</span>
    <small>{new Intl.DateTimeFormat('es-AR', { timeStyle: detailed ? 'medium' : 'short', timeZone: zone, hourCycle: 'h23' }).format(date)}</small>
  </time>;
}

function Filters({ filters, locations, onApply }: {
  filters: MovementQuery; locations: MovementLocation[]; onApply: (filters: MovementQuery) => void;
}) {
  const [draft, setDraft] = useState(filters);
  useEffect(() => { setDraft(filters); }, [filters]);
  const advanced = [draft.desde, draft.hasta, draft.responsable, draft.referencia].filter(Boolean).length;
  function set(field: keyof MovementQuery, value: string) {
    setDraft(current => ({ ...current, [field]: value }));
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    onApply({ ...draft, siguiente: '', anterior: '', detalle: '' });
  }
  return <form className="movement-filters" aria-label="Filtros de movimientos" onSubmit={submit}>
    <label className="movement-search">Producto o SKU
      <input name="q" type="search" placeholder="Nombre o SKU" value={draft.q} onChange={event => set('q', event.target.value)} />
    </label>
    <label>Tipo<select name="tipo" value={draft.tipo} onChange={event => set('tipo', event.target.value)}>
      <option value="">Todos</option>
      {Object.entries(movementKinds).map(([id, name]) => <option key={id} value={id}>{name}</option>)}
    </select></label>
    <label>Ubicación<select name="ubicacion" value={draft.ubicacion} onChange={event => set('ubicacion', event.target.value)}>
      <option value="">Todas</option>
      {draft.ubicacion && !locations.some(location => location.id === draft.ubicacion) &&
        <option value={draft.ubicacion}>Ubicación filtrada</option>}
      {locations.map(location => <option key={location.id} value={location.id}>{location.name} ({location.code}){location.active ? '' : ' · Inactiva'}</option>)}
    </select></label>
    <details className="movement-advanced" onKeyDown={event => {
      if (event.key !== 'Escape' || !event.currentTarget.open) return;
      event.preventDefault();
      event.currentTarget.open = false;
      event.currentTarget.querySelector('summary')?.focus();
    }}>
      <summary>Más filtros{advanced ? ' (' + advanced + ')' : ''}</summary>
      <div className="movement-advanced-fields">
        <label>Desde<input name="desde" type="date" lang="es-AR" value={draft.desde} onChange={event => set('desde', event.target.value)} /></label>
        <label>Hasta<input name="hasta" type="date" lang="es-AR" value={draft.hasta} onChange={event => set('hasta', event.target.value)} /></label>
        <label>Responsable<input name="responsable" type="search" placeholder="Nombre o ID" value={draft.responsable} onChange={event => set('responsable', event.target.value)} /></label>
        <label>Referencia<input name="referencia" type="search" placeholder="UUID de la operación" value={draft.referencia} onChange={event => set('referencia', event.target.value)} /></label>
      </div>
    </details>
    <button className="movement-apply">Aplicar filtros</button>
  </form>;
}

function Location({ data, branch }: { data: MovementLocation; branch: string }) {
  return <>{data.name}{!data.active && <small>Inactiva</small>}{data.storeName !== branch && <small>{data.storeName}</small>}</>;
}

function RouteLocation({ item, branch }: { item: HistoryMovement; branch: string }) {
  if (!item.transferId) return <Location data={item.location} branch={branch} />;
  return <>
    {item.origin ? <Location data={item.origin} branch={branch} /> : 'Origen restringido'}
    <small>→ {item.destination ? <Location data={item.destination} branch={branch} /> : 'Destino restringido'}</small>
  </>;
}

function MovementTable({ page, filters, branch }: { page: MovementHistory; filters: MovementQuery; branch: Store }) {
  return <div className="movement-table">
    <table role="table" aria-label="Historial de movimientos de stock físico">
      <thead role="rowgroup"><tr role="row">{['Fecha', 'Tipo', 'Producto / SKU', 'Unid.', 'Ubicación', 'Detalle']
        .map(column => <th role="columnheader" scope="col" key={column} aria-label={column === 'Unid.' ? 'Unidades' : column === 'Fecha' ? 'Fecha y hora' : undefined}>{column}</th>)}</tr></thead>
      <tbody role="rowgroup">{page.items.map(item => <tr key={item.id} role="row">
        <td role="cell" className="movement-date"><Timestamp value={item.createdAt} zone={page.timezone} /></td>
        <td role="cell" className="movement-kind">{kind(item)}</td>
        <th role="rowheader" scope="row"><strong>{item.product}</strong><small>{item.sku}{!item.productSnapshot && ' · ficha actual'}</small></th>
        <td role="cell" className="movement-delta" data-positive={item.delta > 0}>{item.delta > 0 ? '+' : ''}{item.delta.toLocaleString('es-AR')}</td>
        <td role="cell" className="movement-place"><RouteLocation item={item} branch={branch.name} /></td>
        <td role="cell" className="movement-action"><a id={'movimiento-' + item.id}
          href={movementHref({ ...filters, detalle: item.id })} aria-label={'Ver ' + kind(item) + ' de ' + item.sku}>Ver detalle</a></td>
      </tr>)}</tbody>
    </table>
  </div>;
}

function Detail({ data, branch }: { data: MovementHistoryDetail; branch: Store }) {
  const { item, timezone } = data;
  const related = [
    [item.saleNumber ? 'Venta n.º ' + item.saleNumber : 'Venta', item.saleId],
    ['Devolución', item.returnId], ['Cambio', item.exchangeId], ['Transferencia', item.transferId],
    ['Conteo', item.countId], ['Recepción de compra', item.purchaseReceiptId], ['Orden de compra', item.purchaseOrderId],
  ].filter((entry): entry is [string, string] => Boolean(entry[1]));
  return <article className="movement-detail">
    <header><div><h2>{kind(item)}</h2><p>{item.product}</p></div>
      <p className="movement-delta movement-detail-delta" data-positive={item.delta > 0}>{item.delta > 0 ? '+' : ''}{item.delta.toLocaleString('es-AR')} unidades</p>
    </header>
    <dl className="movement-facts">
      <div><dt>Fecha y hora local</dt><dd><Timestamp value={item.createdAt} zone={timezone} detailed /></dd></div>
      <div><dt>Sucursal consultada</dt><dd>{branch.name}</dd></div>
      <div><dt>SKU registrado</dt><dd>{item.sku}</dd></div>

      <div><dt>Físico en esta ubicación</dt><dd>{item.before.toLocaleString('es-AR')} → {item.after.toLocaleString('es-AR')}</dd></div>
      <div className="movement-fact-wide"><dt>Responsable · nombre actual</dt><dd>{item.actorName}<details className="movement-actor-id"><summary>Ver identificador</summary><small>{item.actorId}</small></details></dd></div>
      <div className="movement-fact-wide"><dt>Ubicación afectada{item.location.historical ? ' · registrada' : ' · nombre actual'}</dt><dd>{item.location.name} ({item.location.code}){!item.location.active && <small>Inactiva</small>}</dd></div>
      {item.origin && <div><dt>Origen</dt><dd><Location data={item.origin} branch={branch.name} /></dd></div>}
      {item.destination && <div><dt>Destino</dt><dd><Location data={item.destination} branch={branch.name} /></dd></div>}
      {(item.type === 'adjustment' || item.type === 'count_adjustment') &&
        <div><dt>Origen / destino</dt><dd>No aplica. Cambio del físico en la ubicación.</dd></div>}
      {item.otherEndHidden && <div><dt>Otro extremo</dt><dd>Ubicación restringida por alcance de sucursal.</dd></div>}
      {item.size && <div><dt>Talle</dt><dd>{item.size}</dd></div>}
      {item.color && <div><dt>Color</dt><dd>{item.color}</dd></div>}
    </dl>
    <p className="movement-muted">{item.productSnapshot ? 'Producto y atributos registrados en el documento.' :
      'Producto, talle y color corresponden a la ficha actual. El SKU pertenece al historial del movimiento.'}</p>
    <div className="movement-references">
      <h3>Trazabilidad</h3>
      <dl className="movement-facts">
        <div><dt>ID del movimiento</dt><dd>{item.id}</dd></div>
        {related.map(([label, id]) => <div key={id}><dt>{label}</dt><dd>{id}</dd></div>)}
      </dl>
      {item.reason && <p><strong>Motivo registrado:</strong> {item.reason}</p>}
    </div>
    {item.countId && <a href={routeHref('stock/conteos', new URLSearchParams({ conteo: item.countId }))}>Ver conteo físico</a>}
    <a href={existencesHref({ ...existenceDefaults, detalle: item.variantId })}>Consultar existencias actuales</a>
  </article>;
}

export function Movements({ branch, hash, context, scope, navigation, stockRevision }: {
  branch: Store; hash: string; context: Context; scope: number; navigation: number; stockRevision: number;
}) {
  const { values, errors } = useMemo(() => readMovementHash(hash), [hash]);
  const valid = errors.length === 0;
  const search = movementSearch(values);
  const detailId = values.detalle;
  const [retry, setRetry] = useState(0);
  const [reading, setReading] = useState<Reading>();
  const [knownLocations, setKnownLocations] = useState<{ scope: number; items: MovementLocation[] }>();
  const request = JSON.stringify([scope, navigation, branch.id, search, detailId, stockRevision, retry]);
  const current = reading?.key === request && valid ? reading : undefined;
  const newIntent = useRef(0);
  useEffect(() => {
    const controller = new AbortController();
    const generation = ++newIntent.current;
    const active = () => {
      if (!valid || controller.signal.aborted || generation !== newIntent.current || context.scope !== scope || context.movements !== navigation) return false;
      const live = window.location.hash;
      if (!isMovementHash(live)) return false;
      const parsed = readMovementHash(live);
      return parsed.errors.length === 0 && movementSearch(parsed.values) === search && parsed.values.detalle === detailId;
    };
    if (!active()) return;
    const load = detailId
      ? api.movementDetail(branch.id, detailId, controller.signal).then(detail => ({ detail }))
      : api.movementHistory(branch.id, search, controller.signal).then(list => ({ list }));
    void load.then(result => {
      if (!active()) return;
      setReading({ key: request, ...result });
      if ('list' in result) setKnownLocations({ scope, items: result.list.locations });
    }).catch((cause: unknown) => {
      if (!active()) return;
      const status = cause instanceof ApiError ? cause.status : undefined;
      const error = status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' : status === 403 ? 'No tenés permiso para consultar movimientos.' :
        status === 404 ? (detailId ? 'El movimiento no está disponible en esta sucursal.' : 'La sucursal, ubicación o página ya no está disponible.') :
          'No pudimos consultar los movimientos. Intentá nuevamente.';
      setReading({ key: request, status, error });
      if (status === 401 || status === 403) setKnownLocations(undefined);
    });
    return () => controller.abort();
  }, [valid, request, detailId, search, branch.id, scope, navigation, context]);

  const backLink = useRef<HTMLAnchorElement>(null);
  const returnTo = useRef('');
  useLayoutEffect(() => {
    if (detailId && valid) {
      returnTo.current = detailId;
      backLink.current?.focus();
    }
  }, [detailId, valid]);
  useLayoutEffect(() => {
    if (!detailId && current?.list && returnTo.current) {
      if (document.activeElement === document.body) document.getElementById('movimiento-' + returnTo.current)?.focus();
      returnTo.current = '';
    }
  }, [detailId, current?.list]);

  const forbidden = current?.status === 401 || current?.status === 403;
  const first = useMemo(() => ({ ...values, siguiente: '', anterior: '', detalle: '' }), [values]);
  const filtering = Boolean(movementSearch(first));
  const paged = Boolean(values.siguiente || values.anterior);
  const reload = () => { newIntent.current++; setRetry(value => value + 1); };
  return <section className="movements" aria-label="Historial de movimientos">
    {!forbidden && <div className="movement-toolbar">
      {detailId && valid ? <a ref={backLink} href={movementHref({ ...values, detalle: '' })}>← Volver al historial</a> :
        <p>Historial de stock físico</p>}
      {valid && <button type="button" className="button-secondary" aria-label="Actualizar movimientos" aria-disabled={!current}
        onClick={() => { if (current) reload(); }}>Actualizar</button>}
    </div>}
    {!valid && <div className="movement-notice" role="alert"><strong>Revisá los filtros de la dirección.</strong>
      <ul>{errors.map(error => <li key={error}>{error}</li>)}</ul>
      <a href={movementHref(movementDefaults)}>Restablecer filtros</a>
    </div>}
    {!forbidden && !detailId && <Filters key={scope} filters={first}
      locations={knownLocations?.scope === scope ? knownLocations.items : []}
      onApply={filters => { window.location.hash = movementHref(filters); }} />}
    {valid && !forbidden && !detailId && values.variante && <p className="movement-filter-chip">
      Variante seleccionada desde Existencias <a href={movementHref({ ...first, variante: '' })}>Quitar filtro de variante</a>
    </p>}
    {valid && !current && <p className="movement-notice" role="status">{detailId ? 'Consultando el movimiento…' : 'Consultando movimientos…'}</p>}
    {current?.error && <div className="movement-notice" role="alert"><p>{current.error}</p>
      {!forbidden && <button type="button" className="button-secondary" onClick={reload}>Reintentar {detailId ? 'detalle' : 'historial'}</button>}
      {!detailId && current.status === 404 && (paged || values.ubicacion) &&
        <a href={movementHref({ ...first, ubicacion: '' })}>Quitar ubicación y volver al inicio del historial</a>}
    </div>}
    {current?.detail && <Detail data={current.detail} branch={branch} />}
    {current?.list && <>
      {current.list.items.length > 0 ? <MovementTable page={current.list} filters={values} branch={branch} /> :
        <p className="movement-notice" role="status">{paged ? 'Ya no hay movimientos en esta página.' :
          filtering ? 'No hay movimientos que coincidan con los filtros.' : 'Todavía no hay movimientos en esta sucursal.'}</p>}
      {(current.list.older || current.list.newer || paged) && <nav className="movement-pagination" aria-label="Páginas del historial">
        {current.list.newer && <a href={movementHref({ ...first, anterior: current.list.newer })}>Más recientes</a>}
        {current.list.older && <a href={movementHref({ ...first, siguiente: current.list.older })}>Más antiguos</a>}
        {paged && !current.list.items.length && <a href={movementHref(first)}>Volver al inicio del historial</a>}
      </nav>}
    </>}
  </section>;
}

