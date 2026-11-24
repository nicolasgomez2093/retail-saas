import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const file = await readFile(new URL('./src/features/catalog/quickProduct.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(file, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const quick = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));
const attr = (name, role, selected, suggestions = []) => ({ id: name, name, role, selected, suggestions });

test('genera la matriz completa agrupada por color', () => {
  const rows = quick.combinations([attr('Talle', 'size', ['S', 'M', 'L'], ['S', 'M', 'L']), attr('Color', 'color', ['Negro', 'Blanco'])]);
  assert.equal(rows.length, 6);
  assert.deepEqual(rows.map(row => row.label), ['S · Negro', 'M · Negro', 'L · Negro', 'S · Blanco', 'M · Blanco', 'L · Blanco']);
  assert.equal(new Set(rows.map(row => row.key)).size, 6);
});

test('combina tres atributos sin depender de talles fijos', () => {
  const jean = [attr('Cintura', 'size', ['W30', 'W32']), attr('Largo', 'option', ['L30', 'L32']), attr('Color', 'color', ['Azul'])];
  assert.equal(quick.combinationCount(jean), 4);
  assert.deepEqual(quick.combinations(jean).map(row => row.values), [
    ['W30', 'L30', 'Azul'], ['W30', 'L32', 'Azul'], ['W32', 'L30', 'Azul'], ['W32', 'L32', 'Azul'],
  ]);
  const shoes = [attr('Número', 'size', ['38', '39', '40', '41', '42']), attr('Color', 'color', ['Negro', 'Blanco'])];
  assert.equal(quick.combinations(shoes).length, 10);
});

test('un atributo sin valores no genera combinaciones parciales', () => {
  assert.deepEqual(quick.combinations([attr('Talle', 'size', ['S']), attr('Color', 'color', [])]), []);
  assert.equal(quick.combinationCount([]), 0);
});

test('respeta el orden comercial y conserva los valores propios', () => {
  const size = attr('Talle', 'size', ['XL', 'S', 'Especial'], ['S', 'M', 'L', 'XL']);
  assert.deepEqual(quick.orderedValues(size), ['S', 'XL', 'Especial']);
});

test('interpreta importes escritos en Argentina sin confundir cero con vacío', () => {
  assert.equal(quick.parsePrice('24.500'), '24500.00');
  assert.equal(quick.parsePrice('$ 24.500,50'), '24500.50');
  assert.equal(quick.parsePrice('24500,5'), '24500.50');
  assert.equal(quick.parsePrice('12.5'), '12.50');
  assert.equal(quick.parsePrice('0'), '0.00');
  assert.equal(quick.parsePrice(''), null);
  assert.equal(quick.parsePrice('-10'), undefined);
  assert.equal(quick.parsePrice('abc'), undefined);
  assert.equal(quick.formatAmount('24500.00'), '24.500');
  assert.equal(quick.formatAmount('1234567.05'), '1.234.567,05');
});

test('acepta cero unidades y rechaza cantidades negativas o fraccionarias', () => {
  assert.equal(quick.parseQuantity(''), 0);
  assert.equal(quick.parseQuantity('0'), 0);
  assert.equal(quick.parseQuantity('10'), 10);
  assert.equal(quick.parseQuantity('-1'), undefined);
  assert.equal(quick.parseQuantity('1.5'), undefined);
  assert.equal(quick.parseQuantity('100001'), undefined);
});

test('describe la variante con todos sus atributos', () => {
  assert.equal(quick.variantLabel({ size: 'W32', color: 'Azul', options: [{ name: 'Largo', value: 'L32' }] }), 'W32 · L32 · Azul');
  assert.equal(quick.variantLabel({ size: '', color: '', options: [] }), '');
});
