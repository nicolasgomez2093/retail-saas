import type { PurchaseOrder, PurchaseOrderStatus, RejectionReason } from '../../shared/api/client';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { formatAmount, parsePrice, primaryLabel, variantLabel } from '../catalog/quickProduct';

export const orderCode = (number: number) => 'OC-' + String(number).padStart(4, '0');

export const statusLabels: Record<PurchaseOrderStatus, string> = {
  OPEN: 'Pendiente', PARTIALLY_RECEIVED: 'Recepción parcial', RECEIVED: 'Recibida', CANCELLED: 'Cancelada',
};
export const statusTone: Record<PurchaseOrderStatus, string> = {
  OPEN: 'warning', PARTIALLY_RECEIVED: 'warning', RECEIVED: '', CANCELLED: 'muted',
};

export const reasonLabels: Record<RejectionReason, string> = {
  damaged: 'Dañadas', wrong_item: 'Producto equivocado', excess: 'Excedente', other: 'Otro motivo',
};
export const reasonHelp: Record<RejectionReason, string> = {
  damaged: 'Llegaron con fallas o roturas.',
  wrong_item: 'Vino otro producto en lugar de este.',
  excess: 'Llegaron más unidades que las pendientes.',
  other: 'Cualquier otra unidad que no se acepta.',
};

/** Ordered minus accepted; a cancelled order has nothing left to receive. */
export function pendingUnits(order: Pick<PurchaseOrder, 'status' | 'items'>) {
  if (order.status === 'CANCELLED') return 0;
  return order.items.reduce((total, item) => total + item.quantity - item.received, 0);
}
export const orderedUnits = (order: Pick<PurchaseOrder, 'items'>) => order.items.reduce((total, item) => total + item.quantity, 0);
export const receivedUnits = (order: Pick<PurchaseOrder, 'items'>) => order.items.reduce((total, item) => total + item.received, 0);

/** Exact money: canonical "1234.50" strings are multiplied as integer cents. */
export function lineCents(quantity: number, unitCost: string | null) {
  if (unitCost === null || !Number.isInteger(quantity) || quantity < 0) return null;
  return minorUnits(unitCost) * BigInt(quantity);
}
export function sumCents(values: (bigint | null)[]) {
  return values.reduce<bigint>((total, value) => total + (value ?? 0n), 0n);
}
export function money(currency: string, cents: bigint) {
  return currency + ' ' + formatAmount(decimalText(cents));
}
export function moneyText(currency: string, canonical: string | null) {
  return canonical === null ? 'Sin costo' : currency + ' ' + formatAmount(canonical);
}
/** Accepts "1.500", "1500,50" or "1500.5"; null when empty, undefined when invalid. */
export const parseCost = parsePrice;

export function itemLabel(item: { size?: string; color?: string; options?: { value: string }[] }) {
  return variantLabel(item) || 'Talle único';
}
export { primaryLabel };

export function newKey() {
  return crypto.randomUUID();
}
