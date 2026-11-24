import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import { api, ApiError, type CountClose, type CountHistory, type CountLine, type CountReview, type CountStatus, type InventoryCount, type Store } from '../../shared/api/client';
import { InventoryVariantPicker, type Selected } from './InventoryVariantPicker';
import { routeHref } from '../../app/routes';
import { movementDefaults, movementHref } from './movementQuery';

const states: Record<CountStatus, string> = { DRAFT: 'Abierto', COMPLETED: 'Completado', CANCELLED: 'Cancelado' };
const uuid = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
const maxUnits = 2147483647;
type Line = CountLine & { input: string };
type Pending = { id: string; key: string; body: CountClose };

function recoverFrom(storage: string, store: string): Pending | undefined {
  try {
    const pending = JSON.parse(sessionStorage.getItem(storage) ?? 'null') as Pending | null;
    if (pending?.body?.storeId === store && uuid.test(pending.id) && uuid.test(pending.key) && Array.isArray(pending.body.references)) return pending;
  } catch { /* A malformed local value is not a pending command. */ }
}

function displayError(cause: unknown) {
  const status = cause instanceof ApiError ? cause.status : undefined;
  const code = cause instanceof ApiError ? cause.code : undefined;
  return code === 'count_stale' ? 'El saldo cambió. Revisá nuevamente las variantes indicadas. Las cantidades contadas se conservaron.' :
    code === 'count_draft_stale' ? 'Otro usuario actualizó el borrador. Consultá su versión antes de guardar o cerrar.' :
    code === 'count_reserved' ? 'Las variantes indicadas tienen unidades reservadas que el conteo no cubre.' :
    status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' :
    status === 403 ? 'No tenés permiso para realizar esta operación.' :
    status === 404 ? 'El conteo, la variante o la ubicación no está disponible en esta sucursal.' :
    status === 409 ? 'El estado del conteo cambió. Actualizá el detalle.' :
    status === 400 ? 'Revisá las variantes, cantidades y observaciones ingresadas.' :
    'No pudimos completar la consulta u operación. Podés volver a consultar.';
}

function asDraft(count: InventoryCount) {
  return count.items.map(line => ({ ...line, input: line.counted === null ? '' : String(line.counted) }));
}

function signed(units: number) {
  return (units > 0 ? '+' : '') + units.toLocaleString('es-AR');
}

function differenceLabel(units: number) {
  return units === 0 ? 'Sin cambios' : units > 0 ? 'Ingreso' : 'Egreso';
}

function LineIdentity({ line }: { line: Pick<CountLine, 'sku' | 'product' | 'size' | 'color'> }) {
  return <div><strong>{line.product}</strong><small>{[line.sku, line.size, line.color].filter(Boolean).join(' · ')}</small></div>;
}

