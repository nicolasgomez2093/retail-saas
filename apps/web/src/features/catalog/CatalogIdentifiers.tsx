import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { api, ApiError, type IdentifierEvent, type IdentifierSettings, type SKUSuggestion } from '../../shared/api/client';

const message = (cause: unknown) => cause instanceof Error ? cause.message : 'No se pudo completar la operación.';

export function VariantSKU({ initial, error, scope, enabled, onApply }: {
  initial?: string; error?: string; scope: number; enabled: boolean; onApply: () => void;
}) {
  const [value, setValue] = useState(initial ?? '');
  const [after, setAfter] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ key: string; offer?: SKUSuggestion; error?: string }>();
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const key = [scope, after, attempt].join(':');
  useEffect(() => {
    if (initial !== undefined || !enabled) return;
    let current = true;
    void api.skuSuggestion(after).then(offer => {
      if (current) setResult({ key, offer });
    }).catch((cause: unknown) => {
      if (current) setResult({ key, error: message(cause) });
    });
    return () => { current = false; };
  }, [initial, enabled, key, after]);
  const visible = result?.key === key ? result : undefined;
  const offer = visible?.offer;
  return <div className="catalog-sku">
    <label>SKU<input ref={input} name="sku" required value={value}
      aria-invalid={Boolean(error)} aria-describedby={error ? id : undefined}
      onChange={event => setValue(event.target.value)} /></label>
    {error && <small id={id} role="alert">{error}</small>}
    {initial === undefined && <>
      {offer && <div className="catalog-id-suggestion">
        <small>Sugerencia: <strong>{offer.sku}</strong>. Se valida al guardar.</small>
        <div className="catalog-id-actions">
          <button type="button" className="button-secondary" onClick={() => {
            setValue(offer.sku);
            onApply();
            input.current?.focus();
          }}>Usar sugerencia</button>
          <button type="button" className="button-secondary" onClick={() => setAfter(offer.ordinal)}>Otra sugerencia</button>
        </div>
      </div>}
      {visible?.error && <small role="alert">{visible.error} <button type="button" className="button-secondary"
        onClick={() => { setAfter(0); setAttempt(n => n + 1); }}>Consultar de nuevo</button></small>}
      {!visible && enabled && <small role="status">Consultando SKU sugerido…</small>}
    </>}
  </div>;
}

export function IdentifierHistory({ scope }: { scope: number }) {
  const [kind, setKind] = useState<'sku' | 'barcode'>('sku');
  const [value, setValue] = useState('');
  const [items, setItems] = useState<IdentifierEvent[]>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    setItems(undefined);
    setError('');
    setLoading(false);
    return () => { generation.current++; };
  }, [scope, kind, value]);
  async function search() {
    if (!value.trim() || loading) return;
    const request = ++generation.current;
    setLoading(true);
    setError('');
    try {
      const result = await api.identifierHistory(kind, value.trim());
      if (generation.current === request) setItems(result.items);
    } catch (cause) {
      if (generation.current === request) setError(message(cause));
    } finally {
      if (generation.current === request) setLoading(false);
    }
  }
  return <details className="catalog-identifier-history">
    <summary>Consultar identificadores retirados</summary>
    <div className="catalog-id-actions">
      <label>Tipo<select value={kind} onChange={event => setKind(event.target.value as typeof kind)}>
        <option value="sku">SKU</option><option value="barcode">Código de barras</option>
      </select></label>
      <label>Identificador exacto<input type="search" value={value} maxLength={80} onChange={event => setValue(event.target.value)}
        onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); void search(); } }} /></label>
      <button type="button" className="button-secondary" disabled={loading || !value.trim()} onClick={() => void search()}>Consultar historial</button>
    </div>
    {loading && <p role="status">Consultando historial…</p>}
    {error && <p role="alert">{error}</p>}
    {items?.length === 0 && <p role="status">Sin eventos registrados para este identificador.</p>}
    {!!items?.length && <ul className="catalog-master-list">{items.map((item, index) =>
      <li key={item.createdAt + item.variantId + index}><span>
        <strong>{item.value} · {item.action === 'retired' ? 'Retirado' : 'Reutilizado'}{item.internal ? ' · Interno' : ''}</strong>
        <small>Variante {item.variantId} · {new Date(item.createdAt).toLocaleDateString('es-AR')}</small>
        <small>{item.reason}</small>
      </span></li>)}</ul>}
  </details>;
}

