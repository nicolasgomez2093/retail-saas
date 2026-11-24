import { Fragment, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type FormEvent, type RefObject } from 'react';
import {
  api, ApiError, type CatalogAttribute, type Classifications, type ClassificationKind, type PriceConfiguration, type QuickProductInput,
  type QuickProductResult, type StockLocation, type Store, type VariantBatchIssue,
} from '../../shared/api/client';
import { ClassificationFields, type ClassificationReadStates } from './Classifications';
import { LabelDialog } from './LabelDialog';
import {
  DEFAULT_COLORS, QUICK_CONFIRM_THRESHOLD, QUICK_VARIANT_LIMIT, TEMPLATES, attributeId, barcodePattern, combinationCount,
  combinations, formatAmount, fromTemplate, orderedValues, parsePrice, parseQuantity, sameText, variantLabel,
  type AttributeRole, type Combination, type DraftAttribute,
} from './quickProduct';
import './quick-create.css';
import { productHandoff } from '../purchases/productHandoff';

type Mode = 'simple' | 'variants';
type RowDraft = { quantity: string; price: string; priceCustom: boolean; barcodeMode: 'internal' | 'existing'; code: string; sku: string };
type CodeCheck = { state: 'checking' | 'free' | 'taken' | 'unknown'; owner?: string };
type Attempt = { key: string; input: QuickProductInput; rowKeys: string[]; then: 'finish' | 'labels' };
type Phase = 'edit' | 'saving' | 'uncertain' | 'done';
type Read<T> = { status: 'loading' | 'ready' | 'error' | 'denied'; data?: T };
type Masters = Partial<Record<'sizes' | 'colors' | 'size-groups', CatalogAttribute[]>>;

const blankDraft: RowDraft = { quantity: '', price: '', priceCustom: false, barcodeMode: 'internal', code: '', sku: '' };
const roleLabel: Record<AttributeRole, string> = { size: 'Talle o medida principal', color: 'Color', option: 'Atributo adicional' };
const SIMPLE = 'simple';

function newKey() {
  return 'alta-' + (crypto.randomUUID?.() ?? Date.now().toString(36) + Math.random().toString(36).slice(2));
}

function roleFor(name: string, attributes: DraftAttribute[]): AttributeRole {
  if (/^colou?r/i.test(name.trim()) && !attributes.some(item => item.role === 'color')) return 'color';
  if (/^(talle|n[uú]mero|cintura|medida)/i.test(name.trim()) && !attributes.some(item => item.role === 'size')) return 'size';
  return 'option';
}

function readError(cause: unknown) {
  return cause instanceof ApiError && (cause.status === 401 || cause.status === 403) ? 'denied' as const : 'error' as const;
}

