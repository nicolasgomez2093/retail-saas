import { primaryLabel, variantLabel } from '../catalog/quickProduct';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { commercialStatus } from '../catalog/commercialStatus';
import { posPrice } from '../pos/priceDisplay';
import { ApiError, api, type CatalogPreview, type ResolvedPrice, type ExistenceDetail, type Product, type ProductPhoto, type Term, type Variant } from '../../shared/api/client';

const barcodePattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$/;

export type FavoriteProduct = Pick<Product, 'id' | 'name'>;

export type Selected = { id: string; sku: string; product: string; size?: string; color?: string; photoURL?: string };

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No pudimos buscar la variante.';
}

export function InventoryVariantPicker({ disabled, onSelect, forSale = false, favorites, onToggleFavorite, storeId, checkoutOpen = false }: {
  disabled: boolean; onSelect: (variant: Selected) => void | Promise<void>; forSale?: boolean;
  storeId?: string; checkoutOpen?: boolean; favorites?: FavoriteProduct[]; onToggleFavorite?: (product: FavoriteProduct) => void;
}) {
  const unifiedInput = useRef<HTMLInputElement>(null);
  const [knownVariants, setKnownVariants] = useState<Record<string, Variant[]>>({});
  const [scanMode, setScanMode] = useState(false);
  const detailOpener = useRef<HTMLButtonElement>(null);
  const detailHeading = useRef<HTMLElement>(null);
  const initialRequest = useRef(true);
  const [previews, setPreviews] = useState<CatalogPreview[]>([]);
  const [starting, setStarting] = useState(false);
  const [products, setProducts] = useState<Product[]>([]);
  const [selection, setSelection] = useState<{ product: string; variants: Variant[]; id: string; productStatus: Product['status']; photo?: ProductPhoto } | null>(null);
  const [variantPage, setVariantPage] = useState(0);
  const [quoteRetry, setQuoteRetry] = useState(0);
  const [variantRead, setVariantRead] = useState<{ key: string; rows: Record<string, {
    price?: ResolvedPrice; stock?: ExistenceDetail; error?: string;
  }> }>();
  const visibleVariants = selection?.variants.slice(variantPage * 8, variantPage * 8 + 8) ?? [];
  const quoteKey = JSON.stringify([selection?.id, storeId, variantPage, quoteRetry]);
  useEffect(() => {
    if (!forSale || !selection || !storeId) return;
    let current = true;
    void Promise.all(visibleVariants.map(async variant => {
      try {
        const [price, stock] = await Promise.all([api.resolvePrice(storeId, variant.id), api.existence(storeId, variant.id)]);
        return [variant.id, { price, stock }] as const;
      } catch (cause) {
        return [variant.id, { error: errorMessage(cause) }] as const;
      }
    })).then(rows => { if (current) setVariantRead({ key: quoteKey, rows: Object.fromEntries(rows) }); });
    return () => { current = false; };
    // Selection, store, page and retry together identify this read intention.
  }, [forSale, quoteKey]);
  const [listing, setListing] = useState<{ query: string; offset: number; categoryId: string | null; hasMore: boolean }>();
  const [categories, setCategories] = useState<Term[]>([]);
  const [categoryError, setCategoryError] = useState(false);
  const [categoryAttempt, setCategoryAttempt] = useState(0);
  useEffect(() => {
    if (!forSale) return;
    let active = true;
    void api.classifications().then((data) => {
      if (active) { setCategories(data.categories); setCategoryError(false); }
    }).catch(() => {
      if (active) setCategoryError(true);
    });
    return () => { active = false; };
  }, [forSale, categoryAttempt]);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState('');
  const [error, setError] = useState('');
  const unavailable = busy || disabled;

  function restoreFocus(control: HTMLElement | null) {
    requestAnimationFrame(() => {
      if (control?.getClientRects().length && (document.activeElement === document.body || document.activeElement === control)) {
        control.focus({ preventScroll: true });
      }
    });
  }

  function closeProduct() {
    setSelection(null);
    const opener = detailOpener.current;
    requestAnimationFrame(() => {
      if (opener?.getClientRects().length) opener.focus();
    });
  }

  useEffect(() => {
    if (!forSale || !storeId) return;
    const wide = window.matchMedia('(min-width: 721px)');
    let active = true;
    let requested = false;
    function loadInitial() {
      if (!wide.matches || requested || !initialRequest.current) return;
      requested = true;
      setStarting(true);
      void api.products('', 0, undefined, storeId).then((page) => {
        if (!active || !initialRequest.current) return;
        setProducts(page.items);
        setPreviews(page.previews ?? []);
        setListing({ query: '', offset: 0, categoryId: null, hasMore: page.hasMore });
        if (!page.items.length) setFeedback('No se encontraron productos.');
      }).catch((cause: unknown) => {
        if (active && initialRequest.current) setError(errorMessage(cause));
      }).finally(() => { if (active) setStarting(false); });
    }
    loadInitial();
    wide.addEventListener('change', loadInitial);
    return () => { active = false; wide.removeEventListener('change', loadInitial); };
  }, [forSale, storeId]);

  useEffect(() => {
    if (selection && forSale) detailHeading.current?.focus();
  }, [selection, forSale]);

  useEffect(() => {
    if (checkoutOpen && forSale && window.matchMedia('(min-width: 721px)').matches) setSelection(null);
  }, [checkoutOpen, forSale]);

  async function lookup(operation: () => Promise<void>) {
    if (unavailable) return;
    setBusy(true);
    setError('');
    setFeedback('');
    try {
      await operation();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function listProducts(query: string, offset: number, categoryId: string | null, tryBarcode = false) {
    const result = await api.products(query, offset, categoryId ? { categoryId, brandId: null } : undefined, forSale ? storeId : undefined);
    if (tryBarcode && !result.items.length && barcodePattern.test(query)) {
      try {
        await selectBarcode(query);
        return;
      } catch (cause) {
        if (!(cause instanceof ApiError) || cause.status !== 404) throw cause;
      }
    }
    setPreviews(result.previews ?? []);
    setProducts(result.items);
    setListing({ query, offset, categoryId, hasMore: result.hasMore });
    setSelection(null);
    if (!result.items.length) setFeedback('No se encontraron productos.');
    else if (!forSale && result.hasMore) setFeedback('Acotá la búsqueda para ver los demás resultados.');
  }

  function loadProducts(query: string, offset = 0, categoryId: string | null = null, tryBarcode = false) {
    if (unavailable) return;
    initialRequest.current = false;
    setStarting(false);
    void lookup(() => listProducts(query, offset, categoryId, tryBarcode));
  }

  function searchUnified(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const input = unifiedInput.current;
    if (!input) return;
    const query = input.value.trim();
    if (scanMode) {
      scanCode(input);
    } else {
      loadProducts(query, 0, listing?.categoryId, true);
    }
  }

  function filterCategory(id: string | null) {
    loadProducts(scanMode ? listing?.query ?? '' : unifiedInput.current?.value.trim() ?? '', 0, id);
  }

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const query = String(new FormData(event.currentTarget).get('variantQuery')).trim();
    const categoryId = String(new FormData(event.currentTarget).get('posCategory') ?? '') || null;
    if (query || forSale) loadProducts(query, 0, categoryId);
  }

  function openProduct(id: string, allowDirectAdd = false, opener?: HTMLButtonElement) {
    if (unavailable) return;
    if (forSale && opener) detailOpener.current = opener;
    void lookup(async () => {
      const detail = await api.product(id);
      const photo = detail.images?.find((image) => image.primary) ?? detail.images?.[0];
      const active = detail.variants.filter((item) => item.status === 'active');
      if (forSale) setKnownVariants((previous) => ({ ...previous, [id]: active }));
      const preview = previews.find((entry) => entry.productId === id);
      if (allowDirectAdd && detail.product.status === 'active' && active.length === 1 && preview?.minPrice != null && preview.stockReadable && (preview.available ?? 0) > 0) {
        const [item] = active;
        setSelection(null);
        await onSelect({ id: item.id, sku: item.sku, product: detail.product.name, size: primaryLabel(item), color: item.color,
          photoURL: photo ? api.productPhotoURL(id, photo.id) : undefined });
        restoreFocus(opener ?? null);
        return;
      }
      setVariantPage(0);
      setSelection({ id, product: detail.product.name, productStatus: detail.product.status, variants: detail.variants, photo });
      if (!detail.variants.length) setFeedback('Este producto todavía no tiene variantes.');
    });
  }

  async function selectBarcode(code: string) {
    const result = await api.barcode(code);
    const photoId = previews.find((preview) => preview.productId === result.product.id)?.photoId;
    await onSelect({ id: result.variant.id, sku: result.variant.sku, product: result.product.name,
      size: primaryLabel(result.variant), color: result.variant.color,
      photoURL: photoId ? api.productPhotoURL(result.product.id, photoId) : undefined });
  }

  function scanCode(input: HTMLInputElement) {
    if (unavailable) return;
    const code = input.value.trim();
    if (!barcodePattern.test(code)) {
      setError('Ingresá un código válido.');
      return;
    }
    input.select();
    void lookup(async () => {
      try {
        await selectBarcode(code);
      } finally {
        if (forSale) restoreFocus(input);
      }
    });
  }

  function scan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const input = event.currentTarget.elements.namedItem('inventoryBarcode');
    if (input instanceof HTMLInputElement) scanCode(input);
  }

  const variants = selection && selection.variants.length > 0 && <>
    <ul className="catalog-items" aria-label="Variantes encontradas">
      {(forSale ? visibleVariants : selection.variants).map(item => {
        const reading = variantRead?.key === quoteKey ? variantRead.rows[item.id] : undefined;
        const state = commercialStatus({
          active: selection.productStatus === 'active' && item.status === 'active',
          price: reading?.price?.amount, stock: reading?.stock?.item.sellable,
          error: Boolean(reading?.error),
        });
        return <li key={item.id} className={forSale ? 'pos-variant-option' : undefined}>
          {forSale && <div className="pos-variant-identity"><strong>{variantLabel(item) || 'Sin atributos'}</strong><small>SKU {item.sku}</small></div>}
          {forSale && <div className="pos-variant-quote">
            <strong>{reading?.price ? reading.price.amount === null ? 'Sin precio' : reading.price.currency + ' ' + posPrice(reading.price.amount) : 'Precio por verificar'}</strong>
            <small>{reading?.stock ? reading.stock.item.sellable === null ? 'Sin saldo registrado' : reading.stock.item.sellable + ' u. vendibles' : reading?.error ? 'Consulta no disponible' : 'Consultando disponibilidad…'}</small>
            <span className={'commercial-readiness ' + state.tone}>{state.label}</span>
          </div>}
          <button type="button" className={forSale ? 'pos-variant-add' : 'catalog-link'}
            disabled={unavailable || (forSale && !state.ready)} onClick={async event => {
              const control = event.currentTarget;
              await onSelect({ id: item.id, sku: item.sku, product: selection.product, size: primaryLabel(item), color: item.color,
                photoURL: selection.photo ? api.productPhotoURL(selection.id, selection.photo.id) : undefined });
              if (forSale) restoreFocus(control);
            }}>
            {forSale ? <>Agregar<span className="pos-sr-only"> {item.sku}</span></> : item.sku}
            {!forSale && item.status === 'inactive' && ' (inactiva)'}
          </button>
        </li>;
      })}
    </ul>
    {forSale && <div className="pos-variant-footer">
      <small>La venta vuelve a validar precio y disponibilidad.</small>
      {variantRead?.key === quoteKey && Object.values(variantRead.rows).some(row => row.error) &&
        <button type="button" className="pos-secondary" onClick={() => setQuoteRetry(value => value + 1)}>Reintentar variantes</button>}
      {selection.variants.length > 8 && <nav className="actions" aria-label="Páginas de variantes">
        <button type="button" disabled={variantPage === 0} onClick={() => setVariantPage(value => value - 1)}>Anterior</button>
        <span>Página {variantPage + 1}</span>
        <button type="button" disabled={(variantPage + 1) * 8 >= selection.variants.length} onClick={() => setVariantPage(value => value + 1)}>Siguiente</button>
      </nav>}
    </div>}
  </>;

  return (
    <>
      {forSale && <div className="pos-catalog-tools">
        <form className="pos-unified-search" aria-label="Buscar por nombre, SKU o código de barras" onSubmit={searchUnified}>
          <label>
            <span className="pos-sr-only">{scanMode ? 'Código de barras' : 'Nombre, SKU o código de barras'}</span>
            <input ref={unifiedInput} name="posQuery" type="search" maxLength={scanMode ? 80 : 160} readOnly={unavailable}
              placeholder={scanMode ? 'Escaneá o ingresá el código' : 'Nombre, SKU o código'} />
          </label>
          <button className="pos-secondary pos-search-submit" disabled={unavailable}>{scanMode ? 'Agregar' : 'Buscar'}</button>
          <button type="button" className="pos-secondary pos-scan-toggle" aria-pressed={scanMode} disabled={unavailable}
            onClick={() => {
              setScanMode(!scanMode);
              unifiedInput.current?.focus();
              unifiedInput.current?.select();
            }}><span aria-hidden="true">▥</span> Escanear</button>
        </form>
        {categories.length > 0 && <div className="pos-category-filters" role="group" aria-label="Categorías">
          <button type="button" disabled={unavailable} aria-pressed={!listing?.categoryId} onClick={() => filterCategory(null)}>Todos</button>
          {categories.slice(0, 3).map((category) => <button key={category.id} type="button" disabled={unavailable}
            aria-pressed={listing?.categoryId === category.id} onClick={() => filterCategory(category.id)}>{category.name}</button>)}
          {categories.length > 3 && <label><span className="pos-sr-only">Más categorías</span>
            <select disabled={unavailable} value={listing?.categoryId ?? ''} onChange={(event) => filterCategory(event.target.value || null)}>
              <option value="">Más categorías</option>
              {categories.map((category) => <option key={category.id} value={category.id}>{category.name}</option>)}
            </select>
          </label>}
        </div>}
      </div>}
      {forSale && categoryError && <details className="auxiliary-status"><summary>Filtro de categorías no disponible</summary><p>Podés seguir buscando productos. <button type="button" onClick={() => setCategoryAttempt(value => value + 1)}>Reintentar categorías</button></p></details>}
      <form aria-label={forSale ? "Escanear para vender" : "Escanear para inventario"} className={forSale ? "inventory-form pos-scan-form" : "inventory-form"} onSubmit={scan}>
        <label>Código de barras<input name="inventoryBarcode" type="search" maxLength={80} required disabled={!forSale && disabled} readOnly={forSale && unavailable} /></label>
        <button className={forSale ? 'pos-secondary' : undefined} disabled={unavailable}>{forSale ? 'Agregar' : 'Buscar código'}</button>
      </form>
      <form className={forSale ? 'inventory-form pos-catalog-search' : 'inventory-form'} aria-label={forSale ? "Buscar productos para vender" : "Buscar variante para inventario"} onSubmit={search}>
        <label>Nombre o SKU<input name="variantQuery" type="search" maxLength={160} required={!forSale} disabled={disabled} /></label>
        {forSale && categories.length > 0 && <label>Categoría
          <select name="posCategory" disabled={unavailable}>
            <option value="">Todas las categorías</option>
            {categories.map((category) => <option key={category.id} value={category.id}>{category.name}</option>)}
          </select>
        </label>}
        <button className={forSale ? 'pos-secondary' : undefined} disabled={unavailable}>{forSale ? 'Buscar' : 'Buscar variante'}</button>
      </form>
      {starting && <p role="status">Cargando catálogo…</p>}
      {feedback && <p role="status">{feedback}</p>}
      {error && <p role="alert">{error}</p>}
      {favorites && favorites.length > 0 && <details className="pos-favorites" aria-label="Favoritos de la sesión">
        <summary>Favoritos ({favorites.length})</summary>
        <ul className="catalog-items pos-product-grid">{favorites.map((entry) => (
          <li key={entry.id}>
            <button type="button" className="catalog-link" disabled={unavailable} onClick={(event) => openProduct(entry.id, false, event.currentTarget)}>{entry.name}</button>
            <button type="button" className="pos-favorite-toggle pos-secondary" disabled={unavailable}
              aria-label={'Quitar de favoritos: ' + entry.name} onClick={() => onToggleFavorite?.(entry)}>★</button>
          </li>
        ))}</ul>
      </details>}
      {forSale && selection && <div onKeyDown={(event) => {
        if (event.key === 'Escape' && !unavailable) {
          event.stopPropagation();
          closeProduct();
        }
      }}>
        <div className="pos-product-detail">
          {selection.photo && <img src={api.productPhotoURL(selection.id, selection.photo.id)} alt="" />}
          <strong tabIndex={-1} ref={detailHeading}>{selection.product}</strong>
          <button type="button" className="pos-secondary pos-close-variants" disabled={unavailable} onClick={closeProduct}>Cerrar variantes</button>
        </div>
        {previews.some((preview) => preview.productId === selection.id && preview.currency && preview.minPrice == null)
          && <p className="pos-validation-note">Este producto no tiene precio en la lista activa. Una variante sin precio queda pendiente en el carrito.</p>}
        {variants}
      </div>}
      {products.length > 0 && <ul className={forSale ? 'catalog-items pos-catalog-grid' : 'catalog-items'}>{products.map((item) => {
        if (!forSale) return <li key={item.id}>
          <button type="button" className="catalog-link" disabled={unavailable} onClick={() => openProduct(item.id)}>
            <strong>{item.name}</strong>
          </button>
        </li>;

        const favorite = favorites?.some((entry) => entry.id === item.id) ?? false;
        const preview = previews.find((entry) => entry.productId === item.id);
        const known = knownVariants[item.id];
        const missingPrice = Boolean(preview?.currency && preview.minPrice == null);
        const knownAvailable = item.status === 'active' && preview?.stockReadable && (preview.available ?? 0) > 0 && preview.minPrice != null;
        const action = !knownAvailable || (known?.length ?? 0) !== 1 ? 'Ver variantes' : 'Agregar';
        const metadata = known?.length === 1
          ? [known[0].sku, variantLabel(known[0])].filter(Boolean).join(' · ')
          : known && known.length > 1 ? known.length + ' variantes' : '';

        return <li key={item.id} className="pos-product-card">
          <span className="pos-card-image">
            {preview?.photoId ? <img src={api.productPhotoURL(item.id, preview.photoId)} loading="lazy" alt="" /> : 'Sin foto'}
          </span>
          <button type="button" className="pos-card-title" title={item.name} disabled={unavailable} onClick={(event) => openProduct(item.id, false, event.currentTarget)}>
            <strong>{item.name}</strong>
          </button>
          <span className="pos-card-sku" title={metadata || undefined}>{metadata || 'Consultá talles y colores'}
            <small className="pos-card-availability">{item.status !== 'active' ? 'Inactivo' : !preview?.stockReadable ? 'Disponibilidad no verificada'
              : preview.available === null ? 'Sin saldo registrado' : preview.available + ' u. vendibles'}</small>
          </span>
          <div className="pos-card-price" data-missing={missingPrice || undefined}>
            {preview?.minPrice == null ? missingPrice ? 'Sin precio' : 'Precio por variante' :
              preview.currency + ' ' + posPrice(preview.minPrice)
              + (preview.maxPrice && preview.maxPrice !== preview.minPrice ? ' a ' + posPrice(preview.maxPrice) : '')}
            {preview?.pricePartial && <small>Algunas variantes sin precio</small>}
          </div>
          <button type="button" className="catalog-link pos-card-action" disabled={unavailable} aria-label={action + ': ' + item.name}
            onClick={(event) => openProduct(item.id, action === 'Agregar', event.currentTarget)}>
            {action === 'Agregar' && <span aria-hidden="true">+ </span>}{action}
          </button>
          {onToggleFavorite && <button type="button" className="pos-favorite-toggle pos-secondary"
            disabled={unavailable} aria-pressed={favorite} aria-label={'Favorito: ' + item.name}
            onClick={() => onToggleFavorite(item)}>{favorite ? '★' : '☆'}</button>}
        </li>;
      })}</ul>}
      {forSale && !listing && !starting && <button type="button" className="pos-secondary" disabled={unavailable} onClick={() => loadProducts('')}>Explorar catálogo</button>}
      {forSale && listing && (listing.offset > 0 || listing.hasMore) && <nav className="actions" aria-label="Resultados de productos">
        <button type="button" className="pos-secondary" disabled={unavailable || listing.offset === 0} onClick={() => loadProducts(listing.query, Math.max(0, listing.offset - 25), listing.categoryId)}>Anterior</button>
        <button type="button" className="pos-secondary" disabled={unavailable || !listing.hasMore} onClick={() => loadProducts(listing.query, listing.offset + 25, listing.categoryId)}>Siguiente</button>
      </nav>}

      {!forSale && variants}
    </>
  );
}
