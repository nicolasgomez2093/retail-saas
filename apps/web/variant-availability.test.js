import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const file = await readFile(new URL('./src/features/catalog/variantAvailability.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(file, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { sellableAvailability } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));

test('suma solo ubicaciones vendibles vigentes y separa variantes', () => {
  const locations = [{ id: 'a', isSellable: true }, { id: 'b', isSellable: true }, { id: 'c', isSellable: false }];
  const rows = [
    { locationId: 'a', variantId: 'v1', available: 4, active: true },
    { locationId: 'b', variantId: 'v1', available: -1, active: true },
    { locationId: 'c', variantId: 'v1', available: 50, active: true },
    { locationId: 'otro', variantId: 'v1', available: 50, active: true },
    { locationId: 'a', variantId: 'v2', available: 0, active: true },
    { locationId: 'a', variantId: 'v3', available: 15, active: false },
  ];
  assert.deepEqual([...sellableAvailability(locations, rows)], [['v1', 3], ['v2', 0]]);
});

test('sin filas autorizadas no inventa un saldo de cero', () => {
  assert.equal(sellableAvailability([], []).get('v1'), undefined);
});
