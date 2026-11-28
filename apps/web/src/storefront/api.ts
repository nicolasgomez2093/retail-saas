// Public storefront API: no session, only what a shopper may see.

export type ShopProduct = {
  id: string;
  name: string;
  categoryId: string | null;
  brand: string | null;
  imageId: string;
  priceFrom: string;
  priceTo: string;
  available: boolean;
};

export type Shop = {
  slug: string;
  name: string;
  welcome: string;
  whatsapp: string;
  currency: string;
  branch: string;
  address: string;
  categories: { id: string; name: string }[];
  products: ShopProduct[];
};

export type ShopVariant = { id: string; size: string; color: string; colorHex: string | null; price: string; available: boolean };

export type ShopProductDetail = ShopProduct & {
  description: string;
  category: string | null;
  images: string[];
  variants: ShopVariant[];
};

export class ShopError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
  }
}

async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  let response: Response;
  try {
    response = await fetch('/api/v1/public/storefronts/' + path, { signal, credentials: 'omit', cache: 'no-store' });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause;
    throw new ShopError(0, 'No pudimos conectar. Revisá tu conexión y volvé a intentar.');
  }
  if (response.status === 404) throw new ShopError(404, 'No encontramos lo que buscás.');
  if (response.status === 429) throw new ShopError(429, 'Hay muchas consultas en este momento. Esperá unos segundos y volvé a intentar.');
  if (!response.ok) throw new ShopError(response.status, 'Algo falló de nuestro lado. Volvé a intentar en un momento.');
  return response.json();
}

export const shopAPI = {
  shop: (slug: string, signal?: AbortSignal) => get<Shop>(encodeURIComponent(slug), signal),
  product: (slug: string, id: string, signal?: AbortSignal) =>
    get<ShopProductDetail>(encodeURIComponent(slug) + '/products/' + encodeURIComponent(id), signal),
  image: (slug: string, imageId: string, size: 'card' | 'detail' = 'card') =>
    '/api/v1/public/storefronts/' + encodeURIComponent(slug) + '/images/' + encodeURIComponent(imageId) + '?size=' + size,
};
