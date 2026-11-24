const catalogPath = '#/productos';
export const catalogPageSize = 25;
export const catalogLastPage = 4001;

const fields = ['q', 'categoryId', 'brandId', 'page'] as const;
type Field = typeof fields[number];
const defaults = { q: '', categoryId: '', brandId: '', page: 1 };
const labels = { q: 'Nombre o SKU', categoryId: 'Categoría', brandId: 'Marca', page: 'Página', encoding: 'URL' };

export function isCatalogHash(hash: string) {
  return hash === catalogPath || hash.startsWith(catalogPath + '?');
}

// Matches strings.TrimSpace in Go, including NEXT LINE and excluding BOM.
function trimSearch(value: string) {
  return value.replace(/^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/gu, '');
}

function decode(value: string) {
  const decoded = decodeURIComponent(value.replace(/\+/g, ' '));
  // Also rejects isolated UTF-16 surrogates in direct inputs.
  encodeURIComponent(decoded);
  return decoded;
}

function segments(hash: string) {
  const query = hash.slice(catalogPath.length + 1);
  return query ? query.split('&').map((raw) => {
    const separator = raw.indexOf('=');
    const key = separator < 0 ? raw : raw.slice(0, separator);
    const value = separator < 0 ? '' : raw.slice(separator + 1);
    try {
      const name = decode(key);
      const field = fields.find((candidate) => candidate === name);
      return { raw, field, value };
    } catch {
      return { raw, field: 'encoding' as const, value };
    }
  }) : [];
}

function validated(field: Field, value: string) {
  const decoded = decode(value);
  if (field === 'q') {
    const query = trimSearch(decoded);
    if (query.includes('\0') || [...query].length > 160) throw new Error('Usá hasta 160 caracteres y evitá el carácter NUL.');
    return query;
  }
  if (field === 'page') {
    if (!/^[0-9]+$/.test(decoded) || Number(decoded) < 1 || Number(decoded) > catalogLastPage) {
      throw new Error('Ingresá una página entre 1 y 4001.');
    }
    return Number(decoded);
  }
  if (decoded && !/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(decoded)) {
    throw new Error('El identificador debe ser un UUID canónico.');
  }
  return decoded;
}

export function readCatalogQuery(hash: string) {
  const parsed = segments(hash);
  const values = { ...defaults };
  const errors: { field: Field | 'encoding'; label: string; message: string }[] = [];
  if (parsed.some(({ field }) => field === 'encoding')) {
    errors.push({ field: 'encoding', label: labels.encoding, message: 'Hay nombres de parámetros con codificación inválida.' });
  }
  for (const field of fields) {
    const entries = parsed.filter((segment) => segment.field === field);
    if (!entries.length) continue;
    try {
      const [first, ...rest] = entries.map(({ value }) => validated(field, value));
      if (rest.some((value) => value !== first)) throw new Error('El parámetro aparece con valores diferentes.');
      if (field === 'page') values.page = Number(first);
      else values[field] = String(first);
    } catch (cause) {
      errors.push({ field, label: labels[field], message: cause instanceof URIError
        ? 'La codificación porcentual o UTF-8 es inválida.' : (cause as Error).message });
    }
  }
  return { values, errors, offset: (values.page - 1) * catalogPageSize };
}

// Only replace named fields. Unknown segments and unrelated invalid fields stay verbatim.
export function updateCatalogHash(hash: string, changes: Partial<typeof defaults> & { encoding?: '' }) {
  const kept = segments(hash).filter(({ field }) => !field || !Object.hasOwn(changes, field)).map(({ raw }) => raw);
  for (const field of fields) {
    const value = changes[field];
    if (value !== undefined && value !== defaults[field]) kept.push(field + '=' + encodeURIComponent(String(value)));
  }
  return catalogPath + (kept.length ? '?' + kept.join('&') : '');
}
