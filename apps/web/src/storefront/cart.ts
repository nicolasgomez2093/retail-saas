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