export function QuickProductCreate({ branch, stores, classifications, classificationStates, onRetryClassification, masters, protect, onCancel, onOpenProduct, onCreated }: {
  branch?: Store; stores: Store[]; classifications: Classifications; classificationStates?: ClassificationReadStates;
  onRetryClassification?: (kind: ClassificationKind, button: HTMLButtonElement) => void; masters?: Masters;
  protect: RefObject<(() => boolean) | null>; onCancel: () => void; onOpenProduct: (id: string) => void; onCreated: () => void;
}) {
  const uid = useId();
  // A purchase in progress is waiting for this product (see productHandoff).
  useSyncExternalStore(productHandoff.subscribe, productHandoff.version);
  const forPurchase = productHandoff.active();
  const heading = useRef<HTMLHeadingElement>(null);
  const form = useRef<HTMLFormElement>(null);
  const reviewDialog = useRef<HTMLDialogElement>(null);
  const running = useRef(false);
  const [name, setName] = useState('');
  const [mode, setMode] = useState<Mode>('variants');
  const colorSuggestions = useMemo(() => {
    const own = (masters?.colors ?? []).filter(item => item.status === 'active').sort((a, b) => a.position - b.position).map(item => item.name);
    return [...own, ...DEFAULT_COLORS.filter(value => !own.some(item => sameText(item, value)))];
  }, [masters]);
  const [template, setTemplate] = useState('ropa');
  const [attributes, setAttributes] = useState<DraftAttribute[]>(() => fromTemplate(TEMPLATES[0], DEFAULT_COLORS));
  const [drafts, setDrafts] = useState<Record<string, RowDraft>>({});
  const [removed, setRemoved] = useState<string[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [generalPrice, setGeneralPrice] = useState('');
  const [generalQuantity, setGeneralQuantity] = useState('');
  const [advanced, setAdvanced] = useState(false);
  const [confirmLarge, setConfirmLarge] = useState(false);
  const [newAttribute, setNewAttribute] = useState('');
  const [customValues, setCustomValues] = useState<Record<string, string>>({});
  const [checks, setChecks] = useState<Record<string, CodeCheck>>({});
  const [issues, setIssues] = useState<Record<string, VariantBatchIssue[]>>({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState('');
  const [phase, setPhase] = useState<Phase>('edit');
  const [attempt, setAttempt] = useState<Attempt>();
  const [result, setResult] = useState<QuickProductResult>();
  const [labelsOpen, setLabelsOpen] = useState(false);
  const labelOrigin = useRef<HTMLElement | null>(null);
  const [locations, setLocations] = useState<Read<{ items: StockLocation[]; canAdjust: boolean }>>({ status: 'loading' });
  const [locationId, setLocationId] = useState('');
  const [pricing, setPricing] = useState<Read<PriceConfiguration>>({ status: 'loading' });
  const [reviewing, setReviewing] = useState(false);
  // Moves focus to the first invalid control once React has rendered the errors.
  const [focusInvalid, setFocusInvalid] = useState(0);
  useEffect(() => {
    if (focusInvalid) form.current?.querySelector<HTMLElement>('input[aria-invalid="true"],select[aria-invalid="true"],[aria-invalid="true"]')?.focus();
  }, [focusInvalid]);

  // Colors offered by the template follow the commerce's own catalog.
  useEffect(() => {
    setAttributes(current => current.map(item => item.role === 'color' ? { ...item, suggestions: colorSuggestions } : item));
  }, [colorSuggestions]);

  useLayoutEffect(() => { heading.current?.focus(); }, []);

  useEffect(() => {
    if (!branch) return;
    let current = true;
    setLocations({ status: 'loading' });
    api.locations(branch.id).then(page => {
      if (!current) return;
      setLocations({ status: 'ready', data: { items: page.items, canAdjust: page.canAdjust } });
      setLocationId(value => page.items.some(item => item.id === value) ? value
        : (page.items.find(item => item.isSellable) ?? page.items[0])?.id ?? '');
    }).catch(cause => { if (current) setLocations({ status: readError(cause) }); });
    return () => { current = false; };
  }, [branch]);

  useEffect(() => {
    let current = true;
    api.priceLists().then(data => { if (current) setPricing({ status: 'ready', data }); })
      .catch(cause => { if (current) setPricing({ status: readError(cause) }); });
    return () => { current = false; };
  }, []);

  const assignment = pricing.data?.stores.find(store => store.id === branch?.id);
  const priceList = pricing.data?.lists.find(list => list.id === assignment?.listId);
  const sharedWith = assignment?.listId ? (pricing.data?.stores ?? []).filter(store => store.listId === assignment.listId && store.id !== branch?.id) : [];
  const canPrice = pricing.status === 'ready' && Boolean(priceList);
  const location = locations.data?.items.find(item => item.id === locationId);
  const canStock = locations.status === 'ready' && Boolean(locations.data?.canAdjust) && Boolean(location);
  const currency = pricing.data?.currency ?? 'ARS';

  const total = mode === 'simple' ? 1 : combinationCount(attributes);
  const tooMany = total > QUICK_VARIANT_LIMIT;
  const rows: Combination[] = useMemo(() => mode === 'simple'
    ? [{ key: SIMPLE, values: [], label: 'Producto simple' }]
    : tooMany ? [] : combinations(attributes).filter(row => !removed.includes(row.key)), [mode, attributes, removed, tooMany]);
  const draft = (key: string): RowDraft => drafts[key] ?? blankDraft;
  const effectivePrice = (key: string) => draft(key).priceCustom ? draft(key).price : generalPrice;

  const rowState = rows.map(row => {
    const d = draft(row.key);
    const priceText = effectivePrice(row.key);
    const price = canPrice ? parsePrice(priceText) : null;
    const quantity = canStock ? parseQuantity(d.quantity) : 0;
    const code = d.code.trim();
    return { row, draft: d, priceText, price, quantity, code, check: d.barcodeMode === 'existing' ? checks[code] : undefined };
  });
  const codeOwners = new Map<string, string>();
  const problems: Record<string, string[]> = {};
  for (const item of rowState) {
    const list: string[] = [];
    if (item.price === undefined) list.push('Revisá el precio.');
    if (item.quantity === undefined) list.push('Ingresá una cantidad entera entre 0 y 100.000.');
    if (item.draft.barcodeMode === 'existing') {
      if (!barcodePattern.test(item.code)) list.push('Escaneá o escribí el código existente.');
      else if (codeOwners.has(item.code)) list.push('El código repite el de ' + codeOwners.get(item.code) + '.');
      else if (item.check?.state === 'taken') list.push('El código ya pertenece a ' + item.check.owner + '.');
      codeOwners.set(item.code, item.row.label);
    }
    if (item.draft.sku.trim().length > 80) list.push('El SKU admite hasta 80 caracteres.');
    for (const issue of issues[item.row.key] ?? []) list.push(issue.message);
    if (list.length) problems[item.row.key] = list;
  }
  const units = rowState.reduce((sum, item) => sum + (item.quantity ?? 0), 0);
  const priced = rowState.filter(item => typeof item.price === 'string').length;
  const existingCodes = rowState.filter(item => item.draft.barcodeMode === 'existing').length;
  const missingValues = mode === 'variants' && attributes.some(item => !orderedValues(item).length);
  const dirty = phase !== 'done' && (Boolean(name.trim()) || Object.keys(drafts).length > 0 || Boolean(generalPrice) || phase === 'uncertain');

  useEffect(() => {
    protect.current = () => !running.current && (!dirty || window.confirm(phase === 'uncertain'
      ? 'El producto podría haberse guardado. ¿Salir sin confirmar el resultado del intento?'
      : 'Hay datos del producto sin guardar. ¿Descartarlos?'));
    return () => { protect.current = null; };
  }, [dirty, phase, protect]);

  function updateDraft(key: string, change: Partial<RowDraft>) {
    setDrafts(current => ({ ...current, [key]: { ...(current[key] ?? blankDraft), ...change } }));
    setIssues(current => { const next = { ...current }; delete next[key]; return next; });
  }

  function chooseTemplate(id: string) {
    const chosen = TEMPLATES.find(item => item.id === id);
    if (!chosen) return;
    setTemplate(id);
    setAttributes(fromTemplate(chosen, colorSuggestions));
    setRemoved([]);
    setSelected([]);
    setConfirmLarge(false);
  }

  function updateAttribute(id: string, change: (item: DraftAttribute) => DraftAttribute) {
    setAttributes(current => current.map(item => item.id === id ? change(item) : item));
    setConfirmLarge(false);
  }

  function toggleValue(attribute: DraftAttribute, value: string) {
    updateAttribute(attribute.id, item => ({
      ...item, selected: item.selected.some(entry => sameText(entry, value))
        ? item.selected.filter(entry => !sameText(entry, value)) : [...item.selected, value],
    }));
  }

  function addValue(attribute: DraftAttribute) {
    const values = (customValues[attribute.id] ?? '').split(/[,;]/).map(value => value.trim()).filter(Boolean);
    if (!values.length) return;
    updateAttribute(attribute.id, item => {
      const next = [...item.selected];
      for (const value of values) if (value.length <= 60 && !next.some(entry => sameText(entry, value))) next.push(value);
      return { ...item, selected: next };
    });
    setCustomValues(current => ({ ...current, [attribute.id]: '' }));
  }

  function addAttribute() {
    const text = newAttribute.trim();
    if (!text || attributes.length >= 4 || attributes.some(item => sameText(item.name, text))) return;
    setAttributes(current => [...current, { id: attributeId(), name: text.slice(0, 40), role: roleFor(text, current),
      suggestions: roleFor(text, current) === 'color' ? colorSuggestions : [], selected: [] }]);
    setNewAttribute('');
  }

  // Rows follow the general price unless a person typed their own; applying
  // returns the chosen rows (or all of them) to the general price.
  function applyPrice() {
    const keys = selected.length ? selected : rows.map(row => row.key);
    setDrafts(current => {
      const next = { ...current };
      for (const key of keys) next[key] = { ...(next[key] ?? blankDraft), price: '', priceCustom: false };
      return next;
    });
  }

  function applyQuantity() {
    const keys = selected.length ? selected : rows.map(row => row.key);
    setDrafts(current => {
      const next = { ...current };
      for (const key of keys) next[key] = { ...(next[key] ?? blankDraft), quantity: generalQuantity };
      return next;
    });
  }

  function checkCode(raw: string) {
    const code = raw.trim();
    if (!barcodePattern.test(code) || checks[code]) return;
    setChecks(current => ({ ...current, [code]: { state: 'checking' } }));
    api.barcode(code).then(match => setChecks(current => ({ ...current, [code]: {
      state: 'taken', owner: match.product.name + ' · ' + (variantLabel(match.variant) || 'sin atributos') + ' · SKU ' + match.variant.sku,
    } }))).catch(cause => setChecks(current => ({ ...current, [code]: {
      state: cause instanceof ApiError && cause.status === 404 ? 'free' : 'unknown',
    } })));
  }

  function focusNextScanner(from: string) {
    const inputs = Array.from(form.current?.querySelectorAll<HTMLInputElement>('input[data-scanner]') ?? []);
    const index = inputs.findIndex(input => input.dataset.scanner === from);
    inputs[index + 1]?.focus();
  }

  function buildInput(): QuickProductInput | undefined {
    const errors: Record<string, string> = {};
    if (!name.trim()) errors.name = 'Ingresá el nombre del producto.';
    else if (Array.from(name.trim()).length > 160) errors.name = 'El nombre admite hasta 160 caracteres.';
    if (!branch) errors.store = 'Elegí una sucursal en el encabezado.';
    if (mode === 'variants') {
      if (!attributes.length) errors.attributes = 'Agregá al menos un atributo o elegí Producto simple.';
      else if (missingValues) errors.attributes = 'Elegí al menos un valor en cada atributo, o quitá el atributo que no uses.';
      else if (tooMany) errors.attributes = 'Reducí la selección a ' + QUICK_VARIANT_LIMIT + ' variantes o menos.';
      else if (!rows.length) errors.attributes = 'No quedan combinaciones para crear.';
      else if (rows.length > QUICK_CONFIRM_THRESHOLD && !confirmLarge) errors.attributes = 'Confirmá que querés crear ' + rows.length + ' variantes.';
    }
    if (units > 0 && !canStock) errors.stock = 'Elegí la ubicación donde ingresa la mercadería.';
    setFieldErrors(errors);
    if (Object.keys(errors).length || Object.keys(problems).length) {
      setError(Object.keys(problems).length ? 'Revisá las variantes marcadas antes de guardar.' : Object.values(errors)[0]);
      setFocusInvalid(value => value + 1);
      return undefined;
    }
    const data = new FormData(form.current ?? undefined);
    const optional = (field: string) => String(data.get(field) ?? '') || null;
    return {
      product: { name: name.trim(), description: String(data.get('description') ?? '').trim(), categoryId: optional('categoryId'), brandId: optional('brandId') },
      storeId: branch!.id,
      locationId: canStock ? locationId : '',
      attributes: mode === 'simple' ? [] : attributes.map(item => ({ name: item.name, role: item.role })),
      variants: rowState.map(item => ({
        values: item.row.values, sku: advanced ? item.draft.sku.trim() : '',
        barcode: item.draft.barcodeMode === 'existing' ? { mode: 'existing', code: item.code } : { mode: 'internal', code: '' },
        price: typeof item.price === 'string' ? item.price : null, quantity: item.quantity ?? 0,
      })),
    };
  }

  function review(event?: FormEvent) {
    event?.preventDefault();
    setError('');
    const input = buildInput();
    if (!input) return;
    setReviewing(true);
    requestAnimationFrame(() => reviewDialog.current?.showModal());
  }

  function closeReview() {
    reviewDialog.current?.close();
    setReviewing(false);
  }

  async function send(next: Attempt) {
    if (running.current) return;
    running.current = true;
    setAttempt(next);
    setPhase('saving');
    setError('');
    try {
      const saved = await api.quickCreateProduct(next.input, next.key);
      closeReview();
      setResult(saved);
      setPhase('done');
      setAttempt(undefined);
      onCreated();
      if (next.then === 'labels') setLabelsOpen(true);
      requestAnimationFrame(() => heading.current?.focus());
    } catch (cause) {
      const unknown = !(cause instanceof ApiError) || cause.status === 0 || cause.status === 408 ||
        (cause.status >= 500 && cause.code !== 'quick_product_invalid') || cause.code === 'idempotency_in_progress';
      closeReview();
      if (unknown) {
        setPhase('uncertain');
        setError('No pudimos confirmar si el producto se guardó.');
        return;
      }
      setPhase('edit');
      setAttempt(undefined);
      if (cause.rows?.length) {
        const mapped: Record<string, VariantBatchIssue[]> = {};
        for (const issue of cause.rows) {
          const key = next.rowKeys[issue.row - 1];
          if (key) (mapped[key] ??= []).push(issue);
        }
        setIssues(mapped);
        setError('El producto no se guardó y no quedó nada registrado. Corregí las variantes marcadas.');
      } else {
        if (cause.field) setFieldErrors({ [cause.field === 'name' ? 'name' : cause.field]: cause.message });
        setError(cause.message + ' No quedó nada registrado.');
      }
      setFocusInvalid(value => value + 1);
    } finally {
      running.current = false;
    }
  }

  function save(then: Attempt['then']) {
    const input = buildInput();
    if (!input) { closeReview(); return; }
    void send({ key: newKey(), input, rowKeys: rows.map(row => row.key), then });
  }

  function restart() {
    setName(''); setMode('variants'); chooseTemplate('ropa'); setDrafts({}); setGeneralPrice(''); setGeneralQuantity('');
    setIssues({}); setFieldErrors({}); setError(''); setResult(undefined); setPhase('edit'); setChecks({});
    requestAnimationFrame(() => heading.current?.focus());
  }

  const locked = phase === 'saving' || phase === 'uncertain';
  const savingLabel = 'Guardando producto, variantes, precios y mercadería…';

  if (phase === 'done' && result) {
    const internal = result.variants.filter(item => item.variant.internalBarcode && item.variant.barcodes[0] === item.variant.internalBarcode).length;
    const store = stores.find(item => item.id === result.storeId);
    const resultLocation = locations.data?.items.find(item => item.id === result.locationId);
    const pricedCount = result.variants.filter(item => item.price !== null).length;
    return <section className="qa-create" aria-labelledby={uid + 'done'}>
      <nav className="qa-breadcrumb" aria-label="Ruta"><button type="button" className="qa-link" onClick={onCancel}>Productos</button> / {result.product.name}</nav>
      <div className="qa-card qa-result">
        <div className="qa-panelhead qa-success">
          <span aria-hidden="true">✓</span>
          <div><h2 id={uid + 'done'} tabIndex={-1} ref={heading}>Producto creado correctamente</h2>
            <p>{result.product.name} · Todo se guardó en una sola operación.</p></div>
        </div>
        <div className="qa-panelbody">
          <dl className="qa-facts">
            <div><dt>Variantes</dt><dd>{result.variants.length}</dd></div>
            <div><dt>Stock registrado</dt><dd>{result.units} {result.units === 1 ? 'unidad' : 'unidades'}</dd></div>
            <div><dt>Precios configurados</dt><dd>{pricedCount} de {result.variants.length}</dd></div>
            <div><dt>Códigos asignados</dt><dd>{result.variants.length}{internal ? ' · ' + internal + ' internos Code 128' : ''}</dd></div>
            <div><dt>Sucursal</dt><dd>{store?.name ?? 'Sucursal'}{resultLocation ? ' · ' + resultLocation.name : ''}</dd></div>
          </dl>
          <div className="qa-table-wrap">
            <table className="qa-result-table">
              <thead><tr><th scope="col">Variante</th><th scope="col">SKU</th><th scope="col">Código de barras</th><th scope="col">Precio</th><th scope="col">Cantidad</th></tr></thead>
              <tbody>{result.variants.map(item => <tr key={item.variant.id}>
                <th scope="row">{variantLabel(item.variant) || 'Producto simple'}</th>
                <td>{item.variant.sku}</td>
                <td className="qa-code">{item.variant.barcodes[0] ?? '—'}</td>
                <td>{item.price === null ? 'Sin precio' : result.currency + ' ' + formatAmount(item.price)}</td>
                <td>{item.quantity}</td>
              </tr>)}</tbody>
            </table>
          </div>
          <div className="qa-note">La impresión de etiquetas es opcional y no modifica el stock. Los códigos quedan fijos: podés reimprimirlos cuando quieras desde la ficha, en Etiquetas.</div>
          {forPurchase && <div className="qa-note"><p>El pedido de compra en curso está esperando este producto. Al volver, sus variantes se agregan como renglones del pedido.</p></div>}
          <div className="qa-actions">
            {forPurchase && <button type="button" className="qa-btn primary" onClick={() => {
              protect.current = null;
              productHandoff.deliver(result);
              window.location.hash = '#/compras/ordenes';
            }}>Agregar al pedido y volver</button>}
            <button type="button" className={'qa-btn' + (forPurchase ? '' : ' primary')} onClick={event => { labelOrigin.current = event.currentTarget; setLabelsOpen(true); }}>🖨 Preparar etiquetas</button>
            <button type="button" className="qa-btn" onClick={() => onOpenProduct(result.product.id)}>Ver ficha del producto</button>
            <button type="button" className="qa-btn" onClick={restart}>＋ Crear otro producto</button>
            <button type="button" className="qa-btn" onClick={onCancel}>Terminar</button>
          </div>
        </div>
      </div>
      {labelsOpen && result.variants.length > 0 && <LabelDialog
        key={result.product.id}
        data={{ product: result.product, variant: result.variants[0].variant }}
        additional={result.variants.slice(1).map(item => ({ product: result.product, variant: item.variant }))}
        initialCounts={Object.fromEntries(result.variants.map(item => [item.variant.id, String(item.quantity)]))}
        countsHint="Sugerimos una etiqueta por unidad ingresada en el alta."
        storeId={result.storeId} returnTo={labelOrigin.current} onClose={() => setLabelsOpen(false)} />}
    </section>;
  }

  const rowIssue = (key: string) => problems[key];
  const selectionLabel = selected.length ? 'a ' + selected.length + (selected.length === 1 ? ' seleccionada' : ' seleccionadas') : 'a todas';
  const priceHelp = generalPrice && parsePrice(generalPrice) ? currency + ' ' + formatAmount(parsePrice(generalPrice)!) : '';

  return <section className="qa-create" aria-labelledby={uid + 'title'}>
    <nav className="qa-breadcrumb" aria-label="Ruta"><button type="button" className="qa-link" disabled={locked} onClick={onCancel}>Productos</button> / Nuevo producto</nav>
    <header className="qa-head">
      <div><h2 id={uid + 'title'} tabIndex={-1} ref={heading}>Crear producto</h2>
        <p>Carga rápida · {branch?.name ?? 'Elegí una sucursal'}</p></div>
    </header>
    {forPurchase && <div className="qa-note" role="status">
      <p>Estás creando un producto para el pedido de compra en curso. Podés dejar las cantidades vacías: el stock ingresa cuando recibas la mercadería.</p>
      <div className="qa-actions"><button type="button" className="qa-btn" disabled={locked} onClick={() => {
        productHandoff.cancel();
        window.location.hash = '#/compras/ordenes';
      }}>Volver al pedido sin crear</button></div>
    </div>}
    {error && <p className={'qa-alert' + (phase === 'uncertain' ? ' warn' : '')} role="alert">{error}</p>}
    {phase === 'uncertain' && attempt && <div className="qa-note warn" role="status">
      <p>Puede que la conexión se haya cortado después de guardar. Reintentá el mismo guardado: si ya se había registrado, vas a ver el resultado original sin duplicar el producto ni el stock.</p>
      <div className="qa-actions">
        <button type="button" className="qa-btn primary" onClick={() => void send(attempt)}>Reintentar el mismo guardado</button>
        <button type="button" className="qa-btn" onClick={onCancel}>Volver a Productos para verificar</button>
      </div>
    </div>}
    <form ref={form} className="qa-layout" onSubmit={review} noValidate>
      <fieldset className="qa-main" disabled={locked}>
        <div className="qa-card">
          <div className="qa-panelhead"><h3>Datos del producto</h3><p>Lo mínimo para empezar a vender</p></div>
          <div className="qa-panelbody">
            <div className="qa-formgrid">
              <label className="wide">Nombre del producto *
                <input value={name} maxLength={160} placeholder="Ej. Remera básica de algodón" aria-invalid={Boolean(fieldErrors.name)}
                  aria-describedby={fieldErrors.name ? uid + 'name-error' : undefined}
                  onChange={event => { setName(event.target.value); setFieldErrors(current => { const next = { ...current }; delete next.name; return next; }); }} />
                {fieldErrors.name && <span className="qa-field-error" id={uid + 'name-error'}>{fieldErrors.name}</span>}
              </label>
              <div className="wide" role="radiogroup" aria-label="Tipo de producto">
                <span className="qa-label">Tipo de producto</span>
                <div className="qa-segment">
                  <button type="button" role="radio" aria-checked={mode === 'variants'} onClick={() => setMode('variants')}>
                    <b>Con variantes</b><small>Talles, colores, números u otros</small></button>
                  <button type="button" role="radio" aria-checked={mode === 'simple'} onClick={() => setMode('simple')}>
                    <b>Producto simple</b><small>Billetera, cartera, accesorio</small></button>
                </div>
              </div>
            </div>
            <details className="qa-more">
              <summary>Más detalles <small>Descripción, categoría y marca (opcionales)</small></summary>
              <div className="qa-formgrid">
                <label className="wide">Descripción<textarea name="description" maxLength={2000} rows={2} /></label>
                <ClassificationFields values={classifications} states={classificationStates} onRetry={onRetryClassification} />
              </div>
              <p className="qa-hint">La foto se agrega desde la ficha del producto, una vez creado. El costo se registra al recepcionar compras.</p>
            </details>
          </div>
        </div>

        {mode === 'variants' && <div className="qa-card">
          <div className="qa-panelhead"><h3>Variantes</h3><p>Elegí una plantilla y marcá los valores. Las combinaciones se generan solas.</p></div>
          <div className="qa-panelbody">
            <div className="qa-templates" role="radiogroup" aria-label="Plantilla de variantes">
              {TEMPLATES.map(item => <button key={item.id} type="button" role="radio" aria-checked={template === item.id} className="qa-template"
                onClick={() => chooseTemplate(item.id)}><b>{item.label}</b><small>{item.hint}</small></button>)}
            </div>
            {attributes.map(attribute => {
              const values = [...attribute.suggestions, ...attribute.selected.filter(value => !attribute.suggestions.some(item => sameText(item, value)))];
              return <fieldset key={attribute.id} className="qa-attribute">
                <legend><b>{attribute.name}</b><small>{roleLabel[attribute.role]} · {attribute.selected.length} {attribute.selected.length === 1 ? 'valor' : 'valores'}</small></legend>
                <div className="qa-attribute-tools">
                  <button type="button" className="qa-link" onClick={() => updateAttribute(attribute.id, item => ({ ...item, selected: [...values] }))}>Todos</button>
                  <button type="button" className="qa-link" onClick={() => updateAttribute(attribute.id, item => ({ ...item, selected: [] }))}>Ninguno</button>
                  <button type="button" className="qa-link danger" onClick={() => setAttributes(current => current.filter(item => item.id !== attribute.id))}>Quitar atributo<span className="pos-sr-only"> {attribute.name}</span></button>
                </div>
                <div className="qa-chips">
                  {values.map(value => <button key={value} type="button" className="qa-chip" aria-pressed={attribute.selected.some(item => sameText(item, value))}
                    onClick={() => toggleValue(attribute, value)}>{value}</button>)}
                </div>
                <div className="qa-inline">
                  <input aria-label={'Agregar valor a ' + attribute.name} placeholder={'Otro valor de ' + attribute.name.toLowerCase() + ' (separá con comas)'}
                    value={customValues[attribute.id] ?? ''} onChange={event => setCustomValues(current => ({ ...current, [attribute.id]: event.target.value }))}
                    onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); addValue(attribute); } }} />
                  <button type="button" className="qa-btn" onClick={() => addValue(attribute)}>Agregar</button>
                </div>
              </fieldset>;
            })}
            {attributes.length < 4 && <div className="qa-inline qa-new-attribute">
              <input aria-label="Nombre del nuevo atributo" placeholder="Nuevo atributo, por ej. Material o Presentación" value={newAttribute}
                onChange={event => setNewAttribute(event.target.value)}
                onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); addAttribute(); } }} />
              <button type="button" className="qa-btn" onClick={addAttribute}>＋ Agregar atributo</button>
            </div>}
            <p className={'qa-count' + (tooMany ? ' danger' : total > QUICK_CONFIRM_THRESHOLD ? ' warn' : '')} role="status" aria-invalid={Boolean(fieldErrors.attributes)}>
              {missingValues || !attributes.length ? 'Marcá al menos un valor en cada atributo para generar las combinaciones.'
                : tooMany ? 'Seleccionaste ' + total + ' combinaciones. Cada alta admite hasta ' + QUICK_VARIANT_LIMIT + ' variantes porque se guarda todo en una sola operación. Reducí la selección o agregá el resto después desde la ficha.'
                : 'Se crearán ' + rows.length + (rows.length === 1 ? ' variante' : ' variantes') + (removed.length ? ' (' + removed.length + ' quitadas)' : '') + '.'}
            </p>
            {!tooMany && rows.length > QUICK_CONFIRM_THRESHOLD && <label className="qa-check">
              <input type="checkbox" checked={confirmLarge} onChange={event => setConfirmLarge(event.target.checked)} />
              Confirmo que quiero crear {rows.length} variantes</label>}
            {fieldErrors.attributes && <p className="qa-field-error">{fieldErrors.attributes}</p>}
          </div>
        </div>}

        <div className="qa-card">
          <div className="qa-panelhead"><h3>{mode === 'simple' ? 'Precio, cantidad y código' : 'Precios, cantidades y códigos'}</h3>
            <p>{canPrice ? 'Lista ' + priceList!.name + ' · ' : ''}{branch?.name ?? 'Sin sucursal'}{location ? ' · ' + location.name : ''}</p></div>
          <div className="qa-panelbody">
            {pricing.status === 'ready' && !priceList && <p className="qa-note warn">La sucursal no tiene una lista de precios asignada. Podés crear el producto sin precios y asignar la lista en Precios.</p>}
            {pricing.status === 'denied' && <p className="qa-note warn">Tu usuario no puede configurar precios. El producto se creará sin precio.</p>}
            {pricing.status === 'error' && <p className="qa-note warn">No pudimos consultar la lista de precios. Podés guardar sin precios o volver a intentarlo más tarde.</p>}
            {canPrice && sharedWith.length > 0 && <p className="qa-note warn">⚠ La lista {priceList!.name} también se usa en {sharedWith.map(item => item.name).join(', ')}. Los precios que cargues valen para esas sucursales.</p>}
            {locations.status === 'ready' && !locations.data?.canAdjust && <p className="qa-note warn">Tu usuario no puede registrar mercadería en esta sucursal. Las cantidades quedan en cero.</p>}
            {locations.status === 'ready' && locations.data?.canAdjust && !locations.data.items.length && <p className="qa-note warn">La sucursal no tiene ubicaciones de stock. Creá una en Inventario para cargar cantidades.</p>}
            {(locations.status === 'error' || locations.status === 'denied') && <p className="qa-note warn">No pudimos consultar las ubicaciones de stock. Podés crear el producto sin cantidades.</p>}
            <div className="qa-formgrid">
              {canPrice && <label>{mode === 'simple' ? 'Precio de venta' : 'Precio general'}
                <span className="qa-money"><span aria-hidden="true">$</span><input inputMode="decimal" value={generalPrice} placeholder="Sin precio"
                  aria-invalid={parsePrice(generalPrice) === undefined}
                  onChange={event => setGeneralPrice(event.target.value)} /></span>
                <small className="qa-hint">{parsePrice(generalPrice) === undefined ? 'Ingresá un importe válido, por ej. 24.500 o 24.500,50.' : priceHelp ? 'Se guardará ' + priceHelp + (mode === 'variants' ? ' en las variantes sin precio propio.' : '.') : 'Vacío = sin precio. Podés cargar 0 si corresponde.'}</small>
              </label>}
              {canStock && locations.data!.items.length > 1 && <label>Ubicación de ingreso
                <select value={locationId} onChange={event => setLocationId(event.target.value)}>
                  {locations.data!.items.map(item => <option key={item.id} value={item.id}>{item.name}{item.isSellable ? '' : ' (no vendible)'}</option>)}
                </select></label>}
              {mode === 'simple' && <SimpleFields draft={draft(SIMPLE)} canStock={canStock} issue={rowIssue(SIMPLE)} check={rowState[0]?.check}
                advanced={advanced} onChange={change => updateDraft(SIMPLE, change)} onCheck={code => checkCode(code)} />}
            </div>
            {mode === 'variants' && rows.length > 0 && <>
              <div className="qa-bulk" role="group" aria-label="Aplicar en bloque">
                {canPrice && <button type="button" className="qa-btn" disabled={parsePrice(generalPrice) === undefined} onClick={applyPrice}>Usar precio general {selected.length ? 'en ' + selectionLabel.slice(2) : 'en todas'}</button>}
                {canStock && <span className="qa-inline">
                  <input aria-label="Cantidad para aplicar" inputMode="numeric" placeholder="Cant." value={generalQuantity} className="qa-qty"
                    onChange={event => setGeneralQuantity(event.target.value)} />
                  <button type="button" className="qa-btn" disabled={parseQuantity(generalQuantity) === undefined || generalQuantity === ''} onClick={applyQuantity}>Aplicar cantidad {selectionLabel}</button>
                </span>}
                {selected.length > 0 && <button type="button" className="qa-btn" onClick={() => { setRemoved(current => [...current, ...selected]); setSelected([]); }}>Quitar {selected.length} {selected.length === 1 ? 'combinación' : 'combinaciones'}</button>}
                {removed.length > 0 && <button type="button" className="qa-link" onClick={() => setRemoved([])}>Restaurar quitadas ({removed.length})</button>}
              </div>
              <div className={'qa-matrix' + (advanced ? ' with-sku' : '')} role="table" aria-label="Variantes a crear">
                <div className="qa-matrix-head" role="row">
                  <span role="columnheader"><input type="checkbox" aria-label="Seleccionar todas" checked={selected.length === rows.length}
                    onChange={event => setSelected(event.target.checked ? rows.map(row => row.key) : [])} /></span>
                  <span role="columnheader">Variante</span>
                  <span role="columnheader">Cantidad</span>
                  <span role="columnheader">Precio</span>
                  {advanced && <span role="columnheader">SKU</span>}
                  <span role="columnheader">Código de barras</span>
                  <span role="columnheader"><span className="pos-sr-only">Quitar</span></span>
                </div>
                {rowState.map(item => {
                  const { row, draft: d } = item;
                  const issue = rowIssue(row.key);
                  return <Fragment key={row.key}>
                    <div className={'qa-matrix-row' + (issue ? ' invalid' : '')} role="row">
                      <span role="cell" className="qa-select"><input type="checkbox" aria-label={'Seleccionar ' + row.label} checked={selected.includes(row.key)}
                        onChange={event => setSelected(current => event.target.checked ? [...current, row.key] : current.filter(key => key !== row.key))} /></span>
                      <span role="cell" className="qa-variant"><b>{row.label}</b>{issue && <small className="qa-field-error">{issue.join(' ')}</small>}</span>
                      <span role="cell" data-label="Cantidad">{canStock ? <input inputMode="numeric" className="qa-qty" aria-label={'Cantidad de ' + row.label} placeholder="0"
                        value={d.quantity} aria-invalid={item.quantity === undefined} onChange={event => updateDraft(row.key, { quantity: event.target.value })} /> : <span className="qa-muted">—</span>}</span>
                      <span role="cell" data-label="Precio">{canPrice ? <span className="qa-price">
                        <input inputMode="decimal" aria-label={'Precio de ' + row.label} placeholder="Sin precio" value={item.priceText}
                          aria-invalid={item.price === undefined} className={d.priceCustom ? 'custom' : ''}
                          onChange={event => updateDraft(row.key, { price: event.target.value, priceCustom: true })} />
                        {d.priceCustom && <button type="button" className="qa-icon" title="Usar el precio general" aria-label={'Usar el precio general en ' + row.label}
                          onClick={() => updateDraft(row.key, { price: '', priceCustom: false })}>↺</button>}
                      </span> : <span className="qa-muted">Sin precio</span>}</span>
                      {advanced && <span role="cell" data-label="SKU"><input aria-label={'SKU de ' + row.label} placeholder="Automático" value={d.sku}
                        onChange={event => updateDraft(row.key, { sku: event.target.value })} /></span>}
                      <span role="cell" data-label="Código" className="qa-code-cell">
                        <select aria-label={'Código de barras de ' + row.label} value={d.barcodeMode}
                          onChange={event => updateDraft(row.key, { barcodeMode: event.target.value as RowDraft['barcodeMode'] })}>
                          <option value="internal">Generar interno</option><option value="existing">Usar existente</option>
                        </select>
                        {d.barcodeMode === 'existing' && <ScanInput id={row.key} label={row.label} value={d.code} check={item.check}
                          onChange={code => updateDraft(row.key, { code })} onCheck={code => checkCode(code)} onNext={() => focusNextScanner(row.key)} />}
                      </span>
                      <span role="cell" className="qa-remove"><button type="button" className="qa-icon" aria-label={'Quitar ' + row.label}
                        onClick={() => { setRemoved(current => [...current, row.key]); setSelected(current => current.filter(key => key !== row.key)); }}>✕</button></span>
                    </div>
                  </Fragment>;
                })}
              </div>
            </>}
            <label className="qa-check"><input type="checkbox" checked={advanced} onChange={event => setAdvanced(event.target.checked)} />
              Escribir SKU manualmente (opcional; si queda vacío se asigna uno automático)</label>
            <p className="qa-hint">El SKU se numera automáticamente y no depende del nombre ni de los atributos. El código interno usa Code 128 y no es un EAN ni un GTIN oficial.</p>
            {fieldErrors.stock && <p className="qa-field-error" aria-invalid="true" tabIndex={-1}>{fieldErrors.stock}</p>}
          </div>
        </div>
      </fieldset>

      <aside className="qa-side" aria-label="Resumen del alta">
        <div className="qa-card qa-sidebox">
          <h3>Resumen del alta</h3>
          {[
            ['Datos del producto', Boolean(name.trim()), name.trim() || 'Falta el nombre'],
            ['Variantes', mode === 'simple' || rows.length > 0, mode === 'simple' ? 'Producto simple' : rows.length + (rows.length === 1 ? ' combinación' : ' combinaciones')],
            ['Precios', canPrice && priced === rows.length && rows.length > 0, !canPrice ? 'Sin precio' : priced + ' de ' + rows.length + ' con precio'],
            ['Mercadería', units > 0, units + (units === 1 ? ' unidad' : ' unidades') + (location ? ' · ' + location.name : '')],
            ['Identificadores', true, 'SKU automático · ' + (rows.length - existingCodes) + ' internos' + (existingCodes ? ' · ' + existingCodes + ' existentes' : '')],
          ].map(([title, done, detail]) => <div className="qa-checkrow" key={String(title)}>
            <span aria-hidden="true" className={done ? 'done' : ''}>{done ? '✓' : '○'}</span>
            <div><b>{title}</b><small>{detail}</small></div>
          </div>)}
          <button type="submit" className="qa-btn primary wide" disabled={locked || !branch}>{phase === 'saving' ? 'Guardando…' : 'Revisar y guardar'}</button>
          <button type="button" className="qa-btn wide" disabled={locked} onClick={onCancel}>Cancelar</button>
          <p className="qa-note">Todo se guarda en una sola operación: si algo falla, no queda nada a medias.</p>
        </div>
      </aside>
    </form>

    {reviewing && <dialog ref={reviewDialog} className="qa-dialog" aria-labelledby={uid + 'review'} onCancel={event => { event.preventDefault(); if (!running.current) closeReview(); }}>
      <header><h2 id={uid + 'review'}>Revisá antes de guardar</h2><button type="button" className="qa-icon" aria-label="Cerrar" disabled={phase === 'saving'} onClick={closeReview}>✕</button></header>
      <div className="qa-dialog-body">
        <p><b>{name.trim()}</b></p>
        <ul className="qa-review-list">
          <li><span>Variantes</span><b>{mode === 'simple' ? '1 (producto simple)' : rows.length}</b></li>
          <li><span>Mercadería</span><b>{units} {units === 1 ? 'unidad' : 'unidades'}{location && units ? ' en ' + (branch?.name ?? '') + ' · ' + location.name : ''}</b></li>
          <li><span>Precios</span><b>{canPrice ? priced + ' de ' + rows.length + ' · Lista ' + priceList!.name : 'Sin precio'}</b></li>
          <li><span>Códigos de barras</span><b>{rows.length - existingCodes} internos{existingCodes ? ' · ' + existingCodes + ' existentes' : ''}</b></li>
          <li><span>SKU</span><b>{advanced && rowState.some(item => item.draft.sku.trim()) ? 'Automáticos y manuales' : 'Automáticos'}</b></li>
        </ul>
        {canPrice && priced < rows.length && <p className="qa-note warn">{rows.length - priced} {rows.length - priced === 1 ? 'variante queda' : 'variantes quedan'} sin precio y no se podrán vender hasta configurarlo.</p>}
        {phase === 'saving' && <p role="status" className="qa-saving"><span className="qa-spinner" aria-hidden="true" />{savingLabel}</p>}
      </div>
      <footer>
        <button type="button" className="qa-btn" disabled={phase === 'saving'} onClick={closeReview}>Volver a editar</button>
        <button type="button" className="qa-btn" disabled={phase === 'saving'} onClick={() => save('finish')}>Guardar y terminar</button>
        <button type="button" className="qa-btn primary" disabled={phase === 'saving'} onClick={() => save('labels')}>Guardar y preparar etiquetas</button>
      </footer>
    </dialog>}
  </section>;
}

