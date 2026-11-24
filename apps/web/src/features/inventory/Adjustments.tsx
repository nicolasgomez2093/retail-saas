import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { ApiError, api, type ExistenceDetail, type ExistencePage, type LocationPage, type StockAdjustment, type StockMovement, type Store } from '../../shared/api/client';
import { InventoryVariantPicker, type Selected } from './InventoryVariantPicker';
import { StockMinimum } from './StockMinimum';
import { existenceDefaults, existencesHref } from './existencesQuery';
import { movementDefaults, movementHref } from './movementQuery';

type Context = { scope: number };
type Reading<T> = { key: string; data?: T; error?: string; status?: number };
type Attempt = { key: string; payload: StockAdjustment };
type Review = Attempt & { before: number; sku: string; locationName: string };
type LocalBalance = ExistenceDetail['locations'][number];
const maxUnits = 2147483647;

function readError(cause: unknown) {
  const status = cause instanceof ApiError ? cause.status : undefined;
  return { status, error: status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' : status === 403 ?
    'No tenés permiso para consultar el inventario.' : status === 404 ? 'La variante o la ubicación no está disponible.' :
      'No pudimos completar la consulta. Podés reintentar.' };
}

function useInventoryRead<T>(enabled: boolean, contextKey: string, revision: number, context: Context, scope: number,
  read: (signal: AbortSignal) => Promise<T>) {
  const [result, setResult] = useState<Reading<T>>();
  const [retry, setRetry] = useState(0);
  const key = JSON.stringify([contextKey, revision, retry, scope]);
  useEffect(() => {
    if (!enabled || context.scope !== scope) return;
    const controller = new AbortController();
    const current = () => !controller.signal.aborted && context.scope === scope;
    void read(controller.signal).then(data => {
      if (current()) setResult({ key, data });
    }).catch(cause => {
      if (current()) setResult({ key, ...readError(cause) });
    });
    return () => controller.abort();
  }, [enabled, key, contextKey, context, scope, read]);
  return { result: enabled && key === result?.key ? result : undefined,
    refresh: () => setRetry(value => value + 1) };
}

function amount(value: number | null) {
  return value === null ? 'Sin registro' : value.toLocaleString('es-AR');
}

function signed(value: number) {
  return (value > 0 ? '+' : '') + value.toLocaleString('es-AR');
}

function readAttempt(vault: string, storeId: string): Attempt | null {
  try {
    const saved = JSON.parse(sessionStorage.getItem(vault) ?? 'null') as Attempt | null;
    return saved?.key && saved.payload?.storeId === storeId ? saved : null;
  } catch {
    return null;
  }
}