export function StockCounts({ store, query, identityScope, context, scope, guard, onBusy, onStockChanged }: {
  store: Store; query: string; identityScope: string; context: {scope: number}; scope: number;
  guard: RefObject<(() => boolean) | null>; onBusy: (busy: boolean) => void; onStockChanged: () => void;
}) {
  const storage = 'retail:e4:close:' + identityScope + ':' + store.id;
  const linkedLocation = new URLSearchParams(query).get('ubicacion') ?? '';
  const linkedId = new URLSearchParams(query).get('conteo') ?? '';
  const [pending, setPending] = useState(() => recoverFrom(storage, store.id));
  const [id, setId] = useState(() => pending?.id ?? (uuid.test(linkedId) ? linkedId : ''));
  const [status, setStatus] = useState('');
  const [location, setLocation] = useState(uuid.test(linkedLocation) ? linkedLocation : '');
  const [search, setSearch] = useState('');
  const [searchDraft, setSearchDraft] = useState('');
  const [page, setPage] = useState(1);
  const [history, setHistory] = useState<CountHistory>();
  const [listError, setListError] = useState('');
  const [listRead, setListRead] = useState(0);
  const [listing, setListing] = useState(true);
  const [detail, setDetail] = useState<InventoryCount>();
  const [draft, setDraft] = useState<Line[]>([]);
  const [detailError, setDetailError] = useState('');
  const [detailRead, setDetailRead] = useState(0);
  const [starting, setStarting] = useState(false);
  const [creationLocation, setCreationLocation] = useState('');
  const [note, setNote] = useState('');
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [invalid, setInvalid] = useState<string[]>([]);
  const [conflicts, setConflicts] = useState<string[]>([]);
  const [review, setReview] = useState<CountReview>();
  const [notFound, setNotFound] = useState(false);
  const busyRef = useRef(false);
  const startKey = useRef<{ key: string; fingerprint: string } | undefined>(undefined);
  const opener = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const loadedFromStart = useRef('');
  const lastLinkedCount = useRef(linkedId);
  const reviewButton = useRef<HTMLButtonElement>(null);
  const detailTitle = useRef<HTMLHeadingElement>(null);
  const reviewTitle = useRef<HTMLHeadingElement>(null);

  const current = detail?.id === id ? detail : undefined;
  const dirty = Boolean(current?.status === 'DRAFT' && JSON.stringify(draft) !== JSON.stringify(asDraft(current))) ||
    starting && note.trim() !== '';
  const canWrite = Boolean(history?.canCount && current?.status === 'DRAFT' && current.locationActive);
  const date = (value: string) => new Intl.DateTimeFormat('es-AR', {
    day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: history?.timezone ?? 'UTC',
  }).format(new Date(value));

  useLayoutEffect(() => {
    const check = () => !busyRef.current && (!dirty || window.confirm('Hay cambios sin guardar. ¿Querés salir?'));
    guard.current = check;
    return () => { if (guard.current === check) guard.current = null; };
  }, [guard, dirty]);
  useEffect(() => {
    if (!dirty && !busy) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty, busy]);

  useEffect(() => {
    if (lastLinkedCount.current === linkedId) return;
    lastLinkedCount.current = linkedId;
    if (!pending) {
      setId(uuid.test(linkedId) ? linkedId : '');
      setReview(undefined);
      setDetailError('');
    }
  }, [linkedId, pending]);

  useEffect(() => {
    setLocation(uuid.test(linkedLocation) ? linkedLocation : '');
    setPage(1);
  }, [linkedLocation]);

  useEffect(() => {
    const controller = new AbortController();
    const live = () => !controller.signal.aborted && context.scope === scope;
    const filters = new URLSearchParams({ location_id: location, status, q: search, page: String(page) });
    setListing(true);
    setListError('');
    void api.countHistory(store.id, filters.toString(), controller.signal).then(result => {
      if (live()) setHistory(result);
    }).catch(cause => {
      if (live()) {
        setListError(displayError(cause));
        if (cause instanceof ApiError && (cause.status === 401 || cause.status === 403)) setHistory(undefined);
      }
    }).finally(() => { if (live()) setListing(false); });
    return () => controller.abort();
  }, [store.id, context, scope, location, status, page, search, listRead]);

  useEffect(() => {
    if (!id) return;
    if (loadedFromStart.current === id) {
      loadedFromStart.current = '';
      return;
    }
    const controller = new AbortController();
    const live = () => !controller.signal.aborted && context.scope === scope;
    setDetailError('');
    void api.inventoryCount(store.id, id, controller.signal).then(result => {
      if (live()) {
        setDetail(result);
        setDraft(asDraft(result));
        setReview(undefined);
      }
    }).catch(cause => {
      if (live()) {
        setDetailError(displayError(cause));
        if (cause instanceof ApiError && (cause.status === 401 || cause.status === 403)) {
          setDetail(undefined);
          setDraft([]);
        }
      }
    });
    return () => controller.abort();
  }, [store.id, id, detailRead, scope, context]);

  useLayoutEffect(() => { if (current?.id) detailTitle.current?.focus(); }, [current?.id]);
  useLayoutEffect(() => { if (review) reviewTitle.current?.focus(); }, [review]);

  function showCountRoute(next: string) {
    const currentQuery = new URLSearchParams(window.location.hash.split('?', 2)[1]);
    if (next) currentQuery.set('conteo', next);
    else currentQuery.delete('conteo');
    window.history.replaceState(window.history.state, '', routeHref('stock/conteos', currentQuery));
    setId(next);
  }

  function setCount(value: InventoryCount) {
    setDetail(value);
    setDraft(asDraft(value));
    setReview(undefined);
  }

  async function perform(operation: () => Promise<void>, handleError?: (cause: unknown) => void) {
    if (busyRef.current || context.scope !== scope) return;
    busyRef.current = true;
    setBusy(true);
    onBusy(true);
    setError('');
    try {
      await operation();
    } catch (cause) {
      if (context.scope === scope) {
        if (handleError) handleError(cause);
        else setError(displayError(cause));
      }
    } finally {
      busyRef.current = false;
      if (context.scope === scope) { setBusy(false); onBusy(false); }
    }
  }

  function start(event: FormEvent) {
    event.preventDefault();
    if (!history?.canCount || !creationLocation) return;
    const payload = { storeId: store.id, locationId: creationLocation, draft: true, note: note.trim() };
    const fingerprint = JSON.stringify(payload);
    if (startKey.current?.fingerprint !== fingerprint) startKey.current = { key: crypto.randomUUID(), fingerprint };
    const key = startKey.current.key;
    void perform(async () => {
      const value = await api.startCount(payload, key);
      if (context.scope !== scope) return;
      startKey.current = undefined;
      setStarting(false);
      setNote('');
      loadedFromStart.current = value.id;
      showCountRoute(value.id);
      setCount(value);
      setNotice('Conteo abierto. Todavía no cambió el stock.');
      setListRead(value => value + 1);
    });
  }

  function putLine(variant: Selected) {
    if (draft.some(line => line.variantId === variant.id)) { setError('La variante ya está incluida.'); return; }
    if (draft.length >= 100) { setError('El conteo admite hasta 100 variantes.'); return; }
    setError('');
    setDraft(previous => [...previous, { variantId: variant.id, sku: variant.sku, product: variant.product, size: variant.size ?? '',
      color: variant.color ?? '', expected: 0, counted: null, note: '', input: '', historicalLabels: false, expectedRecorded: false }]);
  }

  function update(variant: string, fields: Partial<Line>) {
    setInvalid(previous => previous.filter(id => id !== variant));
    setDraft(previous => previous.map(line => line.variantId === variant ? { ...line, ...fields } : line));
  }

  function save(event: FormEvent) {
    event.preventDefault();
    if (!current || !canWrite || pending) return;
    const bad = draft.filter(line => line.input.trim() !== '' &&
      (!/^\d+$/.test(line.input.trim()) || Number(line.input) > maxUnits)).map(line => line.variantId);
    setInvalid(bad);
    if (bad.length) {
      setError('Revisá las cantidades marcadas.');
      document.getElementById('counted-' + bad[0])?.focus();
      return;
    }
    void perform(async () => {
      const saved = await api.saveCountDraft(current.id, { storeId: store.id, expectedRevision: current.revision,
        items: draft.map(line => ({ variantId: line.variantId, counted: line.input.trim() === '' ? null : Number(line.input), note: line.note.trim() })) });
      if (context.scope !== scope) return;
      setCount(saved);
      setAdding(false);
      setNotice('Progreso guardado.');
      setConflicts([]);
      setListRead(value => value + 1);
    });
  }

  function inspect() {
    if (!current || !canWrite || dirty || pending) return;
    void perform(async () => {
      const fresh = await api.reviewCount(current.id, store.id);
      if (context.scope !== scope) return;
      if (fresh.revision !== current.revision) {
        setError('Otro usuario guardó cambios. Actualizá el detalle para ver el borrador vigente.');
        return;
      }
      setReview(fresh);
      setConflicts([]);
    });
  }

  function finish(value: InventoryCount) {
    try { sessionStorage.removeItem(storage); } catch { /* The server result remains confirmed. */ }
    setPending(undefined);
    setNotFound(false);
    setCount(value);
    setConflicts([]);
    setNotice('Conteo completado.');
    setListRead(previous => previous + 1);
    onStockChanged();
  }

  function send(operation: Pending, retry = false) {
    void perform(async () => {
      try { sessionStorage.setItem(storage, JSON.stringify(operation)); }
      catch { setError('No se pudo preparar la recuperación local. El cierre no se envió.'); return; }
      setPending(operation);
      setNotFound(false);
      const value = await api.closeCount(operation.id, operation.body, operation.key);
      if (context.scope === scope) finish(value);
    }, cause => {
      const status = cause instanceof ApiError ? cause.status : undefined;
      const uncertain = status === undefined || status === 0 || status >= 500;
      if (uncertain) {
        setError('El cierre no pudo confirmarse. Consultá el resultado antes de reenviar.');
        return;
      }
      if (!retry || status === 409 || status === 400) {
        try { sessionStorage.removeItem(storage); } catch { /* Keep the server state. */ }
        setPending(undefined);
      }
      setReview(undefined);
      setConflicts(cause instanceof ApiError ? cause.variantIds ?? [] : []);
      setError(displayError(cause));
    });
  }

  function confirm() {
    if (!review || !current || pending || busyRef.current || !canWrite || review.items.some(line => line.blocked) || !review.items.length) return;
    dialog.current?.close();
    send({ id: current.id, key: crypto.randomUUID(), body: { storeId: store.id, expectedRevision: review.revision,
      references: review.items.map(({variantId, stockRevision}) => ({variantId, stockRevision})) } });
  }

  function consult() {
    if (!pending) return;
    void perform(async () => {
      const value = await api.countCloseResult(pending.id, store.id, pending.key);
      if (context.scope === scope) finish(value);
    }, cause => {
      setNotFound(cause instanceof ApiError && cause.status === 404);
      setError(cause instanceof ApiError && cause.status === 404 ?
        'Todavía no se observa un cierre confirmado. Podés reenviar la misma operación con la clave original.' : displayError(cause));
    });
  }

  function back() {
    if (pending || !guard.current?.()) return;
    showCountRoute('');
    setDetail(undefined);
    setDraft([]);
    setReview(undefined);
    setError('');
    setDetailError('');
    setConflicts([]);
    setAdding(false);
  }

  function refreshDetail() {
    if (busy || pending || !guard.current?.()) return;
    setDetailRead(value => value + 1);
  }

  return <section className="counts-workspace" aria-label="Conteos físicos">
    <div className="counts-toolbar">
      {id ? <button type="button" className="button-secondary" disabled={busy || Boolean(pending)} onClick={back}>Volver al listado</button> :
        <span>Conteos de la sucursal activa</span>}
      {id ? <button type="button" className="button-secondary" onClick={refreshDetail} disabled={busy || Boolean(pending)}>Actualizar detalle</button> :
        history?.canCount && !starting && <button type="button" onClick={() => {
          const first = history.locations.find(item => item.id === location && item.active) ?? history.locations.find(item => item.active);
          setCreationLocation(first?.id ?? '');
          setStarting(true); setNotice(''); setError('');
        }}>Nuevo conteo</button>}
    </div>
    {notice && <p className="counts-notice" role="status">{notice}</p>}
    {error && <p className="counts-notice" role="alert">{error}</p>}
    {busy && <p className="counts-notice" role="status">Operación en curso…</p>}
    {pending && <div className="counts-notice">
      <strong>Cierre pendiente de verificación</strong><p>Conteo {pending.id}</p>
      <div className="counts-actions"><button type="button" disabled={busy} onClick={consult}>Consultar resultado</button>
        {notFound && <button type="button" className="button-secondary" disabled={busy} onClick={() => send(pending, true)}>Reenviar mismo cierre</button>}
      </div>
    </div>}
    {!id && <>
      {starting && <form className="counts-panel counts-editor" aria-label="Nuevo conteo físico" onSubmit={start}>
        <h2>Nuevo conteo físico</h2>
        <label>Ubicación de conteo<select value={creationLocation} required disabled={busy} onChange={event => setCreationLocation(event.target.value)}>
          <option value="">Elegí una ubicación</option>
          {history?.locations.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select></label>
        <label>Observación del conteo <input name="countNote" maxLength={300} value={note} disabled={busy} onChange={event => setNote(event.target.value)} /></label>
        {!history?.locations.some(item => item.active) && <p>La sucursal necesita una ubicación activa para abrir un conteo.</p>}
        <div className="counts-actions"><button disabled={busy || !creationLocation}>Abrir conteo</button>
          <button type="button" className="button-secondary" disabled={busy} onClick={() => {
            if (!guard.current?.()) return;
            setStarting(false); setNote('');
          }}>Cancelar alta</button>
        </div>
      </form>}
      <form className="counts-panel counts-filters" aria-label="Filtros de conteos" onSubmit={event => {
        event.preventDefault(); setSearch(searchDraft.trim()); setPage(1);
      }}>
        <label>Buscar sesión<input type="search" placeholder="SKU, responsable o referencia" maxLength={160} value={searchDraft} onChange={event => setSearchDraft(event.target.value)} /></label>
        <label>Estado<select value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}>
          <option value="">Todos</option>{Object.entries(states).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        <label>Ubicación<select value={location} onChange={event => { setLocation(event.target.value); setPage(1); }}>
          <option value="">Todas</option>
          {location && !history?.locations.some(item => item.id === location) && <option value={location}>Ubicación del enlace</option>}
          {history?.locations.map(item => <option key={item.id} value={item.id}>{item.name}{item.active ? '' : ' · Inactiva'}</option>)}
        </select></label>
        <button>Buscar</button>
      </form>
      {listing && <p className="counts-notice" role="status">Consultando conteos…</p>}
      {listError && <div className="counts-notice" role="alert">{listError} <button type="button" onClick={() => setListRead(value => value + 1)}>Reintentar lista</button></div>}
      {!listing && !listError && history?.items.length === 0 && <p className="counts-notice">{status || search || location ?
        'No hay conteos con estos filtros.' : 'Todavía no hay conteos en esta sucursal.'}</p>}
      {!listing && !listError && Boolean(history?.items.length) && <div className="counts-panel counts-table"><table role="table" aria-label="Sesiones de conteo">
        <thead role="rowgroup"><tr role="row">{['Fecha y hora','Ubicación','Responsable','Estado','Progreso','Acción'].map(label =>
          <th key={label} role="columnheader" scope="col">{label}</th>)}</tr></thead>
        <tbody role="rowgroup">{history?.items.map(item => <tr role="row" key={item.id}>
          <td role="cell" data-label="Fecha">{date(item.createdAt)}</td>
          <th role="rowheader" scope="row">{item.location}</th>
          <td role="cell" data-label="Responsable">{item.creator}</td>
          <td role="cell" data-label="Estado">{states[item.status]}</td>
          <td role="cell" data-label="Progreso">{item.countedCount} de {item.lineCount}</td>
          <td role="cell"><button type="button" className="catalog-link" disabled={busy} onClick={() => {
            if (!guard.current?.()) return;
            setStarting(false); setNote(''); setNotice(''); setError(''); showCountRoute(item.id);
          }}>Ver conteo</button></td>
        </tr>)}</tbody>
      </table></div>}
      {(page > 1 || history?.hasMore) && <nav className="counts-actions" aria-label="Páginas de conteos">
        <button type="button" className="button-secondary" disabled={listing || page === 1} onClick={() => setPage(page - 1)}>Anterior</button>
        <span>Página {page}</span>
        <button type="button" className="button-secondary" disabled={listing || !history?.hasMore} onClick={() => setPage(page + 1)}>Siguiente</button>
      </nav>}
    </>}
    {id && !current && !detailError && <p className="counts-notice" role="status">Consultando detalle…</p>}
    {detailError && <p className="counts-notice" role="alert">{detailError}</p>}
    {current && <>
      <header className="counts-panel counts-heading">
        <div><h2 ref={detailTitle} tabIndex={-1}>{current.location}</h2><p>{current.store} · {states[current.status]}</p></div>
        <div><p>Abierto por {current.creator} · {date(current.createdAt)}</p>
          {current.completedAt && <p>Cerrado por {current.closer ?? 'Responsable no disponible'} · {date(current.completedAt)}</p>}
          {current.cancelledAt && <p>Cancelado el {date(current.cancelledAt)}</p>}
        </div>
        <small>Conteo {current.id}{!current.historicalLabels ? ' · etiquetas actuales donde no hay snapshot' : ''}</small>
        {current.note && <p>{current.note}</p>}
      </header>
      {canWrite && !pending && !review && <>
        <div className="counts-panel counts-actions">
          <button type="button" className="button-secondary" disabled={busy} aria-expanded={adding} onClick={() => setAdding(!adding)}>{adding ? 'Cerrar búsqueda' : 'Agregar variante'}</button>
          <button ref={reviewButton} type="button" className="button-secondary" disabled={busy || dirty || current.countedCount === 0} onClick={inspect}>Revisar diferencias</button>
          <button type="button" className="button-secondary" disabled={busy} onClick={() => {
            if (!window.confirm(dirty ? '¿Cancelar el conteo y descartar los cambios sin guardar?' : '¿Cancelar el conteo? Se conserva en el historial.')) return;
            void perform(async () => {
              const cancelled = await api.cancelCount(current.id, store.id);
              if (context.scope !== scope) return;
              setCount(cancelled); setNotice('Conteo cancelado.'); setListRead(value => value + 1);
            });
          }}>Cancelar conteo</button>
        </div>
        {adding && <div className="counts-panel counts-picker"><InventoryVariantPicker disabled={busy} onSelect={putLine} /></div>}
        <form className="counts-panel counts-editor" aria-label="Registrar cantidades" noValidate onSubmit={save}>
          <h3>Cantidades observadas</h3>
          <p>Un campo vacío deja la variante sin contar. Cero registra que la ubicación está vacía para esa variante.</p>
          {!draft.length && <p>Agregá las variantes que querés contar.</p>}
          {draft.map(line => <div className="counts-edit-line" key={line.variantId} data-conflict={conflicts.includes(line.variantId)}>
            <div><LineIdentity line={line} />
              {current.items.some(item => item.variantId === line.variantId) && <small>
                Físico de referencia al agregar {line.expectedRecorded ? line.expected : 'sin registro'}
              </small>}
            </div>
            {conflicts.includes(line.variantId) && <p className="counts-line-warning">El saldo cambió o tiene reservas pendientes.</p>}
            <label>Cantidad contada
              <input id={'counted-' + line.variantId} aria-label={'Cantidad contada de ' + line.sku} inputMode="numeric"
                value={line.input} disabled={busy} onChange={event => update(line.variantId,{input:event.target.value})}
                aria-invalid={invalid.includes(line.variantId) || undefined}
                aria-describedby={invalid.includes(line.variantId) ? 'invalid-' + line.variantId : undefined} />
              {invalid.includes(line.variantId) && <small id={'invalid-' + line.variantId}>Ingresá un entero entre 0 y 2.147.483.647.</small>}
            </label>
            <label>Observación de la línea<input maxLength={300} value={line.note} disabled={busy} onChange={event => update(line.variantId,{note:event.target.value})} /></label>
            <button type="button" className="button-secondary" disabled={busy} aria-label={'Quitar ' + line.sku}
              onClick={() => setDraft(previous => previous.filter(item => item.variantId !== line.variantId))}>Quitar</button>
          </div>)}
          <button disabled={busy || !dirty}>Guardar progreso</button>
          {dirty && <small>Guardá los cambios para revisar las diferencias.</small>}
        </form>
      </>}
      {review && !pending && <section className="counts-panel counts-review" aria-labelledby="count-review-title">
        <h3 id="count-review-title" ref={reviewTitle} tabIndex={-1}>Revisión de diferencias</h3>
        <div className="counts-summary">
          <span>Contadas <strong>{review.items.length}</strong></span>
          <span>Con diferencias <strong>{review.items.filter(line => line.delta !== 0).length}</strong></span>
          <span>A incrementar <strong>{review.items.reduce((sum,line) => sum + Math.max(0,line.delta),0)}</strong></span>
          <span>A disminuir <strong>{review.items.reduce((sum,line) => sum + Math.max(0,-line.delta),0)}</strong></span>
        </div>
        <p>{current.items.length - review.items.length} sin contar. El cierre conserva su saldo.</p>
        {review.items.map(line => {
          const original = current.items.find(item => item.variantId === line.variantId);
          return <div className="counts-review-line" key={line.variantId}>
            {original && <LineIdentity line={original} />}
            <dl className="counts-summary">
              <div><dt>Físico registrado</dt><dd>{line.current ?? 'Sin registro'}</dd></div>
              <div><dt>Cantidad contada</dt><dd>{line.counted}</dd></div>
              <div data-difference={differenceLabel(line.delta)}><dt>{differenceLabel(line.delta)}</dt><dd>{signed(line.delta)}</dd></div>
            </dl>
            {line.blocked && <p className="counts-line-warning">Hay {line.reserved} unidades reservadas. Corregí la cantidad antes de cerrar.</p>}
          </div>;
        })}
        <div className="counts-actions">
          <button type="button" className="button-secondary" disabled={busy} onClick={() => { setReview(undefined); requestAnimationFrame(() => reviewButton.current?.focus()); }}>Volver a editar</button>
          <button ref={opener} type="button" disabled={busy || !review.items.length || review.items.some(line => line.blocked)}
            onClick={() => dialog.current?.showModal()}>Confirmar cierre</button>
        </div>
        <dialog ref={dialog} aria-labelledby="count-close-title" className="counts-dialog" onClose={() => opener.current?.focus()} onCancel={event => {
          event.preventDefault(); dialog.current?.close(); opener.current?.focus();
        }}>
          <h2 id="count-close-title">Cerrar conteo</h2>
          <p>Se aplicarán las diferencias de {review.items.filter(line => line.delta !== 0).length} variantes en {current.location}.</p>
          <p>Una vez cerrado, el conteo queda en el historial.</p>
          <div className="counts-actions"><button type="button" autoFocus className="button-secondary" onClick={() => dialog.current?.close()}>Volver a revisar</button>
            <button type="button" onClick={confirm}>Cerrar y aplicar</button></div>
        </dialog>
      </section>}
      {(current.status !== 'DRAFT' || !canWrite) && <section className="counts-panel counts-review" aria-label="Cantidades y movimientos">
        <h3>{current.status === 'COMPLETED' ? 'Resultado del conteo' : 'Cantidades registradas'}</h3>
        {current.items.length === 0 && <p>Esta sesión no tiene variantes cargadas.</p>}
        {current.items.map(line => {
          const movement = current.movements.find(item => item.variantId === line.variantId);
          return <div className="counts-review-line" key={line.variantId}>
            <LineIdentity line={line} />
            {!line.historicalLabels && <small>Producto y atributos de la ficha actual.</small>}
            <dl className="counts-summary">
              <div><dt>{current.status === 'COMPLETED' && line.counted !== null ? 'Saldo al cierre' : 'Saldo de referencia'}</dt><dd>{line.expectedRecorded ? line.expected : 'Sin registro'}</dd></div>
              <div><dt>Contada</dt><dd>{line.counted ?? 'Sin contar'}</dd></div>
              <div><dt>Ajuste aplicado</dt><dd>{movement ? signed(movement.delta) :
                current.status === 'COMPLETED' && line.counted !== null ? 'Sin cambios' : '—'}</dd></div>
            </dl>
            {line.note && <p>{line.note}</p>}
            {movement && <a href={movementHref({ ...movementDefaults, referencia: current.id, detalle: movement.id })}>Ver movimiento</a>}
          </div>;
        })}
      </section>}
    </>}
  </section>;
}