function ScanInput({ id, label, value, check, onChange, onCheck, onNext }: {
  id: string; label: string; value: string; check?: CodeCheck; onChange: (code: string) => void; onCheck: (code: string) => void; onNext: () => void;
}) {
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const code = value.trim();
  const status = !code ? 'Enfocá y escaneá con el lector, o escribí el código.' : !barcodePattern.test(code) ? 'Código inválido.'
    : check?.state === 'checking' ? 'Verificando…' : check?.state === 'free' ? '✓ Código disponible' : check?.state === 'taken' ? 'En uso por ' + check.owner
    : check?.state === 'unknown' ? 'No pudimos verificarlo ahora; se validará al guardar.' : '';
  return <span className="qa-scan">
    <input data-scanner={id} aria-label={'Código existente de ' + label} placeholder="Escanear código" value={value} autoComplete="off"
      aria-invalid={Boolean(code) && (!barcodePattern.test(code) || check?.state === 'taken')}
      onChange={event => {
        onChange(event.target.value);
        // Scanners that do not send Enter are confirmed after a short pause.
        window.clearTimeout(timer.current);
        const next = event.target.value;
        timer.current = window.setTimeout(() => onCheck(next), 350);
      }}
      onBlur={() => onCheck(value)}
      onKeyDown={event => {
        if (event.key !== 'Enter') return;
        event.preventDefault();
        onCheck(value);
        if (barcodePattern.test(value.trim())) onNext();
      }} />
    <small className={check?.state === 'taken' || (code && !barcodePattern.test(code)) ? 'qa-field-error' : check?.state === 'free' ? 'qa-ok' : 'qa-hint'} role="status">{status}</small>
  </span>;
}

