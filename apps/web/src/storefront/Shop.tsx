import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

import { ShopError, shopAPI, type Shop as ShopData, type ShopProduct, type ShopProductDetail, type ShopVariant } from './api';
import { addLine, money, orderMessage, parseCart, priceText, setQuantity, total, units, variantLabel, whatsappURL, maxQuantity, type CartLine } from './cart';

type Route = { slug: string; view: 'catalog' } | { slug: string; view: 'product'; id: string } | { slug: string; view: 'cart' };

export function readRoute(pathname: string): Route | null {
  const match = /^\/tienda\/([a-z0-9]+(?:-[a-z0-9]+)*)(?:\/(?:(carrito)|producto\/([0-9a-f-]{36})))?\/?$/.exec(pathname);
  if (!match) return null;
  const [, slug, cart, id] = match;
  if (cart) return { slug, view: 'cart' };
  if (id) return { slug, view: 'product', id };
  return { slug, view: 'catalog' };
}

function pathOf(route: Route) {
  const base = '/tienda/' + route.slug;
  return route.view === 'cart' ? base + '/carrito' : route.view === 'product' ? base + '/producto/' + route.id : base;
}

function useCart(slug: string) {
  const key = 'tienda:' + slug + ':carrito';
  const [lines, setLines] = useState<CartLine[]>(() => {
    try { return parseCart(localStorage.getItem(key)); } catch { return []; }
  });
  useEffect(() => {
    try { localStorage.setItem(key, JSON.stringify(lines)); } catch { /* private mode: the cart lives in memory */ }
  }, [key, lines]);
  useEffect(() => {
    const sync = (event: StorageEvent) => { if (event.key === key) setLines(parseCart(event.newValue)); };
    window.addEventListener('storage', sync);
    return () => window.removeEventListener('storage', sync);
  }, [key]);
  return [lines, setLines] as const;
}

