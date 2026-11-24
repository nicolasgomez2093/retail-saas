export const movementPath = '#/inventario/movimientos';
export const movementKinds = {
  adjustment: 'Ajuste',
  count_adjustment: 'Ajuste por conteo',
  transfer_dispatch: 'Despacho de transferencia',
  transfer_receipt: 'Recepción de transferencia',
  purchase_receipt: 'Recepción de compra',
  sale: 'Venta',
  sale_return: 'Devolución de venta',
  sale_exchange: 'Salida por cambio',
} as const;

export const movementDefaults = {
  q: '', tipo: '', desde: '', hasta: '', ubicacion: '', variante: '', responsable: '', referencia: '',
  siguiente: '', anterior: '', detalle: '',
};
export type MovementQuery = typeof movementDefaults;
type Field = keyof MovementQuery;
const fields = Object.keys(movementDefaults) as Field[];
const labels: Record<Field, string> = {
  q: 'Producto o SKU', tipo: 'Tipo', desde: 'Desde', hasta: 'Hasta', ubicacion: 'Ubicación',
  variante: 'Variante', responsable: 'Responsable', referencia: 'Referencia', siguiente: 'Página anterior en el tiempo',
  anterior: 'Página posterior en el tiempo', detalle: 'Detalle',
};
const uuid = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
const trimSpace = /^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/gu;
export function isMovementHash(hash: string) {
  const path = hash.split('?', 1)[0];
  return path === movementPath || path === '#/stock/movimientos';
}

function fieldValue(field: Field, value: string) {
  if (value === '') return value;
  if (field === 'q' || field === 'responsable') {
    const text = value.replace(trimSpace, '');
    if (text.includes('\0') || [...text].length > 160) throw new Error('Ingresá hasta 160 caracteres, sin NUL.');
    return text;
  }
  if (field === 'tipo') {
    if (Object.hasOwn(movementKinds, value)) return value;
  } else if (field === 'desde' || field === 'hasta') {
    if (/^\d{4}-\d{2}-\d{2}$/.test(value) && !value.startsWith('0000')) {
      const date = new Date(value + 'T00:00:00Z');
      if (!Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value) return value;
    }
    throw new Error('Ingresá una fecha válida.');
  } else if (uuid.test(value)) return value;
  throw new Error('El valor no es válido.');
}

export function readMovementHash(hash: string) {
  const values = { ...movementDefaults };
  const errors: string[] = [];
  const mark = hash.indexOf('?');
  if (mark < 0) return { values, errors };
  const seen = new Set<string>();
  const decode = (part: string) => decodeURIComponent(part.replace(/\+/g, ' '));
  for (const raw of hash.slice(mark + 1).split('&').filter(Boolean)) {
    const index = raw.indexOf('=');
    let key: string;
    try {
      key = decode(index < 0 ? raw : raw.slice(0, index));
    } catch {
      errors.push('La dirección contiene codificación inválida.');
      continue;
    }
    const field = fields.find(item => item === key);
    if (!field) {
      errors.push('La dirección contiene un parámetro no admitido.');
      continue;
    }
    if (seen.has(field)) {
      errors.push(labels[field] + '. El filtro aparece más de una vez.');
      continue;
    }
    seen.add(field);
    try {
      values[field] = fieldValue(field, decode(index < 0 ? '' : raw.slice(index + 1)));
    } catch (cause) {
      errors.push(labels[field] + '. ' + (cause instanceof URIError ? 'Codificación inválida.' : (cause as Error).message));
    }
  }
  if (values.desde && values.hasta && values.desde > values.hasta) errors.push('Desde debe ser anterior o igual a Hasta.');
  if (values.siguiente && values.anterior) errors.push('La dirección combina dos límites de paginación.');
  return { values, errors: [...new Set(errors)] };
}

export function movementSearch(values: MovementQuery, includeDetail = false) {
  const result = new URLSearchParams();
  for (const field of fields) {
    if (values[field] && (includeDetail || field !== 'detalle')) result.set(field, values[field]);
  }
  return result.toString();
}

export function movementHref(values: MovementQuery) {
  const query = movementSearch(values, true);
  return movementPath + (query ? '?' + query : '');
}
