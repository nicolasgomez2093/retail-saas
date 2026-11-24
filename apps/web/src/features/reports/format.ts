import type { SaleMethodFilter } from '../../shared/api/client';
import { posPrice } from '../pos/priceDisplay';
import { paymentLabels } from '../pos/SalePayments';

export function money(currency: string, value: string | null | undefined) {
  return value === null || value === undefined ? 'No disponible' : currency + ' ' + posPrice(value);
}

/** Amount with an explicit minus for refunds; posPrice formats the absolute value. */
export function signedMoney(currency: string, value: string) {
  return value.startsWith('-') ? '−' + money(currency, value.slice(1)) : money(currency, value);
}

export function percent(value: string | null) {
  return value === null ? 'No disponible' : Number(value).toLocaleString('es-AR', { maximumFractionDigits: 1 }) + ' %';
}

export function shortDate(day: string) {
  return day.split('-').reverse().join('/');
}

export function units(count: number) {
  return count.toLocaleString('es-AR') + (count === 1 ? ' unidad' : ' unidades');
}

export function plural(count: number, one: string, many: string) {
  return count.toLocaleString('es-AR') + ' ' + (count === 1 ? one : many);
}

/** Chart magnitude from a decimal string; precision beyond cents is irrelevant for bar heights. */
export function amount(value: string | null | undefined) {
  const parsed = Number(value ?? 0);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function methodLabel(method: SaleMethodFilter) {
  return method === 'on_credit' ? 'Fiado' : method ? paymentLabels[method] : 'Todos los medios';
}