export function Shop() {
  const [route, setRoute] = useState(() => readRoute(location.pathname));
  // Pages opened inside the shop, so «Volver» returns to the previous one instead of leaving the site.
  const depth = useRef(0);
  useEffect(() => {
    const sync = () => { depth.current = Math.max(0, depth.current - 1); setRoute(readRoute(location.pathname)); };
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, []);
  const go = useCallback((next: Route, replace = false) => {
    if (replace) history.replaceState(null, '', pathOf(next));
    else { history.pushState(null, '', pathOf(next)); depth.current++; }
    setRoute(next);
    window.scrollTo({ top: 0 });
  }, []);
  const back = useCallback((fallback: Route) => {
    if (depth.current > 0) history.back(); else go(fallback, true);
  }, [go]);
  if (!route) return <Closed title="Esta dirección no existe" text="Revisá el enlace que te compartieron." />;
  return <Store key={route.slug} route={route} go={go} back={back} />;
}

function Store({ route, go, back: goBack }: { route: Route; go: (route: Route, replace?: boolean) => void; back: (fallback: Route) => void }) {
  const [shop, setShop] = useState<ShopData | null>(null);
  const [error, setError] = useState<ShopError | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [lines, setLines] = useCart(route.slug);
  const [toast, setToast] = useState('');
  const catalogScroll = useRef(0);

  useEffect(() => {
    const controller = new AbortController();
    shopAPI.shop(route.slug, controller.signal).then((data) => { setShop(data); setError(null); }).catch((cause: unknown) => {
      if (cause instanceof ShopError) setError(cause);
    });
    return () => controller.abort();
  }, [route.slug, attempt]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(''), 3200);
    return () => window.clearTimeout(timer);
  }, [toast]);

  useEffect(() => { setToast(''); }, [route]);

  useEffect(() => {
    if (shop) document.title = shop.name + (route.view === 'cart' ? ' · Carrito' : ' · Tienda');
  }, [shop, route.view]);

  const open = useCallback((next: Route) => {
    if (route.view === 'catalog') catalogScroll.current = window.scrollY;
    go(next);
  }, [go, route.view]);

  if (error?.status === 404) return <Closed title="Esta tienda no está disponible" text="Puede que todavía no esté publicada o que el enlace haya cambiado." />;
  if (error && !shop) return <Closed title="No pudimos abrir la tienda" text={error.message} action={<button type="button" className="shop-btn primary" onClick={() => setAttempt((n) => n + 1)}>Reintentar</button>} />;
  if (!shop) return <div className="shop-loading" role="status"><span className="shop-spinner" aria-hidden="true" />Cargando la tienda…</div>;

  const count = units(lines);
  const back = () => goBack({ slug: route.slug, view: 'catalog' });

  return <div className="shop">
    <header className="shop-header">
      <a className="shop-brand" href={'/tienda/' + shop.slug} onClick={(event) => { event.preventDefault(); go({ slug: shop.slug, view: 'catalog' }); }}>
        <span className="shop-logo" aria-hidden="true">{shop.name.trim().charAt(0).toUpperCase()}</span>
        <span><strong>{shop.name}</strong><small>{shop.branch}</small></span>
      </a>
      <a className="shop-cart-button" href={'/tienda/' + shop.slug + '/carrito'} aria-label={'Carrito, ' + (count === 1 ? '1 producto' : count + ' productos')}
        onClick={(event) => { event.preventDefault(); open({ slug: shop.slug, view: 'cart' }); }}>
        <CartIcon />{count > 0 && <span className="shop-badge">{count}</span>}
      </a>
    </header>
    <main className="shop-main">
      {route.view === 'catalog' && <Catalog shop={shop} restore={catalogScroll} onOpen={(product) => open({ slug: shop.slug, view: 'product', id: product.id })} />}
      {route.view === 'product' && <ProductView key={route.id} shop={shop} id={route.id} onBack={back}
        onAdd={(line) => { setLines((current) => addLine(current, line)); setToast('Agregaste ' + line.name + (variantLabel(line) ? ' (' + variantLabel(line) + ')' : '')); }} />}
      {route.view === 'cart' && <CartView shop={shop} lines={lines} setLines={setLines} onContinue={() => go({ slug: shop.slug, view: 'catalog' })}
        onOpen={(id) => open({ slug: shop.slug, view: 'product', id })} />}
    </main>
    {route.view !== 'cart' && count > 0 && <div className="shop-dock">
      <button type="button" className="shop-btn primary shop-dock-button" onClick={() => open({ slug: shop.slug, view: 'cart' })}>
        <span>Ver carrito · {count === 1 ? '1 producto' : count + ' productos'}</span><b>{money(total(lines), shop.currency)}</b>
      </button>
    </div>}
    {toast && <div className="shop-toast" role="status">✓ {toast}</div>}
    <footer className="shop-footer">
      <p><b>{shop.name}</b>{shop.address ? ' · ' + shop.address : ''}</p>
      <p>Armá tu pedido y envialo por WhatsApp. El local confirma precio, stock y entrega.</p>
    </footer>
  </div>;
}

function Catalog({ shop, restore, onOpen }: { shop: ShopData; restore: { current: number }; onOpen: (product: ShopProduct) => void }) {
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  useEffect(() => {
    if (restore.current) window.scrollTo({ top: restore.current });
  }, [restore]);
  const products = useMemo(() => {
    const needle = normalize(query);
    return shop.products.filter((product) => (!category || product.categoryId === category) &&
      (!needle || normalize(product.name + ' ' + (product.brand ?? '')).includes(needle)));
  }, [shop.products, query, category]);
  return <>
    {shop.welcome && <p className="shop-welcome">{shop.welcome}</p>}
    <div className="shop-filters">
      <label className="shop-search">
        <SearchIcon />
        <input type="search" value={query} placeholder="Buscar productos" aria-label="Buscar productos" enterKeyHint="search"
          onChange={(event) => setQuery(event.target.value)} />
      </label>
      {shop.categories.length > 1 && <div className="shop-chips" role="group" aria-label="Categorías">
        <button type="button" aria-pressed={!category} onClick={() => setCategory('')}>Todo</button>
        {shop.categories.map((item) => <button key={item.id} type="button" aria-pressed={category === item.id}
          onClick={() => setCategory(category === item.id ? '' : item.id)}>{item.name}</button>)}
      </div>}
    </div>
    {shop.products.length === 0 ? <Empty title="Pronto vas a ver los productos acá" text="El local todavía está armando su tienda." />
      : products.length === 0 ? <Empty title="No encontramos productos" text="Probá con otra palabra o elegí otra categoría." />
        : <ul className="shop-grid" aria-label="Productos">{products.map((product) => <li key={product.id}>
          <a className="shop-card" href={'/tienda/' + shop.slug + '/producto/' + product.id} data-soldout={!product.available || undefined}
            onClick={(event) => { if (event.metaKey || event.ctrlKey || event.shiftKey) return; event.preventDefault(); onOpen(product); }}>
            <span className="shop-card-image">
              <img src={shopAPI.image(shop.slug, product.imageId)} alt="" loading="lazy" decoding="async" />
              {!product.available && <span className="shop-tag">Agotado</span>}
            </span>
            <span className="shop-card-body">
              <span className="shop-card-name">{product.name}</span>
              <span className="shop-card-price">{product.priceFrom !== product.priceTo && <small>Desde </small>}{priceText(product.priceFrom, shop.currency)}</span>
            </span>
          </a>
        </li>)}</ul>}
  </>;
}

