import { CommercialProductPanels } from './CommercialProductPanels';
import { Fragment, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type RefObject } from 'react';
import {
  api, ApiError, type BarcodeMatch, type CatalogPage, type CatalogPreview, type ClassificationIDs, type ClassificationKind, type Classifications, type Product, type ProductDetail, type Store, type Term, type Variant, type VariantCreateData,
} from '../../shared/api/client';
import { VariantPrice, type PriceAccess } from '../pricing/VariantPrice';
import { BarcodeScanner, ScannedStock } from './BarcodeScanner';
import { LabelDialog } from './LabelDialog';
import { posPrice } from '../pos/priceDisplay';
import { BulkBar, BulkLabels, BulkPrices, maxSelection, type Selection } from './CatalogBulk';
import { QuickProductCreate } from './QuickProductCreate';
import { CatalogCSV } from './CatalogCSV';
import { ProductQuickView } from './ProductQuickView';
import { AttributePicker, CatalogAttributeTools, useCatalogAttributes } from './CatalogAttributes';
import { CatalogIdentifierSettings, IdentifierHistory, VariantSKU } from './CatalogIdentifiers';
import { VariantMatrix } from './VariantMatrix';
import { ProductPhotos } from './ProductPhotos';
import { ClassificationEditor, ClassificationFields, ClassificationSummary } from './Classifications';

import { catalogLastPage, isCatalogHash, readCatalogQuery, updateCatalogHash } from './catalogQuery';

const emptyClassifications: Classifications = { categories: [], brands: [] };

const fieldNames: Record<string, string> = {
  name: 'Nombre', description: 'Descripción', categoryId: 'Categoría', brandId: 'Marca',
  status: 'Estado', sku: 'SKU', size: 'Talle', color: 'Color', barcodes: 'Códigos de barras',
};
function changedFields<T extends object>(values: T, original: NoInfer<T>): Partial<T> {
  return Object.fromEntries(Object.entries(values).filter(([field, value]) =>
    JSON.stringify(value) !== JSON.stringify(original[field as keyof T]))) as Partial<T>;
}

function snapshot(form: HTMLFormElement | null) {
  return JSON.stringify(Array.from(form?.elements ?? []).flatMap((field) => {
    if (field instanceof HTMLInputElement) return [[field.name, field.type === 'checkbox' ? field.checked : field.value]];
    if (field instanceof HTMLSelectElement || field instanceof HTMLTextAreaElement) return [[field.name, field.value]];
    return [];
  }));
}

type Form = 'product' | 'edit' | 'variant' | 'matrix' | 'quick';

function classificationsFrom(form: FormData): ClassificationIDs {
  const category = form.get('categoryId');
  const brand = form.get('brandId');
  return {
    categoryId: typeof category === 'string' && category ? category : null,
    brandId: typeof brand === 'string' && brand ? brand : null,
  };
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No pudimos completar la operación.';
}

function VariantInfo({ variant }: { variant: Variant }) {
  return (
    <dl className="catalog-data">
      <dt>SKU</dt><dd>{variant.sku}</dd>
      {variant.size && <><dt>Talle</dt><dd>{variant.size}</dd></>}
      {variant.options?.map(option => <Fragment key={option.name}><dt>{option.name}</dt><dd>{option.value}</dd></Fragment>)}
      {variant.color && <><dt>Color</dt><dd>{variant.color}</dd></>}
      {variant.barcodes.length > 0 && <><dt>Códigos de barras</dt><dd>{variant.barcodes.join(', ')}</dd></>}
    </dl>
  );
}

function PreviewPrice({ preview }: { preview?: CatalogPreview }) {
  if (!preview?.currency) return <>—</>;
  if (preview.minPrice === null) return <>Sin precio configurado</>;
  const minimum = preview.currency + '\u00a0' + posPrice(preview.minPrice);
  const maximum = preview.maxPrice ? posPrice(preview.maxPrice) : undefined;
  return <>{minimum}{preview.minPrice !== preview.maxPrice && maximum && ' a ' + maximum}
    {preview.pricePartial && <small className="catalog-partial-price">Algunas variantes sin precio</small>}
  </>;
}

function filterOptions(values: Classifications, categoryId: string, brandId: string): Classifications {
  const withSelected = (terms: Term[], id: string) => id && !terms.some((term) => term.id === id)
    ? [{ id, name: 'Selección actual (sin verificar)' }, ...terms] : terms;
  return { categories: withSelected(values.categories, categoryId), brands: withSelected(values.brands, brandId) };
}

type CatalogContext = { navigation: number; scope: number };
type Access = 'unknown' | 'manage' | 'read' | 'verify' | 'denied' | 'session';

function restoreFilterFocus(button: HTMLButtonElement, field: 'categoryId' | 'brandId') {
  if (document.activeElement !== button) return;
  const filters = button.closest('.products-view')?.querySelectorAll<HTMLSelectElement>('select[name=' + field + ']');
  const target = Array.from(filters ?? []).find((filter) => !filter.disabled && filter.getClientRects().length);
  target?.focus({ preventScroll: true });
}

function useCatalogTerms(kind: ClassificationKind, enabled: boolean, forbidden: boolean,
  scope: number, context: CatalogContext, expire: () => void) {
  const [items, setItems] = useState<Term[]>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [loading, setLoading] = useState(false);
  const [denied, setDenied] = useState(false);
  const generation = useRef(0);
  const onRecovered = useRef<(() => void)>(undefined);
  useLayoutEffect(() => {
    if (forbidden) {
      setItems(undefined);
      setError('');
    }
    if (!enabled || forbidden) { setError(''); setLoading(false); return; }
    setLoading(true); setError(''); setDenied(false);
    const request = ++generation.current;
    const current = () => request === generation.current && context.scope === scope;
    void api.classificationTerms(kind).then((result) => {
      if (!current()) return;
      onRecovered.current?.();
      onRecovered.current = undefined;
      setItems(result.items ?? []);
      setLoading(false); setError('');
    }).catch((cause: unknown) => {
      if (!current()) return;
      setLoading(false);
      if (cause instanceof ApiError && cause.status === 401) expire();
      else { setDenied(cause instanceof ApiError && cause.status === 403); setError('Consulta no disponible.'); }
    });
    return () => { generation.current++; };
  }, [kind, enabled, forbidden, scope, context, expire, attempt]);
  function retry(restoreFocus: () => void) {
    onRecovered.current = restoreFocus;
    generation.current++;
    setAttempt((value) => value + 1);
  }
  function update(term: Term) {
    generation.current++;
    setError('');
    setItems((current) => [...(current ?? []).filter((item) => item.id !== term.id), term]
      .sort((a, b) => a.name.localeCompare(b.name, 'es')));
  }
  return { items, error, retry, update, state: loading ? 'loading' as const : denied ? 'denied' as const : error ? 'error' as const : items ? 'ready' as const : 'loading' as const };
}

