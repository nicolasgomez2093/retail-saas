import { variantLabel } from './quickProduct';
import { useLayoutEffect, useMemo, useState } from 'react';
import { api, ApiError, type ProductDetail, type StockBalance, type StockLocation, type Store } from '../../shared/api/client';
import { VariantPrice, type PriceAccess } from '../pricing/VariantPrice';
import { sellableAvailability } from './variantAvailability';

export function ProductQuickView({ productId, store, stores, revision, access }: {
  productId: string; store: Store | undefined; stores: Store[]; revision: number; access: PriceAccess;
}) {
  const [detail, setDetail] = useState<ProductDetail>();
  const [variantId, setVariantId] = useState('');
  const [locationId, setLocationId] = useState('');
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [stockAttempt, setStockAttempt] = useState(0);
  const [stockError, setStockError] = useState('');
  const [inventory, setInventory] = useState<{ locations: StockLocation[]; items: StockBalance[] }>();
  const storeId = store?.id;
  const hasVariants = Boolean(detail?.variants.length);
  const selectedVariant = detail?.variants.find((variant) => variant.id === variantId) ?? detail?.variants[0];
  const selectedLocation = inventory?.locations.find((location) => location.id === locationId);
  const totals = useMemo(() => inventory && sellableAvailability(inventory.locations, inventory.items), [inventory]);

  useLayoutEffect(() => {
    let pending = true;
    if (!access.isCurrent()) return;
    void api.product(productId).then((data) => {
      if (pending && access.isCurrent()) setDetail(data);
    }).catch((cause: unknown) => {
      if (!pending || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      else setError(cause instanceof ApiError && cause.status === 403
        ? 'No tenés permiso para consultar estas variantes.' : 'No pudimos cargar las variantes.');
    });
    return () => { pending = false; };
  }, [productId, attempt, access]);

  useLayoutEffect(() => {
    let pending = true;
    if (!storeId || !hasVariants || !access.isCurrent()) return;
    void Promise.all([api.locations(storeId), api.stockBalances(storeId, '')]).then(([locations, balances]) => {
      if (pending && access.isCurrent()) setInventory({ locations: locations.items, items: balances.items });
    }).catch((cause: unknown) => {
      if (!pending || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      else setStockError(cause instanceof ApiError && cause.status === 403
        ? 'No tenés permiso para consultar stock en esta sucursal.' : 'No pudimos cargar el stock de la sucursal.');
    });
    return () => { pending = false; };
  }, [storeId, hasVariants, stockAttempt, access]);

  if (error) return <div>
    <p role="alert">{error}</p>
    <button type="button" className="button-secondary" onClick={() => { setError(''); setAttempt((value) => value + 1); }}>Reintentar variantes</button>
  </div>;
  if (!detail) return <p role="status">Cargando variantes…</p>;
  const photo = detail.images.find((image) => image.primary) ?? detail.images[0];
  const balance = inventory?.items.find((item) => item.variantId === selectedVariant?.id && item.locationId === selectedLocation?.id);

  return <section className="product-quick-view" aria-label={'Variantes de ' + detail.product.name}>
    {photo ? <img src={api.productPhotoURL(productId, photo.id)} alt="" /> : <p>Sin foto</p>}
    {!selectedVariant ? <p role="status">Este producto todavía no tiene variantes.</p> : <div>
      {store && <p><strong>Stock vendible · {store.name}</strong></p>}
      {!store && <p>Seleccioná una sucursal autorizada para consultar stock y precios.</p>}
      {store && !inventory && !stockError && <p role="status">Cargando stock vendible…</p>}
      {stockError && <div>
        <p role="alert">{stockError}</p>
        <button type="button" className="button-secondary" onClick={() => {
          setStockError('');
          setStockAttempt((value) => value + 1);
        }}>Reintentar stock</button>
      </div>}
      <ul className="catalog-variant-summary" aria-label="Variantes y stock vendible">
        {detail.variants.map((variant) => {
          const active = detail.product.status === 'active' && variant.status === 'active';
          const available = totals?.get(variant.id);
          return <li key={variant.id}>
            <strong>{variant.sku}</strong>
            <span>Talle: {variant.size || '—'} · Color: {variant.color || '—'}</span>
            <span className={'products-status ' + variant.status}>{variant.status === 'active' ? 'Activa' : 'Inactiva'}</span>
            <span>{!active ? 'No vendible por estado' : available === undefined
              ? totals ? 'Sin saldo vendible registrado' : 'Stock sin verificar'
              : available <= 0 ? 'Sin stock vendible (' + available + ')' : 'Disponible: ' + available}</span>
          </li>;
        })}
      </ul>
      <label>Variante para consultar precio o ubicación
        <select value={selectedVariant.id} onChange={(event) => setVariantId(event.target.value)}>
          {detail.variants.map((variant) => <option key={variant.id} value={variant.id}>
            {[variant.sku, variantLabel(variant)].filter(Boolean).join(' · ')}
          </option>)}
        </select>
      </label>
      {store && selectedVariant.status === 'active' && detail.product.status === 'active' &&
        <VariantPrice key={selectedVariant.id} variantId={selectedVariant.id} store={store} stores={stores} revision={revision} access={access} />}
      {inventory && inventory.locations.length > 0 && <>
        <label>Ubicación para consultar stock
          <select value={locationId} onChange={(event) => setLocationId(event.target.value)}>
            <option value="">Elegí una ubicación</option>
            {inventory.locations.map((location) => <option key={location.id} value={location.id}>{location.name}</option>)}
          </select>
        </label>
        {selectedLocation && <p role="status">{balance
          ? 'Disponible en esta ubicación: ' + balance.available : 'Sin saldo registrado para esta variante.'}</p>}
      </>}
    </div>}
  </section>;
}