function ProductView({ shop, id, onBack, onAdd }: { shop: ShopData; id: string; onBack: () => void; onAdd: (line: CartLine) => void }) {
  const [product, setProduct] = useState<ShopProductDetail | null>(null);
  const [error, setError] = useState<ShopError | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [color, setColor] = useState('');
  const [size, setSize] = useState('');
  const [quantity, setQty] = useState(1);
  const [photo, setPhoto] = useState(0);
  const [warning, setWarning] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    shopAPI.product(shop.slug, id, controller.signal).then((data) => {
      setProduct(data);
      setError(null);
      // Preselect what has a single choice, preferring an available variant.
      const first = data.variants.find((variant) => variant.available) ?? data.variants[0];
      const sizes = unique(data.variants.map((variant) => variant.size));
      setColor(first?.color ?? '');
      if (sizes.length === 1) setSize(sizes[0]);
    }).catch((cause: unknown) => { if (cause instanceof ShopError) setError(cause); });
    return () => controller.abort();
  }, [shop.slug, id, attempt]);

  if (error) return <div className="shop-page">
    <BackLink onBack={onBack} />
    <Empty title={error.status === 404 ? 'Este producto ya no está en la tienda' : 'No pudimos abrir el producto'} text={error.status === 404 ? 'Mirá el resto del catálogo.' : error.message}
      action={error.status === 404 ? undefined : <button type="button" className="shop-btn" onClick={() => setAttempt((n) => n + 1)}>Reintentar</button>} />
  </div>;
  if (!product) return <div className="shop-page"><BackLink onBack={onBack} /><div className="shop-loading inline" role="status"><span className="shop-spinner" aria-hidden="true" />Cargando…</div></div>;

  const colors = unique(product.variants.map((variant) => variant.color));
  const sizes = unique(product.variants.map((variant) => variant.size));
  const hasColors = colors.some(Boolean);
  const hasSizes = sizes.some(Boolean);
  const pick = (wantedColor: string, wantedSize: string) => product.variants.find((variant) => variant.color === wantedColor && variant.size === wantedSize);
  const selected = pick(hasColors ? color : colors[0], hasSizes ? size : sizes[0]);
  const sizeState = (value: string): 'ok' | 'soldout' | 'none' => {
    const variant = pick(hasColors ? color : colors[0], value);
    return !variant ? 'none' : variant.available ? 'ok' : 'soldout';
  };
  const colorAvailable = (value: string) => product.variants.some((variant) => variant.color === value && variant.available);
  const swatch = (value: string) => product.variants.find((variant) => variant.color === value)?.colorHex ?? null;
  const price = selected?.price ?? product.priceFrom;

  function add() {
    if (hasColors && !color) { setWarning('Elegí un color.'); return; }
    if (hasSizes && !size) { setWarning('Elegí un talle.'); return; }
    if (!selected) { setWarning('Esa combinación no está disponible. Probá con otro talle o color.'); return; }
    if (!selected.available) { setWarning('Ese talle y color está agotado.'); return; }
    setWarning('');
    onAdd({ variantId: selected.id, productId: product!.id, name: product!.name, size: selected.size, color: selected.color,
      price: selected.price, imageId: product!.images[0] ?? product!.imageId, quantity });
    setQty(1);
  }

  return <article className="shop-page shop-product">
    <BackLink onBack={onBack} />
    <div className="shop-product-layout">
      <Gallery slug={shop.slug} images={product.images} name={product.name} current={photo} onChange={setPhoto} />
      <div className="shop-product-info">
        {(product.brand || product.category) && <p className="shop-eyebrow">{[product.brand, product.category].filter(Boolean).join(' · ')}</p>}
        <h1>{product.name}</h1>
        <p className="shop-product-price">{!selected && product.priceFrom !== product.priceTo && <small>Desde </small>}{priceText(price, shop.currency)}</p>
        {!product.available && <p className="shop-soldout-note">Por ahora está agotado. Consultá por WhatsApp si va a volver a entrar.</p>}

        {hasColors && <fieldset className="shop-options">
          <legend>Color{color && <span>: {color}</span>}</legend>
          <div className="shop-swatches">{colors.map((value) => {
            const hex = swatch(value);
            return <button key={value} type="button" aria-pressed={color === value} data-soldout={!colorAvailable(value) || undefined}
              onClick={() => { setColor(value); setWarning(''); if (size && !pick(value, size)) setSize(''); }}
              aria-label={value + (colorAvailable(value) ? '' : ', agotado')}>
              {hex && <span className="shop-swatch" style={{ background: hex }} aria-hidden="true" />}{value}
            </button>;
          })}</div>
        </fieldset>}

        {hasSizes && <fieldset className="shop-options">
          <legend>Talle{size && <span>: {size}</span>}</legend>
          <div className="shop-sizes">{sizes.map((value) => {
            const state = sizeState(value);
            return <button key={value} type="button" aria-pressed={size === value} disabled={state === 'none'} data-soldout={state === 'soldout' || undefined}
              onClick={() => { setSize(value); setWarning(''); }} aria-label={'Talle ' + value + (state === 'soldout' ? ', agotado' : state === 'none' ? ', no disponible en este color' : '')}>
              {value}
            </button>;
          })}</div>
        </fieldset>}

        {selected && !selected.available && <p className="shop-warning" role="status">{variantLabel(selected) || 'Este producto'} está agotado.</p>}
        {warning && <p className="shop-warning" role="alert">{warning}</p>}

        <div className="shop-buy">
          <Stepper value={quantity} onChange={setQty} label={'Cantidad de ' + product.name} />
          <button type="button" className="shop-btn primary shop-add" disabled={!product.available || (selected ? !selected.available : false)} onClick={add}>
            {product.available ? 'Agregar al carrito' : 'Agotado'}
          </button>
        </div>
        {product.description && <div className="shop-description"><h2>Descripción</h2><p>{product.description}</p></div>}
      </div>
    </div>
  </article>;
}

