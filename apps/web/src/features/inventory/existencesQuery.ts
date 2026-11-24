export const existencesPath = '#/inventario/existencias';
export const lastExistencesPage = 4001;
export const existenceDefaults = {
  q: '', categoryId: '', brandId: '', producto: '', variante: '', saldo: '', ubicacion: '', bajo: '', page: '1', detalle: '',
};
export type ExistenceQuery = typeof existenceDefaults;
type Field = keyof ExistenceQuery;
const keys = Object.keys(existenceDefaults) as Field[];
const labels: Record<Field, string> = {
  q: 'Producto o SKU', categoryId: 'Categoría', brandId: 'Marca', producto: 'Estado del producto',
  variante: 'Estado de la variante', saldo: 'Saldo', ubicacion: 'Ubicación', bajo: 'Stock bajo', page: 'Página', detalle: 'Detalle',
};
const uuid = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
const trimSpace = /^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/gu;
export function isExistencesHash(hash: string) {
  const path = hash.split('?', 1)[0];
  return path === existencesPath || path === '#/stock';
}

function validate(field: Field, value: string) {
  if (field === 'q') {
    const trimmed = value.replace(trimSpace, '');
    if (trimmed.includes('\0') || [...trimmed].length > 160) throw new Error('Ingresá hasta 160 caracteres sin NUL.');
    encodeURIComponent(trimmed);
    return trimmed;
  }
  if (field === 'page') {
    const page = Number(value);
    if (!/^[0-9]+$/.test(value) || page < 1 || page > lastExistencesPage) throw new Error('Ingresá una página entre 1 y 4001.');
    return String(page);
  }
  if (field === 'bajo') {
    if (value === '' || value === '0') return '';
    if (value === '1') return value;
  } else if (field === 'producto' || field === 'variante') {
    if (['', 'active', 'inactive'].includes(value)) return value;
  } else if (field === 'saldo') {
    if (['', 'positive', 'zero', 'unrecorded'].includes(value)) return value;
  } else if (!value || uuid.test(value)) return value;
  throw new Error('El valor del filtro es inválido.');
}

export function readExistencesHash(hash: string) {
  const values = { ...existenceDefaults };
  const errors: string[] = [];
  const seen = new Set<Field>();
  const search = hash.slice(hash.indexOf('?') + 1);
  if (!hash.includes('?')) return { values, errors };
  for (const raw of search.split('&').filter(Boolean)) {
    const i = raw.indexOf('=');
    try {
      const name = decodeURIComponent((i === -1 ? raw : raw.slice(0, i)).replace(/\+/g, ' '));
      const field = keys.find(key => key === name);
      if (!field) {
        errors.push('Hay un parámetro no admitido en la dirección.');
        continue;
      }
      try {
        const value = validate(field, decodeURIComponent((i === -1 ? '' : raw.slice(i + 1)).replace(/\+/g, ' ')));
        if (seen.has(field) && values[field] !== value) throw new Error('El filtro tiene valores diferentes.');
        values[field] = value;
        seen.add(field);
      } catch (cause) {
        errors.push(labels[field] + '. ' + (cause instanceof URIError ? 'Codificación inválida.' : (cause as Error).message));
      }
    } catch {
      errors.push('La dirección contiene codificación inválida.');
    }
  }
  return { values, errors: [...new Set(errors)] };
}

export function existenceSearch(values: ExistenceQuery, detail = true) {
  const result = new URLSearchParams();
  for (const field of keys) {
    if ((field !== 'detalle' || detail) && values[field] !== existenceDefaults[field]) result.set(field, values[field]);
  }
  return result.toString();
}

export function existencesHref(values: ExistenceQuery) {
  const parameters = existenceSearch(values);
  return existencesPath + (parameters ? '?' + parameters : '');
}