function AdjustStock({ store, data, balance, ready, active, vault, onBusy, onRefresh, onChanged }: {
  store: Store; data?: ExistenceDetail; balance?: LocalBalance; ready: boolean; active: boolean; vault: string;
  onBusy: (value: boolean) => void; onRefresh: () => void; onChanged: () => void;
}) {
  const [delta, setDelta] = useState('');
  const [reason, setReason] = useState('');
  const [field, setField] = useState<'delta' | 'reason'>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [confirmed, setConfirmed] = useState<StockMovement>();
  const [attempt, setAttempt] = useState(() => readAttempt(vault, store.id));
  const [checked, setChecked] = useState(false);
  const [review, setReview] = useState<Review | null>(null);
  const busyGuard = useRef(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const success = useRef<HTMLDivElement>(null);
  const form = useRef<HTMLFormElement>(null);
  const target = data?.item.variantId && balance ? data.item.variantId + ':' + balance.id : '';
  const oldTarget = useRef('');
  useEffect(() => {
    if (!target || target === oldTarget.current) return;
    const previousTarget = oldTarget.current;
    oldTarget.current = target;
    if (!previousTarget) return;
    setDelta('');
    setReason('');
    setField(undefined);
    setError('');
    setConfirmed(undefined);
  }, [target]);
  const old = balance?.onHand ?? 0;
  const parsed = /^[+-]?\d+$/.test(delta.trim()) ? Number(delta) : NaN;
  const final = old + parsed;
  const validQty = Number.isInteger(parsed) && parsed >= -2147483648 && parsed <= maxUnits &&
    parsed !== 0 && final >= (balance?.reserved ?? 0) && final <= maxUnits;
  const writable = ready && Boolean(balance?.active && data?.access.canAdjust &&
    data.item.productStatus === 'active' && data.item.variantStatus === 'active');

  function storeAttempt(value: Attempt | null) {
    if (value) sessionStorage.setItem(vault, JSON.stringify(value));
    else { try { sessionStorage.removeItem(vault); } catch { /* Keep the confirmed server result. */ } }
    setAttempt(value);
  }

  useLayoutEffect(() => {
    if (!active && review) {
      dialog.current?.close();
      setReview(null);
    } else if (review && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [review, active]);
  useLayoutEffect(() => { if (confirmed) success.current?.focus(); }, [confirmed]);

  function closeReview(restore = true) {
    dialog.current?.close();
    setReview(null);
    if (restore) opener.current?.focus();
  }

  function prepare(event: FormEvent) {
    event.preventDefault();
    if (!writable || busyGuard.current || attempt || !balance || !data?.item.variantId) return;
    setError('');
    setField(undefined);
    setConfirmed(undefined);
    if (!validQty || !reason.trim() || [...reason.trim()].length > 300) {
      const invalid = !validQty ? 'delta' : 'reason';
      setField(invalid);
      setError(invalid === 'delta' ? 'Ingresá una diferencia entera distinta de cero. El físico resultante debe cubrir las unidades reservadas.' :
        'Ingresá un motivo de hasta 300 caracteres.');
      const control = form.current?.elements.namedItem(invalid);
      if (control instanceof HTMLElement) control.focus();
      return;
    }
    setReview({ key: crypto.randomUUID(), before: old, sku: data.item.sku, locationName: balance.name, payload: { storeId: store.id, locationId: balance.id, variantId: data.item.variantId,
      delta: parsed, reason: reason.trim(), expectedRevision: balance.revision ?? 'unrecorded' } });
  }

  function begin() {
    if (busyGuard.current) return false;
    busyGuard.current = true;
    setBusy(true);
    onBusy(true);
    setError('');
    return true;
  }

  function finish() {
    busyGuard.current = false;
    setBusy(false);
    onBusy(false);
  }

  function complete(movement: StockMovement) {
    storeAttempt(null);
    setConfirmed(movement);
    setDelta('');
    setReason('');
    onChanged();
  }

  async function send(value: Attempt, replay: boolean) {
    if (!begin()) return;
    setChecked(false);
    try {
      storeAttempt(value);
    } catch {
      setError('No pudimos preparar la recuperación local. El ajuste no se envió.');
      finish();
      return;
    }
    try {
      complete(await api.adjustStock(value.payload, value.key));
    } catch (cause) {
      const status = cause instanceof ApiError ? cause.status : undefined;
      const code = cause instanceof ApiError ? cause.code : undefined;
      const uncertain = status === undefined || status === 0 || status >= 500;
      if (!uncertain && (!replay || status === 409 || status === 400)) storeAttempt(null);
      setError(uncertain ? 'No se pudo confirmar el ajuste. Consultá el resultado antes de volver a enviar.' :
        code === 'adjustment_stale' ? 'El saldo cambió. Consultá el saldo actual y revisá la diferencia antes de confirmar.' :
        code === 'adjustment_inactive' ? 'El producto o la variante ya no están activos.' :
        code === 'inventory_balance_conflict' ? 'La diferencia supera el saldo permitido o afecta unidades reservadas.' :
        status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' :
        status === 403 ? 'Ya no tenés permiso para registrar ajustes.' :
          'El ajuste fue rechazado. Revisá la variante, la ubicación y el motivo.');
      if (code === 'adjustment_stale' || code === 'adjustment_inactive' || code === 'inventory_balance_conflict' || status === 401 || status === 403) onRefresh();
    } finally {
      finish();
    }
  }

  async function recover() {
    if (!attempt || !begin()) return;
    try {
      complete(await api.adjustmentResult(store.id, attempt.key));
    } catch (cause) {
      const status = cause instanceof ApiError ? cause.status : undefined;
      setChecked(status === 404);
      setError(status === 404 ? 'Todavía no encontramos un ajuste confirmado. Podés reenviar la misma operación con su clave original.' :
        status === 401 ? 'La sesión venció. Iniciá sesión para verificar el ajuste.' :
        status === 403 ? 'Ya no tenés permiso para consultar este ajuste.' :
          'No pudimos consultar el resultado. El ajuste sigue pendiente de verificación.');
    } finally { finish(); }
  }

  return <>
    {confirmed && <div ref={success} tabIndex={-1} className="adjustment-notice" role="status">
      <strong>Ajuste registrado</strong>
      <p>{confirmed.sku} · {signed(confirmed.delta)} unidades · Físico {confirmed.before} → {confirmed.after}</p>
      <small>Referencia {confirmed.id}</small>
      <div className="adjustment-actions">
        <a href={existencesHref({ ...existenceDefaults, detalle: confirmed.variantId })}>Ver existencias</a>
        <a href={movementHref({ ...movementDefaults, detalle: confirmed.id })}>Ver movimiento</a>
      </div>
    </div>}
    {attempt && <div className="adjustment-notice" role="status">
      <strong>{busy ? 'Procesando ajuste…' : 'Ajuste pendiente de verificación'}</strong>
      <p>{signed(attempt.payload.delta)} unidades · {attempt.payload.reason}</p>
      <div className="adjustment-actions">
        <button type="button" disabled={busy} onClick={() => void recover()}>Consultar resultado</button>
        {checked && <button type="button" className="button-secondary" disabled={busy} onClick={() => void send(attempt, true)}>Reenviar misma operación</button>}
      </div>
    </div>}
    {error && !field && <p className="adjustment-notice" role="alert">{error}</p>}
    {!attempt && data && balance && <form ref={form} className="adjustment-form" aria-label="Ajustar stock" noValidate onSubmit={prepare}>
      <label>Diferencia de ajuste
        <input name="delta" inputMode="text" value={delta} disabled={!writable || busy} placeholder="Ej. +3 ingresos o -2 egresos"
          onChange={event => { setDelta(event.target.value); if (field === 'delta') { setField(undefined); setError(''); } }}
          aria-invalid={field === 'delta' || undefined} aria-describedby={field === 'delta' ? 'adjust-field-error' : 'adjust-delta-help'} />
      </label>
      <small id="adjust-delta-help">Ingresá la diferencia en unidades. Un valor negativo reduce el físico.</small>
      <label>Motivo del ajuste
        <textarea name="reason" rows={2} maxLength={300} value={reason} disabled={!writable || busy}
          onChange={event => { setReason(event.target.value); if (field === 'reason') { setField(undefined); setError(''); } }}
          aria-invalid={field === 'reason' || undefined} aria-describedby={field === 'reason' ? 'adjust-field-error' : undefined} />
      </label>
      {field && <p id="adjust-field-error" role="alert">{error}</p>}
      <dl className="adjustment-metrics" aria-label="Vista previa del ajuste">
        <div><dt>Físico anterior</dt><dd>{balance.onHand === null ? '0 · saldo inicial' : amount(balance.onHand)}</dd></div>
        <div><dt>Diferencia</dt><dd>{Number.isInteger(parsed) ? signed(parsed) : '—'}</dd></div>
        <div><dt>Físico resultante</dt><dd>{validQty ? amount(final) : '—'}</dd></div>
      </dl>
      <button ref={opener} disabled={!writable || busy}>Revisar ajuste</button>
    </form>}
    <dialog ref={dialog} className="adjustment-dialog" aria-labelledby="adjust-confirm-title" onCancel={event => { event.preventDefault(); closeReview(); }}>
      {review && <>
        <h2 id="adjust-confirm-title">Confirmar ajuste</h2>
        <p>{review.sku} · {store.name} · {review.locationName}</p>
        <p>Físico {review.before} {signed(review.payload.delta)} → <strong>{review.before + review.payload.delta}</strong></p>
        <p>{review.payload.reason}</p>
        <div className="adjustment-actions">
          <button type="button" autoFocus className="button-secondary" onClick={() => closeReview()}>Volver a editar</button>
          <button type="button" onClick={() => {
            if (!writable || balance?.id !== review.payload.locationId || data?.item.variantId !== review.payload.variantId ||
              (balance.revision ?? 'unrecorded') !== review.payload.expectedRevision) {
              closeReview();
              setError('El saldo de la vista previa cambió. Revisá el físico actual antes de confirmar.');
              onRefresh();
              return;
            }
            closeReview(false);
            void send(review, false);
          }}>Confirmar y registrar</button>
        </div>
      </>}
    </dialog>
  </>;
}

export function Adjustments({ store, locations, locationId, onLocation, linkedVariant, active, context, scope, revision, permissionFresh, recoveryScope, locationTools, onBusy, onChanged }: {
  store: Store; locations: LocationPage; locationId: string; onLocation: (id: string) => void; linkedVariant: string;
  active: boolean; context: Context; scope: number; revision: number; permissionFresh: boolean; recoveryScope: string; locationTools?: ReactNode; onBusy: (value: boolean) => void;
  onChanged: () => void;
}) {
  const vault = 'retail:e3-adjustment:' + recoveryScope + ':' + store.id;
  const [tab, setTab] = useState<'adjust' | 'minimum'>(() => readAttempt(vault, store.id) || locations.canAdjust ? 'adjust' : 'minimum');
  const [adjustLink, setAdjustLink] = useState(() => ({ link: linkedVariant, variant: readAttempt(vault, store.id)?.payload.variantId ?? '' }));
  const [showPicker, setShowPicker] = useState(false);
  const focusReading = useRef(false);
  const readingHeading = useRef<HTMLHeadingElement>(null);
  const [editMinimum, setEditMinimum] = useState('');
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState('');
  const [draftQuery, setDraftQuery] = useState('');
  const [page, setPage] = useState(1);
  const adjusting = tab === 'adjust';
  const variantFromLink = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(linkedVariant) ? linkedVariant : '';
  const variantId = adjusting ? adjustLink.link === linkedVariant ? adjustLink.variant || variantFromLink : variantFromLink : editMinimum;
  const loadDetail = useCallback((signal: AbortSignal) => api.existence(store.id, variantId, signal), [store.id, variantId]);
  const loadList = useCallback((signal: AbortSignal) => api.existences(store.id,
    new URLSearchParams({ ubicacion: locationId, q: query, page: String(page), producto: 'active', variante: 'active' }).toString(), signal),
  [store.id, locationId, query, page]);
  const detail = useInventoryRead<ExistenceDetail>(active && Boolean(variantId), variantId + ':' + locationId, revision, context, scope, loadDetail);
  const list = useInventoryRead<ExistencePage>(active && !adjusting && Boolean(locationId), locationId + ':' + query + ':' + page, revision, context, scope, loadList);
  const fetched = detail.result?.data;
  const balance = fetched?.locations.find(item => item.id === locationId && item.active);
  const refresh = () => { detail.refresh(); if (!adjusting) list.refresh(); };
  const confirmed = () => { onChanged(); };
  const updateBusy = (value: boolean) => { setBusy(value); onBusy(value); };
  const choose = (item: Selected) => {
    focusReading.current = true;
    setAdjustLink({ link: linkedVariant, variant: item.id });
    setShowPicker(false);
  };
  useLayoutEffect(() => {
    if (balance && focusReading.current) {
      readingHeading.current?.focus();
      focusReading.current = false;
    }
  }, [balance, showPicker]);
  const forbidden = [detail, list].some(read => read.result?.status === 401 || read.result?.status === 403);
  const hasPending = Boolean(readAttempt(vault, store.id));
  const validVariant = fetched?.item.productStatus === 'active' && fetched.item.variantStatus === 'active';

  return <div className="adjustment-workspace" aria-label="Operaciones de ajuste y mínimos" hidden={!active}>
    <div className="adjustment-tabs" role="group" aria-label="Operación de inventario">
      {(locations.canAdjust || hasPending) && <button type="button" className="button-secondary" aria-pressed={adjusting} disabled={busy}
        onClick={() => setTab('adjust')}>Ajustar stock</button>}
      <button type="button" className="button-secondary" aria-pressed={!adjusting} disabled={busy || hasPending}
        onClick={() => setTab('minimum')}>Configurar mínimos</button>
    </div>
    <div className="adjustment-settings">
    {locations.items.length > 0 && <label className="adjustment-location">Ubicación de inventario
      <select name="inventoryLocation" value={locationId} disabled={busy || hasPending} onChange={event => {
        setEditMinimum(''); setPage(1); onLocation(event.target.value);
      }}>{!locationId && <option value="">Elegí una ubicación</option>}{locations.items.map(location => <option key={location.id} value={location.id}>
        {location.name}{locations.items.some(other => other.id !== location.id && other.name === location.name) ? ' (' + location.code + ')' : ''}
        {location.isSellable ? '' : ' · No vendible'}</option>)}</select>
    </label>}
    {locationTools}
    </div>
    {!locationId && locations.items.length > 0 && <p role="status" className="adjustment-notice">La ubicación del enlace no está disponible. Elegí una ubicación de esta sucursal.</p>}
    {adjusting && locations.canAdjust && !hasPending && locations.items.length > 0 && <div className="adjustment-panel adjustment-picker" hidden={Boolean(variantId) && !showPicker}>
      <h2>Seleccionar variante</h2><InventoryVariantPicker disabled={busy} onSelect={choose} />
    </div>}
    {!adjusting && locationId && !forbidden && <div className="adjustment-panel">
      <h2>Mínimos por variante</h2>
      {!locations.canManageMinimums && <p className="adjustment-notice">Podés consultar los mínimos. Tu rol no permite modificarlos.</p>}
      <form aria-label="Buscar mínimos" className="adjustment-min-search" onSubmit={event => {
        event.preventDefault(); setQuery(draftQuery.trim()); setPage(1);
      }}>
        <label>Producto o SKU<input name="minSearch" type="search" maxLength={160} value={draftQuery}
          onChange={event => setDraftQuery(event.target.value)} /></label><button>Buscar</button>
      </form>
      {!list.result && <p role="status">Consultando mínimos…</p>}
      {list.result?.data && <>
        {list.result.data.items.length === 0 && <p role="status">{query ? 'No hay coincidencias para esta búsqueda.' : 'Todavía no hay variantes activas para consultar.'}</p>}
        <ul className="adjustment-min-list">{list.result.data.items.map(row => <li key={row.variantId ?? row.productId}>
          <div><strong>{row.product}</strong><small>{row.sku} {[row.size, row.color].filter(Boolean).join(' · ')}</small></div>
          <div><small>Vendible</small>{amount(row.sellable)}</div>
          <div><small>Mínimo</small>{row.minimum === null ? 'Sin configurar' : amount(row.minimum)}
            {row.lowStock && <small className="adjustment-warning">Alerta de mínimo</small>}</div>
          <button id={'minimo-' + store.id + ':' + row.variantId} type="button" className="button-secondary" disabled={busy} onClick={() => { focusReading.current = true; setEditMinimum(row.variantId ?? ''); }}>
            {locations.canManageMinimums ? 'Configurar' : 'Consultar'}</button>
        </li>)}</ul>
        {(page > 1 || list.result.data.hasMore) && <nav aria-label="Páginas de mínimos" className="adjustment-actions">
          <button type="button" className="button-secondary" disabled={page <= 1} onClick={() => setPage(page - 1)}>Anterior</button>
          <span>Página {page}</span>
          <button type="button" className="button-secondary" disabled={!list.result.data.hasMore} onClick={() => setPage(page + 1)}>Siguiente</button>
        </nav>}
      </>}
      <small>La alerta usa el saldo disponible de la ubicación y se activa al alcanzar o bajar del mínimo.</small>
    </div>}
    {list.result?.error && <div className="adjustment-notice" role="alert">{list.result.error} {!forbidden && <button type="button" onClick={list.refresh}>Reintentar mínimos</button>}</div>}
    {variantId && !detail.result && <p role="status" className="adjustment-notice">Consultando saldo…</p>}
    {detail.result?.error && <div className="adjustment-notice" role="alert">{detail.result.error} {!forbidden && <button type="button" onClick={detail.refresh}>Reintentar lectura</button>}</div>}
    {fetched && balance && <div className="adjustment-panel adjustment-reading">
      <div><h2 ref={readingHeading} tabIndex={-1}>{fetched.item.product}</h2><p><strong>{fetched.item.sku}</strong>{[fetched.item.size, fetched.item.color].filter(Boolean).map(value => ' · ' + value)}</p>
        <p>{store.name} · {balance.name}{balance.isSellable ? '' : ' · Ubicación no vendible'}</p></div>
      <dl className="adjustment-metrics" aria-label="Saldo actual">
        <div><dt>Físico</dt><dd>{amount(balance.onHand)}</dd></div><div><dt>Reservado</dt><dd>{amount(balance.reserved)}</dd></div>
        <div><dt>Vendible</dt><dd>{amount(balance.sellable)}</dd></div>
        {!adjusting && <div><dt>Mínimo</dt><dd>{balance.minimum === null ? 'Sin configurar' : amount(balance.minimum)}</dd></div>}
      </dl>
      {!validVariant && <p className="adjustment-notice">El producto o la variante están inactivos.</p>}
      {balance.lowStock && <p className="adjustment-warning">Alerta de mínimo en esta ubicación.</p>}
      {adjusting && locations.canAdjust && !hasPending && <button className="button-secondary" type="button" disabled={busy}
        onClick={() => {
          setShowPicker(!showPicker);
          if (!showPicker) requestAnimationFrame(() => readingHeading.current?.closest('.adjustment-workspace')?.querySelector<HTMLInputElement>('[name=variantQuery]')?.focus());
        }}>{showPicker ? 'Cerrar búsqueda' : 'Cambiar variante'}</button>}
    </div>}
    {adjusting && (locations.canAdjust || hasPending) && <div className="adjustment-panel adjustment-editor" hidden={!variantId && !hasPending}>
      <h2>Registrar corrección física</h2>
      <AdjustStock key={vault} store={store} data={fetched} balance={balance}
        ready={permissionFresh && !forbidden} active={active} vault={vault} onBusy={updateBusy} onRefresh={refresh} onChanged={confirmed} />
    </div>}
    {!adjusting && editMinimum && locations.canManageMinimums && <div className="adjustment-panel adjustment-editor">
      <div className="adjustment-editor-header"><h2>Editar mínimo de la variante</h2>
        <button type="button" className="button-secondary" disabled={busy} onClick={() => {
          setEditMinimum('');
          requestAnimationFrame(() => document.getElementById('minimo-' + store.id + ':' + editMinimum)?.focus());
        }}>Cerrar edición</button>
      </div>
      <StockMinimum key={locationId + ':' + editMinimum} storeId={store.id} locationId={locationId} variantId={editMinimum}
        minimum={balance?.minimum ?? null} version={balance?.minimumVersion ?? null}
        disabled={!permissionFresh || !balance || !fetched?.access.canManageMinimums || busy} onSaved={confirmed} onRefresh={refresh} onBusy={updateBusy} />
    </div>}
  </div>;
}