export function Catalog({ stores, priceRevision, stockRevision, branch, hash, active, context, navigation, scope, confirmExit, recoveryScope, onInventoryBusy, onInventoryChanged, createRequest = 0 }: {
  stores: Store[] | null; priceRevision: number; stockRevision: number; branch?: Store; hash: string;
  active: boolean; context: CatalogContext; navigation: number; scope: number;
  confirmExit: RefObject<(() => boolean) | null>;
  recoveryScope: string; onInventoryBusy: (busy: boolean) => void; onInventoryChanged: () => void; createRequest?: number;
}) {
  const branchId = branch?.id;
  const ready = stores !== null;
  const parsed = readCatalogQuery(hash);
  const { values, errors, offset } = parsed;
  const { q: query, categoryId, brandId, page: pageNumber } = values;
  const [reload, setReload] = useState(0);
  const listKey = [navigation, scope, priceRevision, stockRevision, reload].join(':');
  const [list, setList] = useState<{ key: string; page?: CatalogPage; error?: string }>();
  const [quickProduct, setQuickProduct] = useState<{ id: string; open: boolean }>();
  // Products chosen for bulk labels or prices; the choice survives searches and pages.
  const [selection, setSelection] = useState<Selection>(() => new Map());
  const [bulk, setBulk] = useState<{ kind: 'labels' | 'prices'; origin: HTMLElement | null } | null>(null);
  const toggleSelected = (id: string, name: string, on: boolean) => setSelection((current) => {
    const next = new Map(current);
    if (on && next.size < maxSelection) next.set(id, name); else next.delete(id);
    return next;
  });
  useLayoutEffect(() => { setQuickProduct((current) => current?.open ? current : undefined); }, [scope, navigation, stockRevision]);
  const [toolsOpen, setToolsOpen] = useState(false);
  const [tool, setTool] = useState<'csv' | 'categories' | 'brands' | 'attributes' | 'identifiers'>('csv');
  const toolsId = useId();
  const toolsButton = useRef<HTMLButtonElement>(null);
  const filterForm = useRef<HTMLFormElement | null>(null);
  const filterFocus = useRef<string | null>(null);
  const attachFilters = useCallback((form: HTMLFormElement | null) => {
    if (!form) {
      const focused = document.activeElement;
      filterFocus.current = filterForm.current?.contains(focused) ? focused?.getAttribute('name') ?? null : null;
    } else if (filterFocus.current && document.activeElement === document.body) {
      const target = form.elements.namedItem(filterFocus.current);
      if (target instanceof HTMLElement) target.focus({ preventScroll: true });
    }
    if (form) filterFocus.current = null;
    filterForm.current = form;
  }, []);
  const [detail, setDetail] = useState<ProductDetail | null>(null);
  const [label, setLabel] = useState<BarcodeMatch | null>(null);
  const [batchLabels, setBatchLabels] = useState(false);
  useEffect(() => { if (!active) setLabel(null); }, [active]);
  const labelOrigin = useRef<HTMLButtonElement>(null);
  const [pricedVariant, setPricedVariant] = useState('');
  const [form, setForm] = useState<Form | null>(null);
  const [workspaceTab, setWorkspaceTab] = useState('general');
  const workspaceTabs = useRef<HTMLElement>(null);
  const [tabEdges, setTabEdges] = useState({ left: false, right: false });
  const measureTabs = useCallback(() => {
    const element = workspaceTabs.current;
    if (element) setTabEdges({ left: element.scrollLeft > 2, right: element.scrollLeft + element.clientWidth < element.scrollWidth - 2 });
  }, []);
  useLayoutEffect(() => {
    const element = workspaceTabs.current;
    if (!element) return;
    const fitSelectedTab = () => {
      const selected = element.querySelector<HTMLElement>('[aria-current="page"]');
      if (selected) {
        const visible = element.getBoundingClientRect();
        const current = selected.getBoundingClientRect();
        if (current.left < visible.left) element.scrollLeft -= visible.left - current.left;
        else if (current.right > visible.right) element.scrollLeft += current.right - visible.right;
      }
      measureTabs();
    };
    fitSelectedTab();
    const observer = new ResizeObserver(fitSelectedTab);
    observer.observe(element);
    return () => observer.disconnect();
  }, [detail?.product.id, measureTabs]);

  const [commercialRevision, setCommercialRevision] = useState(0);
  const consumedCreate = useRef(0);
  const [editedVariant, setEditedVariant] = useState<Variant | null>(null);
  const [reuseRetired, setReuseRetired] = useState(false);
  const pendingVariantCreate = useRef<{ productId: string; key: string; data: VariantCreateData } | null>(null);
  const protectIdentifiers = useRef<(() => boolean) | null>(null);
  const protectMatrix = useRef<(() => boolean) | null>(null);
  const matrixOrigin = useRef<HTMLButtonElement>(null);
  const protectAttributes = useRef<(() => boolean) | null>(null);
  const editorRef = useRef<HTMLFormElement>(null);
  const editorBaseline = useRef('[]');
  const [dirty, setDirty] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [conflict, setConflict] = useState(false);
  const [serverVersion, setServerVersion] = useState<Product | Variant>();
  const [reviewedRevision, setReviewedRevision] = useState<number>();

  useLayoutEffect(() => {
    editorBaseline.current = snapshot(editorRef.current);
    setDirty(false);
    setReuseRetired(false);
    pendingVariantCreate.current = null;
    setFieldErrors({});
    setConflict(false);
    setServerVersion(undefined);
    setReviewedRevision(undefined);
  }, [form, detail?.product.id, editedVariant?.id]);

  function allowDiscard() {
    return (protectMatrix.current?.() ?? true) && (snapshot(editorRef.current) === editorBaseline.current ||
      window.confirm('Hay cambios sin guardar. ¿Querés descartarlos?'));
  }

  useEffect(() => {
    confirmExit.current = () => !operation.current && allowDiscard() && (protectAttributes.current?.() ?? true) && (protectIdentifiers.current?.() ?? true);
    return () => { confirmExit.current = null; };
  }, [confirmExit]);

  useEffect(() => {
    if (!dirty || !form) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty, form]);

  function trackChanges() {
    setDirty(snapshot(editorRef.current) !== editorBaseline.current);
  }

  function clearFieldError(name: string) {
    if (!fieldErrors[name]) return;
    setFieldErrors(previous => {
      const next = { ...previous };
      delete next[name];
      return next;
    });
  }

  function fieldAttributes(name: string) {
    return { 'aria-invalid': Boolean(fieldErrors[name]), 'aria-describedby': fieldErrors[name] ? 'catalog-field-' + name : undefined };
  }

  function fieldError(name: string) {
    return fieldErrors[name] && <small id={'catalog-field-' + name} role="alert">{fieldErrors[name]}</small>;
  }

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => {
    if (!active) { setNotice(''); return; }
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 6000);
    return () => window.clearTimeout(timer);
  }, [active, notice]);
  useEffect(() => {
    if (!active) { setNotice(''); return; }
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 6000);
    return () => window.clearTimeout(timer);
  }, [active, notice]);
  const [uncertain, setUncertain] = useState(false);
  const [access, setAccess] = useState<Access>('unknown');
  const accessRef = useRef<Access>('unknown');
  const permissionGeneration = useRef(0);
  const listGeneration = useRef(0);
  const operation = useRef<symbol | null>(null);
  const mounted = useRef(false);
  const latestListKey = useRef(listKey);
  const viewHeading = useRef<HTMLHeadingElement>(null);
  const listToolbar = useRef<HTMLDivElement>(null);
  const listOrigin = useRef<HTMLButtonElement | null>(null);

  useLayoutEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; operation.current = null; };
  }, []);
  useLayoutEffect(() => { latestListKey.current = listKey; }, [listKey]);

  const applyAccess = useCallback((next: Access) => {
    if (accessRef.current !== next && (next !== 'manage' || accessRef.current === 'denied')) {
      permissionGeneration.current++;
    }
    accessRef.current = next;
    setAccess(next);
    if (next === 'denied' || next === 'session') {
      setDetail(null);
      setForm(null);
      setEditedVariant(null);
      setLabel(null);
      setQuickProduct(undefined);
      setToolsOpen(false);
      setPricedVariant('');
      setUncertain(false);
      setError('');
      setNotice('');
    }
  }, []);

  const priceAccess = useMemo<PriceAccess>(() => {
    const isCurrent = () => active && mounted.current && scope === context.scope &&
      isCatalogHash(window.location.hash) && accessRef.current === access &&
      (access === 'manage' || access === 'read');
    return { isCurrent, onSessionExpired: () => { if (isCurrent()) applyAccess('session'); } };
  }, [active, scope, context, access, applyAccess]);

  // Reads and command handlers share the synchronous clock owned by App.
  const scopeIsCurrent = () => mounted.current && context.scope === scope;
  const routeIsCurrent = () => context.navigation === navigation &&
    window.location.hash === hash && isCatalogHash(hash);

  useEffect(() => {
    if (!ready || !active || errors.length || !scopeIsCurrent() || !routeIsCurrent() || accessRef.current === 'session') return;
    const attempt = ++listGeneration.current;
    const permission = permissionGeneration.current;
    let subscribed = true;
    const current = () => subscribed && scopeIsCurrent() && routeIsCurrent() &&
      latestListKey.current === listKey && listGeneration.current === attempt &&
      permissionGeneration.current === permission && accessRef.current !== 'session';
    void api.products(query, offset, { categoryId: categoryId || null, brandId: brandId || null }, branchId)
      .then((result) => {
        if (!current()) return;
        applyAccess(result.canManage ? 'manage' : 'read');
        setList({ key: listKey, page: result });
      }).catch((cause: unknown) => {
        if (!current()) return;
        if (cause instanceof ApiError && (cause.status === 401 || cause.status === 403)) {
          applyAccess(cause.status === 401 ? 'session' : 'denied');
        }
        setList({ key: listKey, error: errorMessage(cause) });
      });
    return () => { subscribed = false; };
  }, [active, ready, hash, listKey, branchId]);

  function reloadList() {
    listGeneration.current++;
    setReload((value) => value + 1);
  }

  const expireCatalog = useCallback(() => applyAccess('session'), [applyAccess]);
  const attributes = useCatalogAttributes(active && ready && access === 'manage' &&
    (form === 'variant' || form === 'matrix' || form === 'quick' || (toolsOpen && tool === 'attributes')), scope, expireCatalog);
  const forbiddenTerms = access === 'denied' || access === 'session';
  const categories = useCatalogTerms('categories', active && ready, forbiddenTerms, scope, context, expireCatalog);
  const brands = useCatalogTerms('brands', active && ready, forbiddenTerms, scope, context, expireCatalog);
  const terms = Boolean(categories.items && brands.items);
  const classificationStates = { categories: categories.state, brands: brands.state };
  const retryClassification = (kind: ClassificationKind, button: HTMLButtonElement) => (kind === 'categories' ? categories : brands).retry(() => restoreFilterFocus(button, kind === 'categories' ? 'categoryId' : 'brandId'));
  const classifications: Classifications = {
    categories: categories.items ?? emptyClassifications.categories,
    brands: brands.items ?? emptyClassifications.brands,
  };

  async function run(operationFn: (current: () => boolean) => Promise<void>, writing = false) {
    if (operation.current || !scopeIsCurrent() || !routeIsCurrent() ||
        (writing ? accessRef.current !== 'manage' : !['manage', 'read'].includes(accessRef.current))) return;
    const id = Symbol();
    operation.current = id;
    const permission = permissionGeneration.current;
    const current = () => scopeIsCurrent() && operation.current === id &&
      permissionGeneration.current === permission && (writing || routeIsCurrent());
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await operationFn(current);
    } catch (cause) {
      const indeterminate = writing && (!(cause instanceof ApiError) || cause.status === 0 ||
        cause.status === 408 || cause.status >= 500);
      if (current()) {
        if (writing && !indeterminate && form === 'variant' && !editedVariant) setUncertain(false);
        if (cause instanceof ApiError && cause.status === 401) applyAccess('session');
        else if (cause instanceof ApiError && cause.status === 403) {
          applyAccess(writing ? 'verify' : 'denied');
          if (writing) reloadList();
        }
        if (writing && cause instanceof ApiError && cause.code === 'revision_conflict') {
          setConflict(true);
          setReviewedRevision(undefined);
          setServerVersion(undefined);
          setError('Otra persona modificó este registro. Tus cambios se conservaron.');
        } else if (writing && cause instanceof ApiError && cause.code === 'attribute_changed') {
          attributes.retry();
          setError(cause.message);
        } else if (writing && cause instanceof ApiError && (cause.field || cause.code === 'sku_taken' || cause.code === 'barcode_taken')) {
          const field = cause.field ?? (cause.code === 'sku_taken' ? 'sku' : 'barcodes');
          setFieldErrors({ [field]: cause.message });
          if (form !== 'variant' || !['sku', 'barcodes', 'reason', 'confirmSensitive', 'size', 'color', 'sizeId', 'colorId'].includes(field)) {
            setError(cause.message);
          }
        } else if (indeterminate) {
          setUncertain(true);
          setError('No pudimos confirmar si se guardó. Revisá el producto antes de intentar nuevamente.');
        } else {
          setError(errorMessage(cause));
        }
      } else if (indeterminate && mounted.current && operation.current === id &&
                 ['manage', 'read', 'verify'].includes(accessRef.current)) {
        setUncertain(true);
        setError('El contexto o los permisos cambiaron durante el guardado y no pudimos confirmarlo. Revisá el producto antes de intentar nuevamente.');
      }
    } finally {
      if (mounted.current && operation.current === id) {
        operation.current = null;
        setBusy(false);
      }
    }
  }

  function navigate(changes: Parameters<typeof updateCatalogHash>[1]) {
    if (!routeIsCurrent()) return;
    const next = updateCatalogHash(hash, changes);
    if (next !== hash) window.location.hash = next;
    else reloadList();
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (errors.length) return;
    const fields = new FormData(event.currentTarget);
    const classifications = classificationsFrom(fields);
    navigate({ q: String(fields.get('q') ?? ''), categoryId: classifications.categoryId ?? '',
      brandId: classifications.brandId ?? '', page: 1 });
  }

  function focusView() {
    const previousFocus = document.activeElement;
    requestAnimationFrame(() => {
      if (routeIsCurrent() && (document.activeElement === previousFocus || document.activeElement === document.body)) {
        (viewHeading.current ?? listToolbar.current)?.focus();
      }
    });
  }

  function backToList() {
    if (form && !allowDiscard()) return;
    const previousFocus = document.activeElement;
    if (detail) reloadList();
    setDetail(null);
    setForm(null);
    setEditedVariant(null);
    setPricedVariant('');
    setUncertain(false);
    setError('');
    setNotice('');
    requestAnimationFrame(() => {
      if (!routeIsCurrent() || (document.activeElement !== previousFocus && document.activeElement !== document.body)) return;
      if (listOrigin.current?.isConnected) listOrigin.current.focus();
      else (viewHeading.current ?? listToolbar.current)?.focus();
    });
  }

  function openProduct(id: string, variantId?: string) {
    if ((protectAttributes.current && !protectAttributes.current()) ||
        (protectIdentifiers.current && !protectIdentifiers.current())) return;
    void run(async (current) => {
      const result = await api.product(id);
      if (!current()) return;
      setUncertain(false);
      const variant = variantId ? result.variants.find(item => item.id === variantId) : undefined;
      setWorkspaceTab(variantId ? 'variantes' : 'general');
      setDetail(result);
      setEditedVariant(variant ?? null);
      setForm(variant ? 'variant' : null);
      focusView();
    });
  }

  function validateText(errors: Record<string, string>, name: string, value: string, limit: number, required = false) {
    if ((required && !value) || Array.from(value).length > limit || value.includes('\0')) {
      errors[name] = (fieldNames[name] ?? 'Motivo') + ': ' + (required ? 'ingresá un valor de hasta ' : 'usá hasta ') + limit + ' caracteres.';
    }
  }

  function showValidation(errors: Record<string, string>) {
    setFieldErrors(errors);
    const name = Object.keys(errors)[0];
    if (!name) return false;
    requestAnimationFrame(() => {
      const field = editorRef.current?.elements.namedItem(name);
      if (field instanceof HTMLElement) field.focus();
    });
    return true;
  }

  function readServerVersion() {
    if (!detail) return;
    void run(async (current) => {
      const result = await api.product(detail.product.id);
      if (!current()) return;
      const resource = form === 'variant' ? result.variants.find(({id}) => id === editedVariant?.id) : result.product;
      if (!resource) {
        setError('La variante ya no está disponible.');
        return;
      }
      setServerVersion(resource);
      setReviewedRevision(undefined);
    });
  }

  const canReviewSave = conflict || ((uncertain || reviewedRevision !== undefined) && (form === 'edit' || Boolean(editedVariant)));
  const reviewConflict = canReviewSave && <section aria-label="Revisión del guardado" className="catalog-conflict">
    <p role="status">Tus cambios siguen en el formulario. Consultá la versión actual antes de volver a guardar.</p>
    <button type="button" className="button-secondary" disabled={busy} onClick={readServerVersion}>Consultar versión actual</button>
    {serverVersion && <>
      <h3>Valores actuales del servidor</h3>
      <dl>{Object.entries(serverVersion).filter(([key]) => fieldNames[key]).map(([key, value]) =>
        <div key={key}><dt>{fieldNames[key]}</dt><dd>{Array.isArray(value) ? value.join(', ') : String(value ?? '—') || '—'}</dd></div>
      )}</dl>
    </>}
    {serverVersion && reviewedRevision !== serverVersion.revision && <button type="button" className="button-secondary"
      onClick={() => {
        setReviewedRevision(serverVersion.revision);
        if (uncertain) {
          setUncertain(false);
          setError('');
        }
      }}>Ya revisé la versión actual</button>}
  </section>;

  function saveProduct(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (accessRef.current !== 'manage' || uncertain || (conflict && !reviewedRevision)) return;
    const fields = new FormData(event.currentTarget);
    const data = {
      name: String(fields.get('name')).trim(),
      description: String(fields.get('description')).trim(),
      ...classificationsFrom(fields),
    };
    const errors: Record<string, string> = {};
    validateText(errors, 'name', data.name, 160, true);
    validateText(errors, 'description', data.description, 2000);
    if (showValidation(errors)) return;
    const status: Product['status'] = fields.get('status') === 'inactive' ? 'inactive' : 'active';
    const changes = editing ? changedFields({ ...data, status }, editing) : {};
    if (editing && !Object.keys(changes).length) {
      setNotice('No hay cambios para guardar.');
      return;
    }
    void run(async (current) => {
      const saved = form === 'edit' && detail
        ? await api.updateProduct(detail.product.id, { ...changes, expectedRevision: reviewedRevision ?? detail.product.revision })
        : await api.createProduct(data);
      if (!current()) {
        recordConfirmationInChangedContext();
        return;
      }
      setDetail((current) => current?.product.id === saved.id
        ? { ...current, product: saved }
        : { product: saved, variants: [], images: [] });
      setForm(null);
      setWorkspaceTab(form === 'edit' ? 'general' : 'variantes');
      setNotice(form === 'edit' ? 'Producto actualizado.' : 'Producto creado. Continuá con las variantes.');
      if (routeIsCurrent()) focusView();
      reloadList();
    }, true);
  }

  function recordConfirmationInChangedContext() {
    if (mounted.current && ['manage', 'read'].includes(accessRef.current)) {
      setUncertain(true);
      setError('El guardado se confirmó, pero el contexto o los permisos cambiaron. Volvé al listado para actualizar los datos.');
      reloadList();
    }
  }

  function saveVariant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || accessRef.current !== 'manage' || uncertain || (conflict && !reviewedRevision)) return;
    const fields = new FormData(event.currentTarget);
    const data = {
      sku: String(fields.get('sku')).trim(),
      size: '',
      color: '',
      barcodes: String(fields.get('barcodes')).split(/\r?\n/).map((line) => line.trim()).filter(Boolean),
    };
    const errors: Record<string, string> = {};
    const selections: { sizeId?: string | null; colorId?: string | null; size?: string; color?: string } = {};
    for (const [kind, key, label] of [['sizes', 'sizeId', 'size'], ['colors', 'colorId', 'color']] as const) {
      const selected = String(fields.get(key) ?? '');
      const original = editedVariant?.[key] ?? (editedVariant?.[label] ? 'legacy' : '');
      if (selected === original || selected === 'legacy') continue;
      const id = selected.replace(/^refresh-/, '');
      const master = attributes.data?.[kind].find(item => item.id === id && item.status === 'active');
      if (id && !master) {
        errors[key] = 'Actualizá las opciones y elegí un valor activo.';
        continue;
      }
      selections[key] = id || null;
      selections[label] = master?.name ?? '';
    }
    validateText(errors, 'sku', data.sku, 80, true);
    if (data.barcodes.length > 20 || new Set(data.barcodes).size !== data.barcodes.length ||
        data.barcodes.some((code) => !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$/.test(code))) {
      errors.barcodes = 'Ingresá hasta 20 códigos válidos y sin duplicados.';
    }
    const reason = String(fields.get('reason') ?? '').trim();
    const confirmSensitive = fields.has('confirmSensitive');
    const generateInternal = fields.has('generateInternal');
    if (generateInternal && data.barcodes.length >= 20) {
      errors.barcodes = 'Dejá un lugar para el código interno dentro del máximo de 20.';
    }
    const baseline = reviewedRevision && serverVersion && 'sku' in serverVersion ? serverVersion : editedVariant;
    const codes = data.barcodes.length ? [data.barcodes[0], ...data.barcodes.slice(1).sort()] : [];
    const status: Variant['status'] = fields.get('status') === 'inactive' ? 'inactive' : 'active';
    const changes = editedVariant ? {
      ...changedFields({ sku: data.sku, barcodes: codes, status }, editedVariant), ...selections,
    } : {};
    const correction = baseline && (generateInternal || ['sku', 'size', 'color', 'barcodes'].some((field) => {
      const key = field as 'sku' | 'size' | 'color' | 'barcodes';
      return changes[key] !== undefined && JSON.stringify(changes[key]) !== JSON.stringify(typeof baseline[key] === 'string' ? baseline[key].trim() : baseline[key]);
    }));
    if (correction || reuseRetired) {
      if (!reason || Array.from(reason).length > 500) errors.reason = 'Ingresá el motivo (hasta 500 caracteres).';
      if (!confirmSensitive) errors.confirmSensitive = 'Marcá la casilla para confirmar la corrección.';
    }
    if (showValidation(errors) || operation.current) return;
    if (editedVariant && !Object.keys(changes).length && !generateInternal) {
      setNotice('No hay cambios para guardar.');
      return;
    }
    const productId = detail.product.id;
    if (!editedVariant) {
      const intent = { productId, key: crypto.randomUUID(), data: {
        ...data, ...selections, generateInternal, reuseRetired, reason, confirmSensitive,
      } };
      pendingVariantCreate.current = intent;
      sendVariantCreation(intent);
      return;
    }
    void run(async (current) => {
      const saved = await api.updateVariant(productId, editedVariant.id, {
        ...changes, expectedRevision: reviewedRevision ?? editedVariant.revision, reason, confirmSensitive, generateInternal, reuseRetired,
      });
      if (!current()) {
        recordConfirmationInChangedContext();
        return;
      }
      commitVariant(productId, saved, 'Variante actualizada.');
    }, true);
  }

  function sendVariantCreation(intent: NonNullable<typeof pendingVariantCreate.current>) {
    void run(async (current) => {
      const saved = await api.createVariant(intent.productId, intent.data, intent.key);
      if (!current()) {
        recordConfirmationInChangedContext();
        return;
      }
      pendingVariantCreate.current = null;
      commitVariant(intent.productId, saved, 'Variante creada.');
    }, true);
  }

  function commitVariant(productId: string, saved: Variant, text: string) {
    setDetail((current) => current?.product.id === productId ? {
      ...current,
      variants: [...current.variants.filter((variant) => variant.id !== saved.id), saved]
        .sort((a, b) => a.sku.localeCompare(b.sku)),
    } : current);
    setUncertain(false);
    setForm(null);
    setEditedVariant(null);
    setNotice(text);
    reloadList();
  }

  function updateClassification(kind: ClassificationKind, term: Term) {
    (kind === 'categories' ? categories : brands).update(term);
  }

  function leaveEditor(next: Form | null) {
    setUncertain(false);
    setError('');
    setNotice('');
    setForm(next);
    if (next !== 'variant') setEditedVariant(null);
    focusView();
  }

  function openVariant(variant: Variant | null) {
    setWorkspaceTab('variantes');
    if (form === 'variant' && (variant?.id === editedVariant?.id || !allowDiscard())) return;
    setEditedVariant(variant);
    setUncertain(false);
    setForm('variant');
    setError('');
    setNotice('');
  }

  useEffect(() => {
    if (!active || access !== 'manage' || !ready || createRequest <= consumedCreate.current) return;
    consumedCreate.current = createRequest;
    if (form && !allowDiscard()) return;
    setDetail(null); setEditedVariant(null); setWorkspaceTab('general'); setForm('quick'); setError(''); setNotice('');
    // This effect consumes an explicit creation intent once after access is established.
  }, [createRequest, active, access, ready]);

  if (access === 'session') return <p role="alert">La sesión expiró. Volvé a iniciar sesión para consultar Productos.</p>;
  if (access === 'denied') return <div role="alert">
    <p>No tenés permiso para consultar Productos.</p>
    <button type="button" onClick={reloadList}>Reintentar</button>
  </div>;
  const canManage = access === 'manage' && ready;
  const currentList = list?.key === listKey && !errors.length ? list : undefined;
  const page = currentList?.page;
  const authorizedStores = stores ?? [];
  const authorizedBranch = authorizedStores.find((store) => store.id === branchId);
  const catalogLookupKey = JSON.stringify([scope, access]);
  const previews = new Map((page?.previews ?? []).map((item) => [item.productId, item]));
  const editing = form === 'edit' ? detail?.product : undefined;
  const recordReadable = access !== 'read' || (detail?.product.status !== 'inactive' && (form !== 'variant' || editedVariant?.status !== 'inactive'));
  const listVisible = (!detail && !form) || !recordReadable;
  const hasFilters = Boolean(query || categoryId || brandId);
  const classificationName = (terms: Term[], id: string | null) => terms.find((term) => term.id === id)?.name;


  return (
    <section aria-label="Catálogo" aria-busy={busy} className="products-view">
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {errors.length > 0 && <div role="alert">
        <p>Corregí los filtros de la URL para consultar el catálogo.</p>
        <ul>{errors.map(({ field, label, message }) => <li key={field}>
          {label}: {message}{' '}
          <button type="button" onClick={() => navigate(field === 'page' ? { page: 1 } : { [field]: '' })}>
            {field === 'page' ? 'Volver a la página 1' : 'Quitar parámetro de ' + label}
          </button>
        </li>)}</ul>
      </div>}
      {currentList?.error && <div role="alert">
        <p>No pudimos cargar el listado. {currentList.error}</p>
        <button type="button" onClick={reloadList}>Reintentar listado</button>
      </div>}
      {!currentList && !errors.length && <p role="status">Cargando productos…</p>}
      {access === 'verify' && <p role="status">Estamos verificando qué acceso tenés al catálogo.</p>}
      {form && access === 'read' && <p role="status">Ya no tenés permiso para guardar. Los cambios locales todavía no se guardaron.</p>}
      <div hidden={access === 'verify'}>
      <div hidden={!listVisible}>
        <div className="products-toolbar" ref={listToolbar} tabIndex={-1} role="group" aria-label="Acciones del catálogo">
          {canManage && <div className="catalog-toolbar-actions">
            <button ref={toolsButton} type="button" className="button-secondary" aria-expanded={toolsOpen}
              aria-controls={toolsId} onClick={() => setToolsOpen(!toolsOpen)}>Herramientas</button>
            <button type="button" disabled={busy} onClick={(event) => {
              listOrigin.current = event.currentTarget;
              setWorkspaceTab('general');
              setForm('quick');
              setNotice('');
              setError('');
            }}>＋ Crear producto</button>
          </div>}
        </div>
        {canManage && <section id={toolsId} className="catalog-tools-panel" hidden={!toolsOpen} aria-label="Herramientas del catálogo"
          onKeyDown={(event) => {
            if (event.key !== 'Escape' || event.defaultPrevented) return;
            event.preventDefault();
            setToolsOpen(false);
            toolsButton.current?.focus();
          }}>
          <div className="catalog-tool-sections" role="group" aria-label="Secciones de Herramientas">
            <button type="button" className="button-secondary" aria-pressed={tool === 'csv'} onClick={() => setTool('csv')}>CSV</button>
            <button type="button" className="button-secondary" disabled={!categories.items || Boolean(categories.error)} aria-pressed={tool === 'categories'} onClick={() => setTool('categories')}>Categorías</button>
            <button type="button" className="button-secondary" disabled={!brands.items || Boolean(brands.error)} aria-pressed={tool === 'brands'} onClick={() => setTool('brands')}>Marcas</button>
            <button type="button" className="button-secondary" aria-pressed={tool === 'attributes'} onClick={() => setTool('attributes')}>Talles y colores</button>
            <button type="button" className="button-secondary" aria-pressed={tool === 'identifiers'} onClick={() => setTool('identifiers')}>Numeración</button>
          </div>
          <div hidden={tool !== 'csv'}>
          <CatalogCSV onImported={async () => {
            if (!mounted.current || accessRef.current !== 'manage') return;
            if (scopeIsCurrent() && routeIsCurrent() && !errors.length && pageNumber !== 1) {
              navigate({ page: 1 });
            } else {
              reloadList();
            }
          }} />
          </div>
          {categories.items && <div hidden={tool !== 'categories' || Boolean(categories.error)}>
            <ClassificationEditor kind="categories" values={classifications.categories} onSaved={updateClassification} />
          </div>}
          {brands.items && <div hidden={tool !== 'brands' || Boolean(brands.error)}>
            <ClassificationEditor kind="brands" values={classifications.brands} onSaved={updateClassification} />
          </div>}
          <div hidden={tool !== 'attributes'}>
            <CatalogAttributeTools masters={attributes.data} loadingError={attributes.error} reload={attributes.retry}
              active={active && toolsOpen && tool === 'attributes' && listVisible} scope={scope} expire={expireCatalog}
              onResolve={item => openProduct(item.productId, item.id)} protect={protectAttributes} />
          </div>
          <div hidden={tool !== 'identifiers'}>
            <CatalogIdentifierSettings active={active && toolsOpen && tool === 'identifiers' && listVisible}
              scope={scope} protect={protectIdentifiers} />
          </div>
        </section>}
        {active && listVisible && (access === 'manage' || access === 'read') &&
          <BarcodeScanner key={catalogLookupKey} access={priceAccess}>{(match) => <>
            <div className="catalog-scan-heading">
              <h3>{match.product.name}</h3>
              <span className="products-status active">Producto y variante activos</span>
            </div>
            <ClassificationSummary values={classifications} product={match.product} />
            <VariantInfo variant={match.variant} />
            <div className="actions">
              <button type="button" className="button-secondary" disabled={busy} onClick={(event) => {
                listOrigin.current = event.currentTarget;
                openProduct(match.product.id);
              }}>Abrir producto</button>
              <button type="button" className="button-secondary" aria-haspopup="dialog" onClick={(event) => {
                labelOrigin.current = event.currentTarget;
                setBatchLabels(false); setLabel(match);
              }}>Etiqueta de {match.variant.sku}</button>
            </div>
            <VariantPrice variantId={match.variant.id} store={branch} stores={authorizedStores} revision={priceRevision} access={priceAccess} />
            {authorizedBranch && <ScannedStock store={authorizedBranch} variantId={match.variant.id} revision={stockRevision} access={priceAccess} />}
          </>}</BarcodeScanner>}
        <form key={hash} ref={attachFilters} onSubmit={search} className="catalog-search" aria-label="Buscar productos">
          <label>Nombre o SKU<input name="q" type="search" defaultValue={query} /></label>
          <ClassificationFields values={filterOptions(classifications, categoryId, brandId)} current={{ categoryId, brandId }} filtering states={classificationStates} onRetry={retryClassification} />
          <button name="catalogSubmit" disabled={busy || errors.length > 0}>Buscar productos</button>
        </form>
        {page && (page.items.length === 0 ? <p className="products-empty" role="status">
          {pageNumber > 1 ? 'No hay resultados en esta página.'
            : hasFilters ? 'No hay productos que coincidan con la búsqueda.' : 'Todavía no hay productos en el catálogo.'}
          {pageNumber > 1 && <button type="button" onClick={() => navigate({ page: 1 })}>Volver a la página 1</button>}
        </p> : (
          <div className="products-table">
            <table role="table" aria-label="Productos del catálogo">
              <thead role="rowgroup"><tr role="row"><th role="columnheader" scope="col"><div className="catalog-product-name">
                <label className="catalog-select" title="Seleccionar los productos de esta página"><input type="checkbox"
                  aria-label="Seleccionar los productos de esta página"
                  checked={page.items.length > 0 && page.items.every((product) => selection.has(product.id))}
                  onChange={(event) => { for (const product of page.items) toggleSelected(product.id, product.name, event.target.checked); }} /></label>
                Producto</div></th><th role="columnheader" scope="col">Categoría y marca</th><th role="columnheader" scope="col">{branch ? 'Precio · ' + branch.name : 'Precio'}</th><th role="columnheader" scope="col">Stock vendible</th><th role="columnheader" scope="col">Estado</th><th role="columnheader" scope="col">Consulta</th></tr></thead>
              <tbody role="rowgroup">{page.items.map((product) => {
                const category = classificationName(classifications.categories, product.categoryId);
                const brand = classificationName(classifications.brands, product.brandId);
                const preview = previews.get(product.id);
                return <Fragment key={product.id}><tr role="row" className="catalog-product-row">
                  <th scope="row" role="rowheader"><div className="catalog-product-name">
                    <label className="catalog-select"><input type="checkbox" aria-label={'Seleccionar ' + product.name} checked={selection.has(product.id)}
                      onChange={(event) => toggleSelected(product.id, product.name, event.target.checked)} /></label>
                    {preview?.photoId && <img src={api.productPhotoURL(product.id, preview.photoId)} loading="lazy" alt="" />}
                    <button type="button" className="catalog-link" title={product.name} disabled={busy} onClick={(event) => {
                    listOrigin.current = event.currentTarget;
                    openProduct(product.id);
                  }}><span>{product.name}</span></button></div></th>
                  <td role="cell" data-label="Clasificación">{[category, brand].filter(Boolean).join(' · ') || (terms ? 'Sin clasificación' : 'Clasificación no disponible')}</td>
                  <td role="cell" data-label="Precio" data-store={branch?.name}><PreviewPrice preview={preview} /></td>
                  <td role="cell" data-label="Stock vendible">{preview?.stockReadable ? preview.available ?? 'Sin saldo' : '—'}</td>
                  <td role="cell" data-label="Estado del producto"><span className={'products-status ' + product.status}>{product.status === 'active' ? 'Producto activo' : 'Producto inactivo'}</span></td>
                  <td role="cell" className="product-quick-action"><button type="button" className="button-secondary"
                    aria-expanded={quickProduct?.id === product.id && quickProduct.open}
                    aria-controls={quickProduct?.id === product.id ? 'catalog-variants-' + product.id : undefined}
                    onClick={() => setQuickProduct((current) => ({ id: product.id, open: current?.id !== product.id || !current.open }))}>
                    {quickProduct?.id === product.id && quickProduct.open ? 'Contraer variantes' : 'Ver variantes'}<span className="pos-sr-only"> de {product.name}</span>
                  </button></td>
                </tr>{quickProduct?.id === product.id && <tr id={'catalog-variants-' + product.id} hidden={!quickProduct.open} className="product-quick-row" role="row">
                  <td colSpan={6} role="cell">{active && <ProductQuickView key={product.id + ':' + catalogLookupKey + ':' + stockRevision} productId={product.id}
                    store={authorizedBranch} stores={authorizedStores} revision={priceRevision} access={priceAccess} />}</td>
                </tr>}</Fragment>;
              })}</tbody>
            </table>
          </div>
        ))}
        {listVisible && <BulkBar selected={selection} onClear={() => setSelection(new Map())}
          onLabels={(origin) => setBulk({ kind: 'labels', origin })} onPrices={(origin) => setBulk({ kind: 'prices', origin })} />}
        {active && bulk?.kind === 'labels' && <BulkLabels ids={[...selection.keys()]} storeId={authorizedBranch?.id} returnTo={bulk.origin} onClose={() => setBulk(null)} />}
        {active && bulk?.kind === 'prices' && <BulkPrices ids={[...selection.keys()]} branch={authorizedBranch} returnTo={bulk.origin}
          onClose={() => setBulk(null)} onApplied={() => reloadList()} />}
        {page && (pageNumber > 1 || page.hasMore) && <nav className="catalog-pagination" aria-label="Páginas del catálogo">
          <button type="button" className="button-secondary" disabled={busy || pageNumber === 1} onClick={() => navigate({ page: pageNumber - 1 })}>Anterior</button>
          <span aria-current="page">Página {pageNumber}</span>
          <button type="button" className="button-secondary" disabled={busy || !page.hasMore || pageNumber === catalogLastPage}
            onClick={() => navigate({ page: pageNumber + 1 })}>Siguiente</button>
        </nav>}
      </div>
      {form === 'quick' && canManage && <QuickProductCreate branch={authorizedBranch} stores={authorizedStores}
        classifications={filterOptions(classifications, '', '')} classificationStates={classificationStates} onRetryClassification={retryClassification}
        masters={attributes.data} protect={protectMatrix} onCancel={backToList}
        onOpenProduct={id => { listOrigin.current = null; openProduct(id); }}
        onCreated={() => { reloadList(); onInventoryChanged(); }} />}
      {(detail || (form && form !== 'quick')) && <button type="button" className="products-back" disabled={busy} onClick={backToList}>← Volver a productos</button>}
      {recordReadable && detail && <div className="commercial-product-heading" key={detail.product.id}>
        <div><small>Ficha comercial · {branch?.name ?? 'Sin sucursal'}</small><h2>{detail.product.name}</h2>
          {Array.from(detail.product.name).length > 45 && <details className="commercial-full-name">
            <summary>Ver nombre completo</summary><p>{detail.product.name}</p>
          </details>}</div>
        <span className={'products-status ' + detail.product.status}>{detail.product.status === 'active' ? 'Activo' : 'Inactivo'}</span>
      </div>}
      {recordReadable && detail && <div className="commercial-tabs-shell">
        {(tabEdges.left || tabEdges.right) && <button type="button" className="button-secondary commercial-tabs-scroll" aria-label="Ver secciones anteriores" disabled={!tabEdges.left}
          onClick={() => workspaceTabs.current?.scrollBy({ left: -220, behavior: 'smooth' })}>‹</button>}
        <nav ref={workspaceTabs} onScroll={measureTabs} className="commercial-product-tabs" aria-label="Secciones del producto">
        {[['general', 'Datos generales'], ['variantes', 'Variantes'], ['precios', 'Precios'], ['mercaderia', 'Mercadería'], ['etiquetas', 'Etiquetas'], ['resumen', 'Resumen']].map(([id,label]) =>
          <button type="button" key={id} disabled={busy} aria-current={workspaceTab === id ? 'page' : undefined}
            onClick={event => { setWorkspaceTab(id); event.currentTarget.scrollIntoView({ block: 'nearest', inline: 'nearest' }); }}>{label}</button>)}
        </nav>
        {(tabEdges.left || tabEdges.right) && <button type="button" className="button-secondary commercial-tabs-scroll" aria-label="Ver más secciones" disabled={!tabEdges.right}
          onClick={() => workspaceTabs.current?.scrollBy({ left: 220, behavior: 'smooth' })}>›</button>}
      </div>}
      <div className={detail ? 'commercial-workspace-layout' : ''}>
      <div className="commercial-workspace-main">
      <div hidden={workspaceTab !== 'general'}>
      {recordReadable && (form === 'product' || form === 'edit') && (
        <form key={editing?.id ?? 'new-product'} aria-label={editing ? 'Editar producto' : 'Nuevo producto'} ref={editorRef} onChange={trackChanges} onSubmit={saveProduct}>
          <h2 tabIndex={-1} ref={viewHeading}>{editing ? 'Editar producto' : 'Nuevo producto'}</h2>
          <fieldset className="catalog-editor-fields"  disabled={!canManage || busy || uncertain}>
          <label>Nombre<input name="name" required {...fieldAttributes("name")} defaultValue={editing?.name} />{fieldError("name")}</label>
          <label>Descripción<textarea name="description" {...fieldAttributes("description")} defaultValue={editing?.description} />{fieldError("description")}</label>
          <ClassificationFields values={filterOptions(classifications, editing?.categoryId ?? '', editing?.brandId ?? '')} current={editing} states={classificationStates} onRetry={retryClassification} />
          {editing && <label>Estado<select name="status" defaultValue={editing.status}>
            <option value="active">Activo</option>
            <option value="inactive">Inactivo</option>
          </select></label>}
          </fieldset>
          {reviewConflict}
          <div className="actions">
            <button disabled={busy || !canManage || uncertain || (conflict && !reviewedRevision)}>{busy ? 'Guardando…' : 'Guardar producto'}</button>
            <button type="button" disabled={busy} onClick={() => {
              if (editing) { if (allowDiscard()) leaveEditor(null); }
              else backToList();
            }}>Cancelar</button>
          </div>
        </form>
      )}
      </div>
      <div hidden={workspaceTab !== 'variantes'}>
      {recordReadable && detail && form === 'matrix' && <VariantMatrix key={detail.product.id} productId={detail.product.id}
        masters={attributes.data} masterError={attributes.error} reloadMasters={attributes.retry} manage={canManage}
        protect={protectMatrix} expire={expireCatalog}
        onClose={() => {
          setForm(null);
          requestAnimationFrame(() => matrixOrigin.current?.focus());
        }}
        onCreated={result => {
          const productId = detail.product.id;
          setDetail(current => current?.product.id === productId ? {
            ...current,
            variants: [...current.variants.filter(item => !result.variants.some(saved => saved.id === item.id)), ...result.variants]
              .sort((a,b) => a.sku.localeCompare(b.sku)),
          } : current);
          reloadList();
        }} />}
      </div>
      {recordReadable && detail && form !== 'product' && form !== 'edit' && form !== 'matrix' && (
        <section aria-label="Detalle del producto">
          <div hidden={workspaceTab !== 'general' && workspaceTab !== 'variantes'}>
          <h3 tabIndex={-1} ref={viewHeading}>Información del producto</h3>
          {detail.product.description && <p>{detail.product.description}</p>}
          <ClassificationSummary values={classifications} product={detail.product} />
          {canManage && <div className="actions">
            <button type="button" disabled={busy} onClick={() => { if (!form || allowDiscard()) { setWorkspaceTab('general'); leaveEditor('edit'); } }}>Editar producto</button>
            {detail.product.status === 'active' && form !== 'variant' && (
              <>
                <button type="button" disabled={busy} onClick={() => openVariant(null)}>Agregar variante</button>
                <button ref={matrixOrigin} type="button" className="button-secondary" disabled={busy}
                  onClick={() => { setWorkspaceTab('variantes'); leaveEditor('matrix'); }}>Generar variantes</button>
              </>
            )}
          </div>}
          </div>
          <div hidden={workspaceTab !== 'variantes'}>
          {form === 'variant' && (
            <form className="catalog-variant-editor" key={editedVariant?.id ?? 'new-variant'} aria-label={editedVariant ? 'Editar variante' : 'Nueva variante'} ref={editorRef}
              onChange={event => {
                trackChanges();
                const control = event.target;
                if (control instanceof HTMLInputElement || control instanceof HTMLTextAreaElement || control instanceof HTMLSelectElement) {
                  clearFieldError(control.name);
                }
              }} onSubmit={saveVariant}>
              <fieldset className="catalog-editor-fields"  disabled={!canManage || busy || uncertain}>
              <VariantSKU initial={editedVariant?.sku} error={fieldErrors.sku} scope={scope}
                enabled={active && canManage} onApply={() => { setDirty(true); clearFieldError('sku'); }} />
              <div className="catalog-variant-attributes">
              <AttributePicker label="Talle" kind="sizes" id={editedVariant?.sizeId} text={editedVariant?.size}
                masters={attributes.data} error={attributes.error} invalid={Boolean(fieldErrors.sizeId)} describedBy={fieldAttributes('sizeId')['aria-describedby']} />
              {fieldError('sizeId')}{fieldError('size')}
              <AttributePicker label="Color" kind="colors" id={editedVariant?.colorId} text={editedVariant?.color}
                masters={attributes.data} error={attributes.error} invalid={Boolean(fieldErrors.colorId)} describedBy={fieldAttributes('colorId')['aria-describedby']} />
              {fieldError('colorId')}{fieldError('color')}
              </div>
              {attributes.error && <p role="alert">{attributes.error} <button type="button" onClick={attributes.retry}>Reintentar talles y colores</button></p>}
              <label>Códigos de barras, principal en la primera línea
                <textarea rows={2} name="barcodes" {...fieldAttributes("barcodes")} defaultValue={editedVariant?.barcodes.join('\n')} />{fieldError("barcodes")}
              </label>
              {editedVariant && <label>Estado
                <select name="status" defaultValue={editedVariant.status}>
                  <option value="active">Activa</option>
                  <option value="inactive">Inactiva</option>
                </select>
              </label>}
              {editedVariant?.internalBarcode ? <small>Interno: <strong>{editedVariant.internalBarcode}</strong>
                {editedVariant.barcodes[0] === editedVariant.internalBarcode ? ' · Principal' : ' · Secundario'}</small> :
                <label className="catalog-id-check"><input type="checkbox" name="generateInternal" /> Generar código interno al guardar</label>}
              <IdentifierHistory scope={scope} />
              <label className="catalog-id-check"><input type="checkbox" name="reuseRetired" checked={reuseRetired}
                onChange={event => setReuseRetired(event.target.checked)} /> Solicitar reutilización de un identificador retirado</label>
              {reuseRetired && <small>Un código de barras retirado solo puede volver a su variante original.</small>}
              {(editedVariant || reuseRetired) && <>
                <label>Motivo de la corrección
                  <input name="reason" {...fieldAttributes("reason")} />{fieldError("reason")}
                </label>
                <label className="catalog-id-check"><input type="checkbox" name="confirmSensitive" {...fieldAttributes("confirmSensitive")} />
                  {reuseRetired ? 'Confirmo que solicito la reutilización' : 'Confirmo que es la misma variante'}</label>
                {fieldError("confirmSensitive")}
              </>}
              </fieldset>
              {reviewConflict}
              <div className="actions">
                {uncertain && !editedVariant && pendingVariantCreate.current && <button type="button" className="button-secondary"
                  disabled={busy || !canManage} onClick={() => {
                    if (pendingVariantCreate.current) sendVariantCreation(pendingVariantCreate.current);
                  }}>Reintentar la misma alta</button>}
                <button disabled={busy || !canManage || uncertain || (conflict && !reviewedRevision)}>{busy ? 'Guardando…' : 'Guardar variante'}</button>
                <button type="button" disabled={busy} onClick={() => { if (allowDiscard()) leaveEditor(null); }}>Cancelar</button>
              </div>
            </form>
          )}
          </div>
          <div hidden={workspaceTab !== 'general'}>
          <ProductPhotos key={detail.product.id} product={detail.product} images={detail.images} canManage={canManage} active={active}
            onChanged={(images) => setDetail((current) =>
              current?.product.id === detail.product.id ? { ...current, images } : current)} />
          </div>
          <div hidden={workspaceTab !== 'variantes' && workspaceTab !== 'etiquetas'}>
          <h3>{workspaceTab === 'etiquetas' ? 'Etiquetas de las variantes' : 'Variantes'}</h3>
          {workspaceTab === 'etiquetas' && detail.product.status === 'active' && detail.variants.some(item => item.status === 'active' && item.barcodes.length) &&
            <button type="button" onClick={event => {
              const first = detail.variants.find(item => item.status === 'active' && item.barcodes.length)!;
              labelOrigin.current = event.currentTarget; setBatchLabels(true); setLabel({ product: detail.product, variant: first });
            }}>Imprimir todas las etiquetas</button>}
          {detail.variants.length === 0 ? <p>Este producto todavía no tiene variantes.</p> : (
            <ul className="catalog-items">{detail.variants.filter((variant) => access !== 'read' || variant.status === 'active').map((variant) => (
              <li key={variant.id}>
                <VariantInfo variant={variant} />
                {variant.status === 'inactive' && <p>Variante inactiva</p>}
                {canManage && (
                  <button type="button" className="catalog-link" disabled={busy} onClick={() => openVariant(variant)}>
                    Editar variante {variant.sku}
                  </button>
                )}
                {detail.product.status === 'active' && variant.status === 'active' && (
                  <>
                    <button type="button" className="catalog-link" aria-expanded={pricedVariant === variant.id}
                      onClick={() => setPricedVariant((current) => current === variant.id ? '' : variant.id)}>
                      Precio de {variant.sku}
                    </button>
                    {active && pricedVariant === variant.id && <VariantPrice key={catalogLookupKey} variantId={variant.id} store={branch}
                      stores={authorizedStores} revision={priceRevision} access={priceAccess} />}
                    {variant.barcodes.length > 0 ? (
                      <button type="button" className="catalog-link" aria-haspopup="dialog" onClick={(event) => {
                        labelOrigin.current = event.currentTarget;
                        setBatchLabels(false); setLabel({ product: detail.product, variant });
                      }}>Imprimir etiqueta de {variant.sku}</button>
                    ) : <p>Etiqueta no disponible: esta variante no tiene código de barras.</p>}
                  </>
                )}
                {(detail.product.status !== 'active' || variant.status !== 'active') &&
                  <p>Etiqueta no disponible para productos o variantes inactivos.</p>}
              </li>
            ))}</ul>
          )}
          </div>
        </section>
      )}
      {recordReadable && detail && authorizedBranch && <CommercialProductPanels key={detail.product.id}
        detail={detail} branch={authorizedBranch} active={active && access !== 'verify'} tab={workspaceTab} scope={scope}
        context={context} revision={priceRevision + stockRevision + commercialRevision} access={priceAccess}
        recoveryScope={recoveryScope} onBusy={onInventoryBusy} onChanged={() => {
          setCommercialRevision(value => value + 1); reloadList(); onInventoryChanged();
        }} />}
      </div>
      </div>
      {active && label && (access === 'manage' || (access === 'read' && label.product.status === 'active' && label.variant.status === 'active')) && <LabelDialog key={label.variant.id + ':' + batchLabels + ':' + (authorizedBranch?.id ?? '')} data={label} storeId={authorizedBranch?.id}
        additional={batchLabels && detail ? detail.variants.filter(item => item.status === 'active' && item.id !== label.variant.id).map(variant => ({ product: detail.product, variant })) : undefined}
        returnTo={labelOrigin.current} onClose={() => setLabel(null)} />}
      </div>
    </section>
  );
}
