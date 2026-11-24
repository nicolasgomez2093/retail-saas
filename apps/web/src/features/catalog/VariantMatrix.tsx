import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import {
  api, ApiError, type AttributeKind, type BatchVariantInput, type CatalogAttribute, type Variant,
  type VariantBatchIssue, type VariantBatchResult,
} from '../../shared/api/client';

const limit = 50;
const pageSize = 12;
type Draft = { sku: string; manual: boolean; generateInternal: boolean; status: Variant['status']; issues: VariantBatchIssue[] };
type Combination = { key: string; size: CatalogAttribute | null; color: CatalogAttribute | null };
type Attempt = { key: string; keys: string[]; rows: BatchVariantInput[] };
const blank: Draft = { sku: '', manual: false, generateInternal: false, status: 'active', issues: [] };
const message = (cause: unknown) => cause instanceof Error ? cause.message : 'No se pudo completar la operación.';

function combination(size: CatalogAttribute | null, color: CatalogAttribute | null): Combination {
  return { key: (size?.id ?? '') + ':' + (color?.id ?? ''), size, color };
}

function MatrixAxis({ title, items, selected, groups, onChange }: {
  title: string; items?: CatalogAttribute[]; selected: CatalogAttribute[]; groups?: CatalogAttribute[];
  onChange: (items: CatalogAttribute[]) => void;
}) {
  const [query, setQuery] = useState('');
  const q = query.trim().toLocaleLowerCase();
  const active = items?.filter(item => item.status === 'active') ?? [];
  const unavailable = selected.filter(item => items && !active.some(value => value.id === item.id));
  const filtered = active.filter(item => !q || [item.name, item.code].some(value => value?.toLocaleLowerCase().includes(q)));
  const ordered = groups ? [...filtered].sort((a,b) =>
    groups.findIndex(group => group.id === a.groupId) - groups.findIndex(group => group.id === b.groupId)) : filtered;
  function toggle(item: CatalogAttribute) {
    onChange(selected.some(value => value.id === item.id) ? selected.filter(value => value.id !== item.id) : [...selected, item]);
  }
  return <fieldset className="variant-matrix-axis">
    <legend>{title} · {selected.length} seleccionados</legend>
    {active.length > 8 && <label>Buscar {title.toLowerCase()}
      <input type="search" value={query} onChange={event => setQuery(event.target.value)} />
    </label>}
    {items && active.length === 0 && <p>Sin {title.toLowerCase()} activos configurados.</p>}
    {active.length > 0 && filtered.length === 0 && <p>Sin coincidencias.</p>}
    <div className="variant-matrix-choices">
      {[...ordered, ...unavailable].map(item => {
        const isUnavailable = unavailable.some(value => value.id === item.id);
        const group = groups?.find(value => value.id === item.groupId)?.name;
        return <label key={item.id}>
          <input type="checkbox" checked={selected.some(value => value.id === item.id)} onChange={() => toggle(item)} />
          <span>{item.name}{group && <small> · {group}</small>}{isUnavailable && <small> · Quitá este valor inactivo</small>}</span>
        </label>;
      })}
    </div>
  </fieldset>;
}