function Gallery({ slug, images, name, current, onChange }: { slug: string; images: string[]; name: string; current: number; onChange: (index: number) => void }) {
  const track = useRef<HTMLDivElement>(null);
  const scrollTo = (index: number) => {
    const element = track.current;
    if (!element) return;
    element.scrollTo({ left: index * element.clientWidth, behavior: 'smooth' });
  };
  return <div className="shop-gallery">
    <div ref={track} className="shop-gallery-track" tabIndex={0} aria-label={'Fotos de ' + name}
      onScroll={(event) => { const element = event.currentTarget; onChange(Math.round(element.scrollLeft / Math.max(element.clientWidth, 1))); }}>
      {images.map((image, index) => <img key={image} src={shopAPI.image(slug, image, 'detail')} alt={index === 0 ? name : name + ', foto ' + (index + 1)}
        loading={index === 0 ? 'eager' : 'lazy'} decoding="async" />)}
    </div>
    {images.length > 1 && <div className="shop-gallery-dots" role="group" aria-label="Elegir foto">
      {images.map((image, index) => <button key={image} type="button" aria-pressed={current === index} aria-label={'Foto ' + (index + 1)} onClick={() => scrollTo(index)} />)}
    </div>}
    {images.length > 1 && <div className="shop-thumbs">{images.map((image, index) =>
      <button key={image} type="button" aria-pressed={current === index} aria-label={'Ver foto ' + (index + 1)} onClick={() => scrollTo(index)}>
        <img src={shopAPI.image(slug, image)} alt="" loading="lazy" />
      </button>)}</div>}
  </div>;
}

