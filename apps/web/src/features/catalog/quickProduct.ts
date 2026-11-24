// Pure helpers for the one-screen product creation. They are kept free of
// React so the rules (combinations, prices, quantities) can be tested alone.

export type AttributeRole = 'size' | 'color' | 'option';
export type DraftAttribute = { id: string; name: string; role: AttributeRole; suggestions: string[]; selected: string[] };
export type Template = { id: string; label: string; hint: string; attributes: Omit<DraftAttribute, 'id'>[] };

export const QUICK_VARIANT_LIMIT = 50;
export const QUICK_CONFIRM_THRESHOLD = 24;
export const QUICK_QUANTITY_LIMIT = 100000;
export const DEFAULT_COLORS = ['Negro', 'Blanco', 'Gris', 'Azul', 'Rojo', 'Verde', 'Beige', 'Marrón'];

const sizes = (name: string, values: string[], selected: string[] = []) =>
  ({ name, role: 'size' as const, suggestions: values, selected });
const option = (name: string, values: string[]) => ({ name, role: 'option' as const, suggestions: values, selected: [] });
const color = () => ({ name: 'Color', role: 'color' as const, suggestions: [], selected: [] });
const range = (from: number, to: number, step = 1, prefix = '') =>
  Array.from({ length: Math.floor((to - from) / step) + 1 }, (_, i) => prefix + String(from + i * step));

// Suggested starting points. They are examples, not limits: every list
// accepts custom values and no equivalence between brands is implied.
export const TEMPLATES: Template[] = [
  { id: 'ropa', label: 'Ropa', hint: 'Remeras, camisas, camperas', attributes: [sizes('Talle', ['XXS', 'XS', 'S', 'M', 'L', 'XL', 'XXL', '3XL', '4XL']), color()] },
  { id: 'pantalones', label: 'Pantalones', hint: 'Talles 34 a 54', attributes: [sizes('Talle', range(34, 54, 2)), color()] },
  { id: 'jeans', label: 'Jeans', hint: 'Cintura, largo y color', attributes: [sizes('Cintura', range(28, 40, 2, 'W')), option('Largo', ['L30', 'L32', 'L34']), color()] },
  { id: 'calzado', label: 'Calzado', hint: 'Números 35 a 46', attributes: [sizes('Número', range(35, 46)), color()] },
  { id: 'infantil', label: 'Infantil', hint: 'Talles 2 a 16', attributes: [sizes('Talle', range(2, 16, 2)), color()] },
  { id: 'cinturones', label: 'Cinturones', hint: 'Largo y color', attributes: [sizes('Largo', range(85, 110, 5)), color()] },
  { id: 'perfumeria', label: 'Perfumería', hint: 'Presentación', attributes: [option('Presentación', ['30 ml', '50 ml', '100 ml'])] },
  { id: 'unico', label: 'Talle único', hint: 'Un talle, varios colores', attributes: [sizes('Talle', ['Único'], ['Único']), color()] },
  { id: 'personalizado', label: 'Personalizado', hint: 'Definí tus atributos', attributes: [] },
];

let sequence = 0;
export function attributeId() {
  sequence += 1;
  return 'a' + sequence;
}

export function fromTemplate(template: Template, colors: string[]): DraftAttribute[] {
  return template.attributes.map(attribute => ({
    ...attribute, id: attributeId(), selected: [...attribute.selected],
    suggestions: attribute.role === 'color' ? colors : [...attribute.suggestions],
  }));
}

export function sameText(a: string, b: string) {
  return a.trim().toLocaleLowerCase('es') === b.trim().toLocaleLowerCase('es');
}

/** Values in commercial order: suggestions first, then custom values as added. */
export function orderedValues(attribute: DraftAttribute) {
  const known = attribute.suggestions.filter(value => attribute.selected.some(item => sameText(item, value)));
  const custom = attribute.selected.filter(item => !attribute.suggestions.some(value => sameText(item, value)));
  return [...known, ...custom];
}

export type Combination = { key: string; values: string[]; label: string };

export function combinationKey(values: string[]) {
  return JSON.stringify(values.map(value => value.trim().toLocaleLowerCase('es')));
}

/** Number of variants the current selection would create, before building them. */
export function combinationCount(attributes: DraftAttribute[]) {
  if (!attributes.length) return 0;
  return attributes.reduce((total, attribute) => total * orderedValues(attribute).length, 1);
}

/**
 * Cartesian product of the selected values. Rows are grouped by color (as
 * stores usually count merchandise), while values keep attribute order.
 */
export function combinations(attributes: DraftAttribute[]): Combination[] {
  if (!attributes.length || attributes.some(attribute => !orderedValues(attribute).length)) return [];
  const colorIndex = attributes.findIndex(attribute => attribute.role === 'color');
  const order = colorIndex < 0 ? attributes.map((_, i) => i) : [colorIndex, ...attributes.map((_, i) => i).filter(i => i !== colorIndex)];
  let partial: string[][] = [[]];
  for (const index of order) {
    const values = orderedValues(attributes[index]);
    partial = partial.flatMap(prefix => values.map(value => [...prefix, value]));
  }
  return partial.map(picked => {
    const values = attributes.map((_, i) => picked[order.indexOf(i)]);
    return { key: combinationKey(values), values, label: values.join(' · ') };
  });
}

/**
 * Interprets an amount typed by a person in Argentina. "24.500" and
 * "24.500,50" use dots as thousands separators; "24500,5" and "24500.5" are
 * decimals. Returns the canonical API amount, null for an empty field, or
 * undefined when the text is not a valid nonnegative amount.
 */
export function parsePrice(raw: string): string | null | undefined {
  let text = raw.trim().replace(/\s/g, '').replace(/^\$/, '');
  if (text === '') return null;
  if (/^\d{1,3}(\.\d{3})+(,\d{1,2})?$/.test(text)) text = text.replace(/\./g, '').replace(',', '.');
  else text = text.replace(',', '.');
  if (!/^(0|[1-9]\d{0,15})(\.\d{1,2})?$/.test(text)) return undefined;
  const [whole, decimals = ''] = text.split('.');
  return whole + '.' + decimals.padEnd(2, '0');
}

/** Whole units between 0 and the backend limit; an empty field means zero. */
export function parseQuantity(raw: string): number | undefined {
  const text = raw.trim();
  if (text === '') return 0;
  if (!/^\d{1,6}$/.test(text)) return undefined;
  const value = Number(text);
  return value <= QUICK_QUANTITY_LIMIT ? value : undefined;
}

/** "24500.00" → "24.500" and "12.50" → "12,50", for display. */
export function formatAmount(canonical: string) {
  const [whole, decimals = '00'] = canonical.split('.');
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  return decimals === '00' ? grouped : grouped + ',' + decimals;
}

export const barcodePattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$/;

export type LabelledVariant = { size?: string; color?: string; options?: { value: string }[] };

/** Attributes of a saved variant, including additional ones such as a jean length. */
export function variantLabel(variant: LabelledVariant) {
  return [variant.size, ...(variant.options ?? []).map(option => option.value), variant.color].filter(Boolean).join(' · ');
}

/** Size plus additional attributes (everything except color), as used by the POS cart. */
export function primaryLabel(variant: LabelledVariant) {
  return [variant.size, ...(variant.options ?? []).map(option => option.value)].filter(Boolean).join(' · ');
}