export function VariantMatrix({ productId, masters, masterError, reloadMasters, manage, protect, onClose, onCreated, expire }: {
  productId: string; masters?: Record<AttributeKind, CatalogAttribute[]>; masterError?: string; reloadMasters: () => void;
  manage: boolean; protect: RefObject<(() => boolean) | null>; onClose: () => void;
  onCreated: (result: VariantBatchResult) => void; expire: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const mounted = useRef(false);
  const running = useRef(false);
  const [sizes, setSizes] = useState<CatalogAttribute[]>([]);
  const [colors, setColors] = useState<CatalogAttribute[]>([]);
  const [without, setWithout] = useState(false);
  const [excluded, setExcluded] = useState(new Set<string>());
  const [only, setOnly] = useState<Set<string> | null>(null);
  const [page, setPage] = useState(0);
  const [review, setReview] = useState(false);
  const [reviewPage, setReviewPage] = useState(0);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [phase, setPhase] = useState<'preview' | 'saving' | null>(null);
  const [error, setError] = useState('');
  const [uncertain, setUncertain] = useState<Attempt>();
  const [success, setSuccess] = useState<VariantBatchResult>();

  const sizeAxis = without || sizes.length === 0 ? [null] : sizes;
  const colorAxis = without || colors.length === 0 ? [null] : colors;
  const total = without || sizes.length > 0 || colors.length > 0 ? sizeAxis.length * colorAxis.length : 0;
  const sizeIndices = new Map(sizeAxis.map((value, i) => [value?.id ?? '', i]));
  const colorIndices = new Map(colorAxis.map((value, i) => [value?.id ?? '', i]));
  function position(key: string) {
    const [size, color] = key.split(':');
    const s = sizeIndices.get(size), c = colorIndices.get(color);
    return s === undefined || c === undefined || total === 0 ? -1 : s * colorAxis.length + c;
  }
  function rowAt(index: number) {
    return combination(sizeAxis[Math.floor(index / colorAxis.length)], colorAxis[index % colorAxis.length]);
  }
  const activeSet = [...(only ?? excluded)].map(position).filter(i => i >= 0);
  const count = only ? activeSet.length : total - activeSet.length;
  const selected = (key: string) => only ? only.has(key) : !excluded.has(key);
  const dirty = !success && (total > 0 || Object.keys(drafts).length > 0 || Boolean(uncertain));

  useLayoutEffect(() => {
    mounted.current = true;
    heading.current?.focus();
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => {
    protect.current = () => !running.current && (!dirty ||
      window.confirm(uncertain ? 'El lote podría haberse guardado. ¿Salir y perder la posibilidad de recuperar este intento desde el editor?' :
        'Hay selecciones o cambios sin guardar. ¿Descartarlos?'));
    return () => { protect.current = null; };
  }, [dirty, protect, uncertain]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  function toggle(key: string) {
    const update = (old: Set<string>) => {
      const next = new Set(old);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    };
    if (only) setOnly(update(only)); else setExcluded(update);
  }
  function patchDraft(key: string, changes: Partial<Draft>) {
    setDrafts(previous => ({ ...previous, [key]: { ...(previous[key] ?? blank), ...changes } }));
  }
  const maximumPage = Math.max(0, Math.ceil(total / pageSize) - 1);
  const visiblePage = Math.min(page, maximumPage);
  const visibleRows = Array.from({ length: Math.min(pageSize, total - visiblePage * pageSize) },
    (_, index) => rowAt(visiblePage * pageSize + index));
  const rows: Combination[] = [];
  if (count <= limit) {
    if (only) {
      for (const index of activeSet.sort((a,b) => a-b)) rows.push(rowAt(index));
    } else if (count > 0) {
      for (let i = 0; i < total; i++) {
        const row = rowAt(i);
        if (selected(row.key)) rows.push(row);
      }
    }
  }
  function input(row: Combination, preview: boolean): BatchVariantInput {
    const current = drafts[row.key] ?? blank;
    const ref = (value: CatalogAttribute | null) => value ? { id: value.id, revision: value.revision } : null;
    return { size: ref(row.size), color: ref(row.color), sku: preview && !current.manual ? '' : current.sku,
      status: current.status, generateInternal: current.generateInternal };
  }
  async function requestPreview() {
    if (!manage || !masters || running.current || count < 1 || count > limit) return;
    running.current = true;
    setPhase('preview');
    setError('');
    try {
      const response = await api.previewVariantBatch(productId, rows.map(row => input(row, true)));
      if (!mounted.current) return;
      setDrafts(previous => {
        const next = { ...previous };
        for (const result of response.rows) {
          const key = rows[result.row - 1].key;
          const draft = previous[key] ?? blank;
          next[key] = { ...draft, sku: result.sku, issues: result.issues };
        }
        return next;
      });
      if (!review) setReviewPage(0);
      setReview(true);
      requestAnimationFrame(() => heading.current?.focus());
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.status === 401) expire();
      setError(message(cause));
    } finally {
      running.current = false;
      if (mounted.current) setPhase(null);
    }
  }

  async function send(attempt: Attempt) {
    if (!manage || running.current) return;
    running.current = true;
    setPhase('saving');
    setError('');
    try {
      const result = await api.createVariantBatch(productId, attempt.rows, attempt.key);
      if (!mounted.current) return;
      onCreated(result);
      setSuccess(result);
      setUncertain(undefined);
      requestAnimationFrame(() => heading.current?.focus());
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.status === 401) expire();
      const unknown = !(cause instanceof ApiError) || cause.status === 0 || cause.status === 408 ||
        (cause.status >= 500 && cause.code !== 'variant_batch_invalid');
      if (unknown) {
        setUncertain(attempt);
        setError('No pudimos confirmar el lote. Podés consultar el resultado con el mismo intento.');
      } else {
        setUncertain(undefined);
        setError(message(cause));
      }
      if (cause instanceof ApiError && cause.rows?.length) {
        setDrafts(previous => {
          const next = { ...previous };
          for (const key of attempt.keys) next[key] = { ...(next[key] ?? blank), issues: [] };
          for (const issue of cause.rows ?? []) {
            const key = attempt.keys[issue.row - 1];
            if (key) next[key].issues = [...next[key].issues, issue];
          }
          return next;
        });
      }
    } finally {
      running.current = false;
      if (mounted.current) setPhase(null);
    }
  }
  function useLatestMasters() {
    if (!masters || running.current) return;
    const replace = (values: CatalogAttribute[], kind: 'sizes' | 'colors') => values.map(value =>
      masters[kind].find(next => next.id === value.id && next.status === 'active') ?? value);
    setSizes(replace(sizes, 'sizes'));
    setColors(replace(colors, 'colors'));
    setReview(false);
    setError('');
  }
  function close() {
    if (protect.current?.() ?? true) onClose();
  }
  const firstIssue = rows.findIndex(row => !drafts[row.key]?.sku.trim() || drafts[row.key]?.issues.length);
  const lastReviewPage = Math.max(0, Math.ceil(rows.length / pageSize) - 1);
  const currentReviewPage = Math.min(reviewPage, lastReviewPage);
  const pageRows = rows.slice(currentReviewPage * pageSize, (currentReviewPage + 1) * pageSize);
  if (success) return <section className="variant-matrix" aria-label="Resultado del lote">
    <h2 tabIndex={-1} ref={heading}>Variantes creadas</h2>
    <p role="status">{success.variants.length} variantes creadas. Lote {success.id}.</p>
    <ul className="catalog-master-list">{success.variants.map(variant => <li key={variant.id}><span>
      <strong>{variant.sku}</strong><small>Variante {variant.id}{variant.internalBarcode ? ' · Código interno ' + variant.internalBarcode : ''}</small>
    </span></li>)}</ul>
    <button type="button" onClick={onClose}>Volver al producto</button>
  </section>;
  return <section className="variant-matrix" aria-label="Matriz de variantes" aria-busy={Boolean(phase)}>
    <h2 tabIndex={-1} ref={heading}>{review ? 'Previsualización de variantes' : 'Generar variantes'}</h2>
    {!manage && <p role="alert">Ya no tenés permiso para guardar variantes.</p>}
    {masterError && <p role="alert">{masterError} <button type="button" onClick={reloadMasters}>Reintentar catálogos</button></p>}
    {!masters && !masterError && manage && <p role="status">Consultando talles y colores…</p>}
    {error && <p role="alert">{error}</p>}
    {phase && <p role="status">{phase === 'preview' ? 'Preparando previsualización…' : 'Confirmando lote…'}</p>}
    <p role="status"><strong>{count} de {total} combinaciones seleccionadas</strong> · Máximo {limit} por operación</p>
    {count > limit && <p role="alert">Excluí {count - limit} combinaciones o elegí solo las filas que querés crear.</p>}
    <fieldset className="catalog-editor-fields" disabled={!manage || Boolean(phase) || Boolean(uncertain)}>
      {!review ? <>
        <label className="matrix-check"><input type="checkbox" checked={without} onChange={event => { setWithout(event.target.checked); setPage(0); }} />
          Generar una variante sin talle ni color</label>
        {!without && <div className="variant-matrix-axes">
          <MatrixAxis title="Talles" items={masters?.sizes} selected={sizes} groups={masters?.['size-groups']} onChange={items => { setSizes(items); setPage(0); }} />
          <MatrixAxis title="Colores" items={masters?.colors} selected={colors} onChange={items => { setColors(items); setPage(0); }} />
        </div>}
        {total === 0 && <p>Elegí talles, colores o la opción sin atributos.</p>}
        {total > 0 && <>
          <div className="actions">
            <button type="button" className="button-secondary" onClick={() => {
              if (only) { setOnly(null); setExcluded(new Set()); } else setOnly(new Set());
            }}>{only ? 'Seleccionar todas' : 'Elegir filas manualmente'}</button>
          </div>
          <ul className="variant-matrix-candidates">{visibleRows.map(row => <li key={row.key}>
            <label><input type="checkbox" checked={selected(row.key)} onChange={() => toggle(row.key)} />
              {row.size?.name ?? 'Sin talle'} · {row.color?.name ?? 'Sin color'}</label>
          </li>)}</ul>
          {maximumPage > 0 && <nav className="catalog-pagination" aria-label="Páginas de combinaciones">
            <button type="button" className="button-secondary" disabled={visiblePage===0} onClick={() => setPage(visiblePage - 1)}>Anterior</button>
            <span>Página {visiblePage+1} de {maximumPage+1}</span>
            <button type="button" className="button-secondary" disabled={visiblePage===maximumPage} onClick={() => setPage(visiblePage + 1)}>Siguiente</button>
          </nav>}
        </>}
      </> : <>
        <p>La previsualización no reserva identificadores. Los SKU y las combinaciones se validan al guardar.</p>
        <div className="variant-matrix-review">
          <table role="table">
            <thead role="rowgroup"><tr role="row"><th role="columnheader" scope="col">Talle y color</th><th role="columnheader" scope="col">SKU</th><th role="columnheader" scope="col">Código interno</th><th role="columnheader" scope="col">Estado</th><th role="columnheader" scope="col">Acciones</th></tr></thead>
            <tbody role="rowgroup">{pageRows.map((row, pageIndex) => {
              const index = currentReviewPage * pageSize + pageIndex;
              const draft = drafts[row.key] ?? blank;
              const name = (row.size?.name ?? 'Sin talle') + ' · ' + (row.color?.name ?? 'Sin color');
              const issueID = 'matrix-issue-' + row.key;
              return <tr role="row" key={row.key} id={'matrix-row-' + index} tabIndex={-1}>
                <th role="rowheader" scope="row">{name}
                  {draft.issues.length > 0 && <ul id={issueID}>{draft.issues.map((issue, i) =>
                    <li key={issue.code+i}>{issue.message}{issue.existing && <small> SKU {issue.existing.sku} · {issue.existing.status === 'active' ? 'Activa' : 'Inactiva'} · Variante {issue.existing.id}</small>}</li>)}</ul>}
                </th>
                <td role="cell"><input aria-label={'SKU fila ' + (index+1)} aria-invalid={draft.issues.some(issue => issue.field==='sku')}
                  aria-describedby={draft.issues.length ? issueID : undefined} maxLength={80} required
                  value={draft.sku} onChange={event => patchDraft(row.key, { sku: event.target.value, manual: true, issues: draft.issues.filter(issue => issue.field!=='sku') })} /></td>
                <td role="cell"><label className="matrix-check"><input type="checkbox" checked={draft.generateInternal}
                  onChange={event => patchDraft(row.key, { generateInternal: event.target.checked, issues: draft.issues.filter(issue => issue.field!=='generateInternal') })} />
                  Generar código<span className="pos-sr-only"> interno fila {index+1}</span></label></td>
                <td role="cell"><select aria-label={'Estado fila ' + (index+1)} value={draft.status}
                  onChange={event => patchDraft(row.key,{status:event.target.value as Variant['status']})}>
                  <option value="active">Activa</option><option value="inactive">Inactiva</option>
                </select></td>
                <td role="cell"><button type="button" className="button-secondary" aria-label={'Excluir ' + name} onClick={() => toggle(row.key)}>Excluir</button></td>
              </tr>;
            })}</tbody>
          </table>
          {rows.length === 0 && <p role="status">Sin combinaciones disponibles. Volvé a selección para incluir filas.</p>}
        </div>
        {lastReviewPage > 0 && <nav className="catalog-pagination" aria-label="Páginas de previsualización">
          <button type="button" className="button-secondary" disabled={currentReviewPage===0} onClick={() => setReviewPage(currentReviewPage-1)}>Anterior</button>
          <span>Página {currentReviewPage+1} de {lastReviewPage+1}</span>
          <button type="button" className="button-secondary" disabled={currentReviewPage===lastReviewPage} onClick={() => setReviewPage(currentReviewPage+1)}>Siguiente</button>
        </nav>}
        {firstIssue >= 0 && <button type="button" className="button-secondary" onClick={() => {
          setReviewPage(Math.floor(firstIssue / pageSize));
          requestAnimationFrame(() => document.getElementById('matrix-row-' + firstIssue)?.focus());
        }}>Ir a la primera fila pendiente</button>}

        <div className="actions">
          <button type="button" className="button-secondary" onClick={() => setReview(false)}>Volver a selección</button>
          <button type="button" className="button-secondary" disabled={!masters || count===0} onClick={() => void requestPreview()}>Revisar y renovar sugerencias</button>
          <button type="button" className="button-secondary" onClick={reloadMasters}>Actualizar catálogos</button>
          <button type="button" className="button-secondary" disabled={!masters} onClick={useLatestMasters}>Usar versiones actuales</button>
        </div>
      </>}
    </fieldset>
    <div className="actions variant-matrix-footer">
      {uncertain ? <button type="button" disabled={!manage || Boolean(phase)} onClick={() => void send(uncertain)}>Recuperar el mismo lote</button> :
        review ? <button type="button" disabled={!manage || Boolean(phase) || count===0 || count>limit || firstIssue>=0}
          onClick={() => void send({ key:crypto.randomUUID(),keys:rows.map(row => row.key),rows:rows.map(row => input(row,false)) })}>Crear {count} variantes</button> :
          <button type="button" disabled={!manage || !masters || Boolean(phase) || count===0 || count>limit}
            onClick={() => void requestPreview()}>Previsualizar</button>}
      <button type="button" className="button-secondary" disabled={Boolean(phase)} onClick={close}>Cancelar matriz</button>
    </div>
  </section>;
}
