import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { api, ApiError, type AttributeData, type AttributeKind, type CatalogAttribute, type PendingVariant } from '../../shared/api/client';

const labels = { sizes: 'Talles', colors: 'Colores', 'size-groups': 'Grupos de talles' };
const kinds: AttributeKind[] = ['size-groups', 'sizes', 'colors'];
type Masters = Record<AttributeKind, CatalogAttribute[]>;
const empty: Masters = { sizes: [], colors: [], 'size-groups': [] };
const initial: AttributeData = { name: '', code: null, groupId: null, hex: null, position: 0, status: 'active' };
const message = (cause: unknown) => cause instanceof Error ? cause.message : 'No pudimos completar la consulta.';

export function useCatalogAttributes(enabled: boolean, scope: number, expire: () => void) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ scope: number; attempt: number; data?: Masters; error?: string }>();
  useEffect(() => {
    if (!enabled) return;
    let current = true;
    void Promise.all(kinds.map((kind) => api.attributes(kind))).then((responses) => {
      if (!current) return;
      setResult({ scope, attempt, data: {
        'size-groups': responses[0].items, sizes: responses[1].items, colors: responses[2].items,
      } });
    }).catch((cause: unknown) => {
      if (!current) return;
      if (cause instanceof ApiError && cause.status === 401) expire();
      setResult({ scope, attempt, error: message(cause) });
    });
    return () => { current = false; };
  }, [enabled, attempt, scope, expire]);
  const valid = enabled && result?.scope === scope && result.attempt === attempt ? result : undefined;
  return { ...valid, retry: () => setAttempt(n => n + 1) };
}

export function AttributePicker({ label, kind, id, text, masters, error, invalid, describedBy }: {
  label: string; kind: 'sizes' | 'colors'; id?: string | null; text?: string; masters?: Masters; error?: string;
  invalid?: boolean; describedBy?: string;
}) {
  const fieldID = useId();
  const [selection, setSelection] = useState(id ?? (text ? 'legacy' : ''));
  const [query, setQuery] = useState('');
  const current = masters?.[kind].find(item => item.id === selection || 'refresh-' + item.id === selection);
  const q = query.trim().toLocaleLowerCase();
  const items = (masters?.[kind] ?? []).filter(item => item.id === current?.id || item.id === id ||
    (item.status === 'active' && (!q || [item.name, item.code].some(value => value?.toLocaleLowerCase().includes(q)))));
  const groups = kind === 'sizes' ? masters?.['size-groups'] ?? [] : [];
  const choices = (entries: CatalogAttribute[]) => entries.map(item =>
    <option key={item.id} value={item.id} disabled={item.status === 'inactive' && item.id !== id && item.id !== selection}>
      {item.name}{item.status === 'inactive' ? ' (inactivo)' : ''}
    </option>);
  const storedMaster = masters?.[kind].find(item => item.id === id);
  const refreshAvailable = storedMaster?.status === 'active' && storedMaster.name !== text;
  const staleSelection = selection && selection !== 'legacy' && selection !== id &&
    !items.some(item => item.id === selection) && !(refreshAvailable && selection === 'refresh-' + storedMaster?.id);
  return <div className="catalog-attribute-picker">
    {Boolean(masters && masters[kind].length > 8) && <label>Buscar {label.toLowerCase()}
      <input type="search" value={query} onChange={event => setQuery(event.target.value)} />
    </label>}
    <label htmlFor={fieldID}>{label}</label>
    <select id={fieldID} name={kind === 'sizes' ? 'sizeId' : 'colorId'} value={selection}
      aria-invalid={invalid} aria-describedby={describedBy} onChange={event => setSelection(event.target.value)}>
      <option value="">Sin {label.toLowerCase()}</option>
      {text && !id && <option value="legacy">Conservar texto histórico: {text}</option>}
      {id && !storedMaster && <option value={id}>{text || 'Valor asociado'}</option>}
      {groups.map(group => {
        const members = items.filter(item => item.groupId === group.id);
        return members.length > 0 && <optgroup key={group.id} label={group.name + (group.status === 'inactive' ? ' (grupo inactivo)' : '')}>
          {choices(members)}
        </optgroup>;
      })}
      {choices(items.filter(item => !item.groupId))}
      {staleSelection && <option value={selection}>Selección no disponible</option>}
      {refreshAvailable &&
        <option value={'refresh-' + storedMaster.id}>Aplicar etiqueta actual: {storedMaster.name}</option>}
    </select>
    {id && text && storedMaster && storedMaster.name !== text && <small>Etiqueta de la variante: {text}</small>}
    {!masters && !error && <small role="status">Cargando opciones…</small>}
    {q && items.length === 0 && <small role="status">Sin coincidencias.</small>}
  </div>;
}