function CartView({ shop, lines, setLines, onContinue, onOpen }: {
  shop: ShopData; lines: CartLine[]; setLines: (update: (lines: CartLine[]) => CartLine[]) => void; onContinue: () => void; onOpen: (productId: string) => void;
}) {
  const [customer, setCustomer] = useState(() => { try { return localStorage.getItem('tienda:cliente') ?? ''; } catch { return ''; } });
  const [note, setNote] = useState('');
  const [sent, setSent] = useState(false);
  const [checked, setChecked] = useState<Record<string, ShopVariant | null>>({});

  // Refresh prices and availability of the lines with the current catalog.
  const products = unique(lines.map((line) => line.productId)).join(',');
  useEffect(() => {
    if (!products) return;
    const controller = new AbortController();
    void Promise.all(products.split(',').map((id) => shopAPI.product(shop.slug, id, controller.signal).catch((cause: unknown) =>
      cause instanceof ShopError && cause.status === 404 ? null : undefined))).then((details) => {
      const found: Record<string, ShopVariant | null> = {};
      products.split(',').forEach((id, index) => {
        const detail = details[index];
        if (detail === undefined) return;
        for (const line of lines.filter((item) => item.productId === id)) found[line.variantId] = detail?.variants.find((variant) => variant.id === line.variantId) ?? null;
      });
      setChecked(found);
      setLines((current) => current.map((line) => { const variant = found[line.variantId]; return variant && variant.price !== line.price ? { ...line, price: variant.price } : line; }));
    });
    return () => controller.abort();
  }, [shop.slug, products]);

  useEffect(() => { try { localStorage.setItem('tienda:cliente', customer); } catch { /* ignore */ } }, [customer]);

  const unavailable = lines.filter((line) => checked[line.variantId] === null || checked[line.variantId]?.available === false);
  const ready = lines.filter((line) => !unavailable.includes(line));
  const link = location.origin + '/tienda/' + shop.slug;
  const href = whatsappURL(shop.whatsapp, orderMessage({ store: shop.name, lines: ready, currency: shop.currency, customer, note, link }));

  if (!lines.length) return <div className="shop-page">
    <h1 className="shop-title">Tu carrito</h1>
    <Empty title="Tu carrito está vacío" text="Elegí productos del catálogo y armá tu pedido." action={<button type="button" className="shop-btn primary" onClick={onContinue}>Ver productos</button>} />
  </div>;

  return <div className="shop-page shop-cart">
    <h1 className="shop-title">Tu carrito <small>{units(lines) === 1 ? '1 producto' : units(lines) + ' productos'}</small></h1>
    <ul className="shop-lines">{lines.map((line) => {
      const off = unavailable.includes(line);
      return <li key={line.variantId} data-off={off || undefined}>
        <button type="button" className="shop-line-image" onClick={() => onOpen(line.productId)} aria-label={'Ver ' + line.name}>
          <img src={shopAPI.image(shop.slug, line.imageId)} alt="" loading="lazy" />
        </button>
        <div className="shop-line-main">
          <b>{line.name}</b>
          <small>{variantLabel(line)}</small>
          {off ? <small className="shop-line-off">{checked[line.variantId] === null ? 'Ya no está en la tienda' : 'Se agotó'} · no se incluye en el pedido</small>
            : <span className="shop-line-price">{priceText(line.price, shop.currency)}</span>}
        </div>
        <div className="shop-line-side">
          {!off && <Stepper value={line.quantity} small onChange={(value) => setLines((current) => setQuantity(current, line.variantId, value))} label={'Cantidad de ' + line.name} />}
          <button type="button" className="shop-remove" onClick={() => setLines((current) => setQuantity(current, line.variantId, 0))} aria-label={'Quitar ' + line.name}>Quitar</button>
        </div>
      </li>;
    })}</ul>

    <div className="shop-summary">
      <div className="shop-total"><span>Total</span><b>{money(total(ready), shop.currency)}</b></div>
      <label className="shop-field"><span>Tu nombre <small>(opcional)</small></span>
        <input value={customer} maxLength={60} autoComplete="name" placeholder="Para que el local sepa quién escribe" onChange={(event) => setCustomer(event.target.value)} />
      </label>
      <label className="shop-field"><span>Comentario <small>(opcional)</small></span>
        <textarea value={note} maxLength={300} rows={2} placeholder="Ej. Retiro en el local / Envío a Palermo" onChange={(event) => setNote(event.target.value)} />
      </label>
      <p className="shop-hint">Te abrimos WhatsApp con el pedido escrito. El local te confirma precio, stock y forma de entrega.</p>
    </div>

    {sent && <div className="shop-sent" role="status">
      <b>¿Pudiste enviar el pedido?</b>
      <span>Si ya lo mandaste por WhatsApp, podés vaciar el carrito.</span>
      <div><button type="button" className="shop-btn" onClick={() => setSent(false)}>Todavía no</button>
        <button type="button" className="shop-btn primary" onClick={() => { setLines(() => []); setSent(false); onContinue(); }}>Sí, vaciar carrito</button></div>
    </div>}

    <div className="shop-dock">
      {ready.length > 0 ? <a className="shop-btn whatsapp shop-dock-button" href={href} target="_blank" rel="noopener" onClick={() => setSent(true)}>
        <WhatsAppIcon /><span>Enviar por WhatsApp</span><b>{money(total(ready), shop.currency)}</b>
      </a> : <button type="button" className="shop-btn shop-dock-button" onClick={onContinue}>Seguir comprando</button>}
    </div>
  </div>;
}

