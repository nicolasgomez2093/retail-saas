import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

async function load(path, replace = {}) {
  let source = await readFile(new URL(path, import.meta.url), 'utf8');
  let { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
  for (const [from, to] of Object.entries(replace)) outputText = outputText.replaceAll(`'${from}'`, `'${to}'`);
  return 'data:text/javascript;base64,' + Buffer.from(outputText).toString('base64');
}
const decimal = await load('./src/shared/lib/decimal.ts');
const quick = await load('./src/features/catalog/quickProduct.ts');
const format = await import(await load('./src/features/purchases/purchaseFormat.ts', {
  '../../shared/lib/decimal': decimal, '../catalog/quickProduct': quick,
}));

test('calcula subtotales y totales en centavos exactos', () => {
  const lines = [[10, '9000.00'], [8, '9000.00'], [12, '21500.50'], [5, '40000.00']].map(([q, c]) => format.lineCents(q, c));
  assert.deepEqual(lines, [9000000n, 7200000n, 25800600n, 20000000n]);
  assert.equal(format.money('ARS', format.sumCents(lines)), 'ARS 620.006');
  // 0,1 + 0,2 en punto flotante no da 0,3; en centavos sí.
  assert.equal(format.money('ARS', format.sumCents([format.lineCents(1, '0.10'), format.lineCents(1, '0.20')])), 'ARS 0,30');
  assert.equal(format.lineCents(3, null), null);
  assert.equal(format.sumCents([null, format.lineCents(2, '1.05')]), 210n);
});

test('interpreta costos con formato argentino y rechaza importes inválidos', () => {
  assert.equal(format.parseCost('21.500,50'), '21500.50');
  assert.equal(format.parseCost('9000'), '9000.00');
  assert.equal(format.parseCost(''), null);
  for (const bad of ['-5', '12,345', 'diez', '1.2.3']) assert.equal(format.parseCost(bad), undefined, bad);
});

test('cuenta pendientes solo en pedidos vigentes', () => {
  const items = [{ quantity: 10, received: 10 }, { quantity: 8, received: 5 }, { quantity: 5, received: 3 }];
  assert.equal(format.pendingUnits({ status: 'PARTIALLY_RECEIVED', items }), 5);
  assert.equal(format.pendingUnits({ status: 'CANCELLED', items }), 0);
  assert.equal(format.orderedUnits({ items }), 23);
  assert.equal(format.receivedUnits({ items }), 18);
  assert.equal(format.orderCode(7), 'OC-0007');
  assert.equal(format.orderCode(12345), 'OC-12345');
});