export function CatalogIdentifierSettings({ active, scope, protect }: {
  active: boolean; scope: number; protect: RefObject<(() => boolean) | null>;
}) {
  const [values, setValues] = useState<IdentifierSettings>();
  const [draft, setDraft] = useState<IdentifierSettings>();
  const [error, setError] = useState('');
  const [errorField, setErrorField] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [needsRefresh, setNeedsRefresh] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const saving = useRef(false);
  const currentScope = useRef(scope);
  const mounted = useRef(false);
  const id = useId();
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const dirty = !!draft && !!values && (draft.skuPrefix !== values.skuPrefix || draft.skuWidth !== values.skuWidth);
  useEffect(() => {
    protect.current = () => !saving.current && (!dirty || window.confirm('Hay cambios sin guardar en la numeración. ¿Descartarlos?'));
    return () => { protect.current = null; };
  }, [dirty, protect]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  useEffect(() => {
    currentScope.current = scope;
    if (!active) return;
    let current = true;
    void api.identifierSettings().then(settings => {
      if (!current) return;
      setValues(settings);
      setDraft(old => old ?? settings);
    }).catch((cause: unknown) => { if (current) setError(message(cause)); });
    return () => { current = false; };
  }, [active, scope, attempt]);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!draft || saving.current || needsRefresh) return;
    saving.current = true;
    setBusy(true);
    setError('');
    setErrorField('');
    setNotice('');
    try {
      const saved = await api.saveIdentifierSettings(draft);
      if (!mounted.current || scope !== currentScope.current) return;
      setValues(saved);
      setDraft(saved);
      setNotice('Numeración actualizada.');
    } catch (cause) {
      if (!mounted.current || scope !== currentScope.current) return;
      setError(message(cause));
      if (cause instanceof ApiError && cause.field) setErrorField(cause.field);
      if (!(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500 || cause.code === 'revision_conflict') {
        setNeedsRefresh(true);
        setValues(undefined);
      }
    } finally {
      saving.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  return <section className="catalog-identifiers-settings" aria-label="Numeración de variantes">
    <p>Configurá el formato de los SKU sugeridos. La secuencia sigue avanzando aunque cambies el prefijo.</p>
    {error && <p id={id} role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {!values && !error && <p role="status">Cargando numeración…</p>}
    {(!values || needsRefresh) && <button type="button" className="button-secondary" onClick={() => setAttempt(n => n + 1)}>Consultar numeración</button>}
    {values && draft && needsRefresh && <p>Servidor: prefijo {values.skuPrefix || 'vacío'}, {values.skuWidth} dígitos.{' '}
      <button type="button" className="button-secondary" onClick={() => {
        setDraft(old => old ? { ...old, revision: values.revision } : values);
        setNeedsRefresh(false);
        setError('');
      }}>Conservar mis cambios</button>
    </p>}
    {draft && <form aria-label="Configurar numeración" onSubmit={event => void save(event)}>
      <div className="catalog-id-actions">
        <label>Prefijo del SKU (opcional)<input maxLength={12} value={draft.skuPrefix}
          aria-invalid={errorField === 'skuPrefix'} aria-describedby={errorField === 'skuPrefix' ? id : undefined}
          onChange={event => setDraft({ ...draft, skuPrefix: event.target.value })} /></label>
        <label>Dígitos<input type="number" min={4} max={12} value={draft.skuWidth} required
          aria-invalid={errorField === 'skuWidth'} aria-describedby={errorField === 'skuWidth' ? id : undefined}
          onChange={event => setDraft({ ...draft, skuWidth: Number(event.target.value) })} /></label>
      </div>
      <button disabled={busy || needsRefresh || !values}>Guardar numeración</button>
    </form>}
  </section>;
}