function Stepper({ value, onChange, label, small = false }: { value: number; onChange: (value: number) => void; label: string; small?: boolean }) {
  return <div className={'shop-stepper' + (small ? ' small' : '')} role="group" aria-label={label}>
    <button type="button" onClick={() => onChange(value - 1)} disabled={!small && value <= 1} aria-label="Uno menos">−</button>
    <output aria-live="polite">{value}</output>
    <button type="button" onClick={() => onChange(value + 1)} disabled={value >= maxQuantity} aria-label="Uno más">+</button>
  </div>;
}

function BackLink({ onBack }: { onBack: () => void }) {
  return <button type="button" className="shop-back" onClick={onBack}>← Volver</button>;
}

function Empty({ title, text, action }: { title: string; text: string; action?: ReactNode }) {
  return <div className="shop-empty"><b>{title}</b><p>{text}</p>{action}</div>;
}

function Closed({ title, text, action }: { title: string; text: string; action?: ReactNode }) {
  return <main className="shop-closed"><span className="shop-closed-icon" aria-hidden="true"><CartIcon /></span><h1>{title}</h1><p>{text}</p>{action}</main>;
}

function unique(values: string[]) {
  return [...new Set(values)];
}

function normalize(value: string) {
  return value.toLocaleLowerCase('es-AR').normalize('NFD').replace(/\p{Diacritic}/gu, '').trim();
}

const CartIcon = () => <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" focusable="false">
  <path d="M3 4h2l2.2 10.2a1.5 1.5 0 0 0 1.5 1.2h8.6a1.5 1.5 0 0 0 1.5-1.1L21 8H6.2" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
  <circle cx="9.5" cy="19.5" r="1.4" fill="currentColor" /><circle cx="17" cy="19.5" r="1.4" fill="currentColor" />
</svg>;

const SearchIcon = () => <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
  <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.8" /><path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
</svg>;

const WhatsAppIcon = () => <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" focusable="false">
  <path fill="currentColor" d="M12 2.2a9.7 9.7 0 0 0-8.4 14.6L2.3 21.7l5-1.3A9.7 9.7 0 1 0 12 2.2Zm0 17.7a8 8 0 0 1-4.1-1.1l-.3-.2-3 .8.8-2.9-.2-.3A8 8 0 1 1 12 19.9Zm4.4-6c-.2-.1-1.4-.7-1.7-.8-.2-.1-.4-.1-.5.1l-.8 1c-.1.2-.3.2-.5.1a6.6 6.6 0 0 1-3.3-2.9c-.2-.4.2-.4.7-1.3.1-.2 0-.3 0-.4l-.8-1.8c-.2-.5-.4-.4-.5-.4h-.5a.9.9 0 0 0-.7.3 2.8 2.8 0 0 0-.9 2.1 4.9 4.9 0 0 0 1 2.6 11.2 11.2 0 0 0 4.3 3.8c1.6.7 2.2.7 3 .6a2.6 2.6 0 0 0 1.7-1.2 2.1 2.1 0 0 0 .2-1.2c-.1-.2-.2-.2-.4-.3Z" />
</svg>;