function SimpleFields({ draft, canStock, issue, check, advanced, onChange, onCheck }: {
  draft: RowDraft; canStock: boolean; issue?: string[]; check?: CodeCheck; advanced: boolean;
  onChange: (change: Partial<RowDraft>) => void; onCheck: (code: string) => void;
}) {
  return <>
    {canStock && <label>Cantidad inicial
      <input inputMode="numeric" placeholder="0" value={draft.quantity} aria-invalid={parseQuantity(draft.quantity) === undefined}
        onChange={event => onChange({ quantity: event.target.value })} /></label>}
    {advanced && <label>SKU<input placeholder="Automático" value={draft.sku} onChange={event => onChange({ sku: event.target.value })} /></label>}
    <div className="wide qa-simple-code">
      <span className="qa-label">Código de barras</span>
      <div className="qa-segment compact" role="radiogroup" aria-label="Código de barras">
        <button type="button" role="radio" aria-checked={draft.barcodeMode === 'internal'} onClick={() => onChange({ barcodeMode: 'internal' })}><b>Generar automáticamente</b><small>Code 128 interno</small></button>
        <button type="button" role="radio" aria-checked={draft.barcodeMode === 'existing'} onClick={() => onChange({ barcodeMode: 'existing' })}><b>Usar código existente</b><small>Del fabricante o etiqueta previa</small></button>
      </div>
      {draft.barcodeMode === 'existing' && <ScanInput id={SIMPLE} label="el producto" value={draft.code} check={check}
        onChange={code => onChange({ code })} onCheck={onCheck} onNext={() => undefined} />}
    </div>
    {issue && <p className="wide qa-field-error" aria-invalid="true">{issue.join(' ')}</p>}
  </>;
}