export function CatalogAttributeTools({ masters, loadingError, reload, active, scope, expire, onResolve, protect }: {
  masters?: Masters; loadingError?: string; reload: () => void; active: boolean; scope: number; expire: () => void;
  onResolve: (item: PendingVariant) => void; protect: RefObject<(() => boolean) | null>;
}) {
  const [section, setSection] = useState<AttributeKind | 'pending'>('sizes');
  const [filter, setFilter] = useState('');
  const [status, setStatus] = useState('');
  const [sort, setSort] = useState('position');
  const [draft, setDraft] = useState<AttributeData>(initial);
  const [editing, setEditing] = useState<CatalogAttribute>();
  const [fieldError, setFieldError] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [blocked, setBlocked] = useState(false);
  const [checkedCurrent, setCheckedCurrent] = useState(false);
  const [busy, setBusy] = useState(false);
  const writing = useRef(false);
  const mounted = useRef(false);
  const firstField = useRef<HTMLInputElement>(null);
  const prefix = useId();
  const dirty = (Object.keys(draft) as Array<keyof AttributeData>).some(key => draft[key] !== (editing ?? initial)[key]);
  const latestScope = useRef(scope);
  useEffect(() => { latestScope.current = scope; }, [scope]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);

  function discard() {
    return !writing.current && (!dirty || window.confirm('Hay cambios sin guardar en el catálogo maestro. ¿Descartarlos?'));
  }
  useEffect(() => {
    protect.current = discard;
    return () => { protect.current = null; };
  });
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  function reset(target?: CatalogAttribute) {
    setEditing(target);
    setDraft(target ? { name: target.name, code: target.code, hex: target.hex, position: target.position, groupId: target.groupId, status: target.status } : initial);
    setError('');
    setFieldError('');
    setNotice('');
    setBlocked(false);
    setCheckedCurrent(false);
  }
  function switchSection(next: typeof section) {
    if (next === section || !discard()) return;
    reset();
    setFilter('');
    setStatus('');
    setSort('position');
    setSection(next);
  }
  const update = <K extends keyof AttributeData>(key: K, value: AttributeData[K]) => setDraft(previous => ({ ...previous, [key]: value }));
  const field = (name: keyof AttributeData) => ({
    'aria-invalid': fieldError === name,
    'aria-describedby': fieldError === name ? prefix + '-error' : undefined,
  });
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (section === 'pending' || writing.current || blocked) return;
    writing.current = true;
    setBusy(true);
    setError('');
    setFieldError('');
    setNotice('');
    try {
      const result = await api.saveAttribute(section, draft, editing);
      if (!mounted.current || latestScope.current !== scope) return;
      reset(result);
      setNotice('Valor guardado.');
      reload();
    } catch (cause) {
      if (!mounted.current || latestScope.current !== scope) return;
      if (cause instanceof ApiError && cause.status === 401) expire();
      if (cause instanceof ApiError && cause.field) setFieldError(cause.field);
      const uncertain = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500 || cause.status === 408;
      if (uncertain || (cause instanceof ApiError && cause.code === 'revision_conflict')) setBlocked(true);
      setError(uncertain ? 'No pudimos confirmar el guardado. Actualizá el listado antes de volver a guardar.' : message(cause));
    } finally {
      writing.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  const available = masters ?? empty;
  const visible = section === 'pending' ? [] : available[section].filter(item =>
    (!status || item.status === status) &&
    (!filter || [item.name, item.code].some(value => value?.toLocaleLowerCase().includes(filter.toLocaleLowerCase().trim()))));
  if (sort === 'name') visible.sort((a, b) => a.name.localeCompare(b.name, 'es'));
  const server = section !== 'pending' && editing ? available[section].find(item => item.id === editing.id) : undefined;
  const groupName = (id: string | null) => available['size-groups'].find(entry => entry.id === id)?.name ?? 'Sin grupo';
  return <section className="catalog-master" aria-label="Catálogos de talles y colores">
    <div className="catalog-tool-sections" role="group" aria-label="Secciones de atributos">
      {kinds.map(kind => <button key={kind} type="button" className="button-secondary" aria-pressed={section === kind}
        onClick={() => switchSection(kind)}>{labels[kind]}</button>)}
      <button type="button" className="button-secondary" aria-pressed={section === 'pending'} onClick={() => switchSection('pending')}>Asociaciones pendientes</button>
    </div>
    {loadingError && <p role="alert">{loadingError} <button type="button" onClick={reload}>Reintentar catálogos</button></p>}
    {section === 'pending'
      ? <PendingAssociations active={active} scope={scope} expire={expire} onResolve={onResolve} />
      : <>
        <div className="catalog-master-filters">
          <label>Buscar en {labels[section].toLowerCase()}<input type="search" value={filter} onChange={event => setFilter(event.target.value)} /></label>
          <label>Filtrar por estado<select value={status} onChange={event => setStatus(event.target.value)}>
            <option value="">Todos</option><option value="active">Activos</option><option value="inactive">Inactivos</option>
          </select></label>
          <label>Ordenar por<select value={sort} onChange={event => setSort(event.target.value)}>
            <option value="position">Orden configurado</option><option value="name">Nombre</option>
          </select></label>
        </div>
        {!masters && !loadingError && <p role="status">Cargando valores…</p>}
        {masters && <ul className="catalog-master-list">
          {visible.map(item => <li key={item.id}>
            <span>{item.hex && <span className="catalog-color-swatch" aria-hidden="true" style={{ backgroundColor: item.hex }} />}
              <strong>{item.name}</strong>
              <small>{item.status === 'active' ? 'Activo' : 'Inactivo'} · Orden {item.position}
                {item.code && ' · ' + item.code}
                {item.groupId && ' · ' + (available['size-groups'].find(group => group.id === item.groupId)?.name ?? 'Grupo')}</small>
            </span>
            <button type="button" className="button-secondary" aria-label={'Editar ' + item.name} onClick={() => {
              if (discard()) { reset(item); firstField.current?.focus(); }
            }}>Editar</button>
          </li>)}
          {!visible.length && <li>Sin valores para mostrar.</li>}
        </ul>}
        <form aria-label={editing ? 'Editar valor maestro' : 'Nuevo valor maestro'} onSubmit={event => void save(event)}>
          <h3>{editing ? 'Editar ' + editing.name : 'Nuevo valor'}</h3>
          <fieldset className="catalog-editor-fields" disabled={busy}>
            <label>Nombre<input ref={firstField} required maxLength={60} value={draft.name} {...field('name')}
              onChange={event => update('name', event.target.value)} /></label>
            {section !== 'size-groups' && <label>Código<input maxLength={60} value={draft.code ?? ''} {...field('code')}
              onChange={event => update('code', event.target.value || null)} /></label>}
            <label>Orden<input type="number" min={0} max={9999} value={draft.position} required {...field('position')}
              onChange={event => update('position', event.target.valueAsNumber || 0)} /></label>
            <label>Estado<select value={draft.status} {...field('status')}
              onChange={event => update('status', event.target.value as AttributeData['status'])}>
              <option value="active">Activo</option><option value="inactive">Inactivo</option>
            </select></label>
            {section === 'colors' && <label>Color hexadecimal (opcional)<input value={draft.hex ?? ''} placeholder="#000000" {...field('hex')}
              onChange={event => update('hex', event.target.value || null)} /></label>}
            {section === 'sizes' && <label>Grupo de talles<select value={draft.groupId ?? ''} {...field('groupId')}
              onChange={event => update('groupId', event.target.value || null)}>
              <option value="">Sin grupo</option>{available['size-groups'].filter(g => g.status === 'active' || g.id === draft.groupId).map(g =>
                <option key={g.id} value={g.id}>{g.name}{g.status === 'inactive' ? ' (inactivo)' : ''}</option>)}
            </select></label>}
          </fieldset>
          {editing && <small>Las variantes vinculadas conservan su talle y color registrados.</small>}
          {error && <p role="alert" id={prefix + '-error'}>{error}</p>}
          {notice && <p role="status">{notice}</p>}
          {blocked && <div>
            <button type="button" className="button-secondary" onClick={() => { setCheckedCurrent(true); reload(); }}>Consultar valores actuales</button>
            {checkedCurrent && server && <p>Servidor: {server.name}, {server.code ?? 'sin código'}, {server.status === 'active' ? 'activo' : 'inactivo'}, orden {server.position}.
              {section === 'sizes' && ' Grupo: ' + groupName(server.groupId) + '.'}
              {section === 'colors' && ' Muestra: ' + (server.hex ?? 'sin hexadecimal') + '.'}
              {' '}Revisión {server.revision}.</p>}
            {checkedCurrent && server && <button type="button" className="button-secondary" onClick={() => {
              const newer = { name: server.name, code: server.code, hex: server.hex, position: server.position, groupId: server.groupId, status: server.status };
              const changed = Object.fromEntries(Object.entries(draft).filter(([key, value]) => value !== editing?.[key as keyof AttributeData]));
              setDraft({ ...newer, ...changed });
              setEditing(server); setError(''); setBlocked(false); setCheckedCurrent(false);
            }}>Revisé el valor actual, conservar mis cambios</button>}
          </div>}
          <div className="actions">
            <button disabled={busy || blocked}>{busy ? 'Guardando…' : 'Guardar valor'}</button>
            {(editing || dirty || blocked) && <button type="button" disabled={busy} className="button-secondary" onClick={() => {
              if (discard()) reset();
            }}>{editing ? 'Nuevo valor' : 'Limpiar formulario'}</button>}
          </div>
        </form>
      </>}
  </section>;
}

function PendingAssociations({ active, scope, expire, onResolve }: {
  active: boolean; scope: number; expire: () => void; onResolve: (item: PendingVariant) => void;
}) {
  const [filter, setFilter] = useState('');
  const [offset, setOffset] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ key: string; items?: PendingVariant[]; hasMore?: boolean; error?: string }>();
  const key = JSON.stringify([scope, filter, offset, attempt, active]);
  useEffect(() => {
    if (!active) return;
    let current = true;
    void api.pendingAttributes(filter, offset).then(page => { if (current) setResult({ key, ...page }); }).catch((cause: unknown) => {
      if (!current) return;
      if (cause instanceof ApiError && cause.status === 401) expire();
      setResult({ key, error: message(cause) });
    });
    return () => { current = false; };
  }, [key, active, filter, offset, expire]);
  const visible = result?.key === key ? result : undefined;
  return <>
    <p>Las variantes conservan sus textos históricos. Podés vincular un valor maestro revisando cada variante.</p>
    <label>Buscar asociaciones pendientes<input type="search" value={filter} maxLength={160}
      onChange={event => { setFilter(event.target.value); setOffset(0); }} /></label>
    {!visible && <p role="status">Consultando asociaciones…</p>}
    {visible?.error && <p role="alert">{visible.error} <button type="button" onClick={() => setAttempt(n => n + 1)}>Reintentar</button></p>}
    {visible?.items?.length === 0 && <p role="status">Sin asociaciones pendientes en esta página.</p>}
    <ul className="catalog-master-list">{visible?.items?.map(item => <li key={item.id}>
      <span><strong>{item.productName} · {item.sku}</strong>
        <small>{[item.size && !item.sizeId && 'Talle: ' + item.size, item.color && !item.colorId && 'Color: ' + item.color].filter(Boolean).join(' · ')}</small>
      </span>
      <button type="button" className="button-secondary" onClick={() => onResolve(item)}>Revisar asociación</button>
    </li>)}</ul>
    {(offset > 0 || visible?.hasMore) && <nav className="catalog-pagination" aria-label="Páginas de asociaciones">
      <button type="button" disabled={offset === 0} onClick={() => setOffset(n => n - 25)}>Anterior</button>
      <span>Página {offset / 25 + 1}</span>
      <button type="button" disabled={!visible?.hasMore} onClick={() => setOffset(n => n + 25)}>Siguiente</button>
    </nav>}
  </>;
}
