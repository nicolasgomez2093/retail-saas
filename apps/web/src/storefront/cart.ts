// Cart of the public storefront. Amounts travel as canonical decimal strings ("25000.00") and are
// added in cents with BigInt, never with floating point.

export type CartLine = {
  variantId: string;
  productId: string;
  name: string;
  size: string;
  color: string;
  price: string;
  imageId: string;
  quantity: number;
};

export const maxQuantity = 20;

export function cents(amount: string): bigint {
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(amount.trim());
  if (!match) throw new Error('Importe inválido: ' + amount);
  return BigInt(match[1]) * 100n + BigInt((match[2] ?? '').padEnd(2, '0'));
}

/** Formats cents as the shopper reads them: «$ 25.000» or «$ 1.250,50» for ARS, «USD 30» otherwise. */
export function money(value: bigint, currency = 'ARS'): string {
  const negative = value < 0n;
  const absolute = negative ? -value : value;
  const units = (absolute / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  const decimals = absolute % 100n;
  const text = units + (decimals ? ',' + decimals.toString().padStart(2, '0') : '');
  return (negative ? '-' : '') + (currency === 'ARS' ? '$ ' : currency + ' ') + text;
}

export function priceText(amount: string, currency = 'ARS') {
  return money(cents(amount), currency);
}

export function variantLabel(line: Pick<CartLine, 'size' | 'color'>) {
  return [line.size && 'Talle ' + line.size, line.color].filter(Boolean).join(' · ');
}

export function addLine(lines: CartLine[], line: CartLine): CartLine[] {
  const existing = lines.find((item) => item.variantId === line.variantId);
  if (!existing) return [...lines, { ...line, quantity: clamp(line.quantity) }];
  return lines.map((item) => item.variantId === line.variantId
    ? { ...item, ...line, quantity: clamp(item.quantity + line.quantity) } : item);
}

export function setQuantity(lines: CartLine[], variantId: string, quantity: number): CartLine[] {
  if (quantity < 1) return lines.filter((item) => item.variantId !== variantId);
  return lines.map((item) => item.variantId === variantId ? { ...item, quantity: clamp(quantity) } : item);
}

export function units(lines: CartLine[]) {
  return lines.reduce((sum, line) => sum + line.quantity, 0);
}

export function total(lines: CartLine[]) {
  return lines.reduce((sum, line) => sum + cents(line.price) * BigInt(line.quantity), 0n);
}

function clamp(quantity: number) {
  return Math.max(1, Math.min(maxQuantity, Math.trunc(quantity) || 1));
}

/** Reads a stored cart, dropping anything that does not look like a cart line. */
export function parseCart(raw: string | null): CartLine[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value.filter((line): line is CartLine => typeof line === 'object' && line !== null &&
      typeof line.variantId === 'string' && typeof line.productId === 'string' && typeof line.name === 'string' &&
      typeof line.size === 'string' && typeof line.color === 'string' && typeof line.imageId === 'string' &&
      typeof line.price === 'string' && /^\d+(\.\d{1,2})?$/.test(line.price) &&
      Number.isInteger(line.quantity) && line.quantity >= 1 && line.quantity <= maxQuantity).slice(0, 100);
  } catch {
    return [];
  }
}

export function orderMessage({ store, lines, currency, customer, note, link }: {
  store: string; lines: CartLine[]; currency: string; customer: string; note: string; link: string;
}) {
  const rows = lines.map((line, index) => {
    const detail = variantLabel(line);
    const subtotal = cents(line.price) * BigInt(line.quantity);
    return `${index + 1}. ${line.name}${detail ? ' — ' + detail : ''}\n   ${line.quantity} × ${priceText(line.price, currency)} = ${money(subtotal, currency)}`;
  });
  const extra = [customer.trim() && `Nombre: ${customer.trim()}`, note.trim() && `Comentario: ${note.trim()}`].filter(Boolean);
  return [
    `¡Hola ${store}! Quiero hacer este pedido:`,
    '',
    ...rows,
    '',
    `*Total: ${money(total(lines), currency)}*`,
    ...extra,
    '',
    `Pedido armado en ${link}`,
  ].join('\n');
}

export function whatsappURL(number: string, text: string) {
  return 'https://wa.me/' + number.replace(/\D/g, '') + '?text=' + encodeURIComponent(text);
}

const letterSizes = ['XXS', 'XS', 'S', 'M', 'L', 'XL', 'XXL', '2XL', 'XXXL', '3XL', '4XL', '5XL'];
const oneSize = ['U', 'UNICO', 'TU', 'TALLE UNICO'];

function plain(value: string) {
  return value.normalize('NFD').replace(/\p{Diacritic}/gu, '').trim().toUpperCase();
}

// Shoppers expect S, M, L or 36, 38, 40; the catalog may hold them in the order they were created.
export function sortSizes(sizes: string[]): string[] {
  const rank = (value: string) => {
    const key = plain(value);
    if (/^\d+([.,]\d+)?$/.test(key)) return Number(key.replace(',', '.'));
    const letter = letterSizes.indexOf(key);
    if (letter >= 0) return 1000 + letter;
    if (oneSize.includes(key)) return 2000;
    return null;
  };
  const ranks = sizes.map(rank);
  if (ranks.some((value) => value === null)) return sizes;
  return sizes.map((size, index) => ({ size, rank: ranks[index] as number })).sort((a, b) => a.rank - b.rank).map((item) => item.size);
}

const colorNames: Record<string, string> = {
  NEGRO: '#1f1f1f', BLANCO: '#f6f5f2', GRIS: '#8d9093', 'GRIS MELANGE': '#a9abad', GRAFITO: '#4a4d50', PLOMO: '#6e7275',
  AZUL: '#2f4f86', MARINO: '#1f2f52', 'AZUL MARINO': '#1f2f52', CELESTE: '#8fbfe6', JEAN: '#4a6fa5', INDIGO: '#3b4a8a',
  ROJO: '#c0392b', BORDO: '#6d1a2a', BORDEAUX: '#6d1a2a', VINO: '#6d1a2a', ROSA: '#f2a7bd', FUCSIA: '#d63384', CORAL: '#f07b6b',
  VERDE: '#3f7a4f', OLIVA: '#6b7a3a', MILITAR: '#556b2f', 'VERDE MILITAR': '#556b2f', PETROLEO: '#1f5a66', TURQUESA: '#2bb5b0', AGUA: '#7fd1c7',
  AMARILLO: '#f2cf4a', MOSTAZA: '#d4a017', NARANJA: '#e67e22', VIOLETA: '#7b4fa0', LILA: '#c3a6dc', MORADO: '#6a3d8f',
  BEIGE: '#d9c7a7', ARENA: '#d8c8a8', CREMA: '#f3ead6', CRUDO: '#efe8d8', NATURAL: '#e9e0cf', HUESO: '#ede4d3', NUDE: '#e3bc9a',
  CAMEL: '#c19a6b', KHAKI: '#b5a57a', CAQUI: '#b5a57a', MARRON: '#6b4528', CHOCOLATE: '#4e2f1c', SUELA: '#a0632f', TOSTADO: '#a0703c',
  DORADO: '#c9a44c', PLATEADO: '#c0c0c0', COBRE: '#b87333',
};

// The hex of the catalog wins; common color names get a swatch even when the commerce did not set one.
export function swatchColor(name: string, hex: string | null): string | null {
  if (hex) return hex;
  const key = plain(name);
  return colorNames[key] ?? colorNames[key.split(/\s+/)[0]] ?? null;
}
