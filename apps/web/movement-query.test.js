import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./src/features/inventory/movementQuery.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
const { readMovementHash, movementHref, movementSearch, isMovementHash } =
  await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));
const id = '00000000-0000-4000-8000-000000000001';

test('Movimientos valida escapes, parámetros duplicados y sucursal fuera de URL', () => {
  for (const invalid of ['q=%', 'q=%FF', 'q=%00', 'responsable=%ED%A0%80', 'q=x&q=x', 'q=x&%71=y',
    'store_id=' + id, 'tipo=ajuste', 'ubicacion=no', 'referencia=x', 'responsable=' + 'ó'.repeat(161)]) {
    assert.ok(readMovementHash('#/stock/movimientos?' + invalid).errors.length, invalid);
  }
  assert.equal(readMovementHash('#/stock/movimientos?q=%20Café&tipo=adjustment').values.q, 'Café');
  assert.ok(isMovementHash('#/stock/movimientos?ubicacion=' + id));
  assert.ok(!isMovementHash('#/stock?ubicacion=' + id));
});

test('Movimientos valida calendario local y evita cursores contradictorios', () => {
  for (const invalid of ['desde=2026-02-29', 'desde=0000-01-01', 'hasta=2026-06-31',
    'desde=2026-06-01&hasta=2026-05-31', 'anterior=' + id + '&siguiente=' + id]) {
    assert.ok(readMovementHash('#/inventario/movimientos?' + invalid).errors.length, invalid);
  }
  assert.equal(readMovementHash('#/inventario/movimientos?desde=2024-02-29&hasta=2024-03-01').errors.length, 0);
});

test('detalle, cursores y filtros conservan el contexto al volver', () => {
  const { values, errors } = readMovementHash('#/stock/movimientos?ubicacion=' + id + '&responsable=Ana+Sol&q=%C3%B1&variante=' + id + '&siguiente=' + id);
  assert.deepEqual(errors, []);
  const detail = movementHref({ ...values, detalle: id });
  assert.deepEqual(readMovementHash(detail).values, { ...values, detalle: id });
  assert.equal(movementSearch(readMovementHash(detail).values), movementSearch(values));
  assert.equal(movementHref({ ...readMovementHash(detail).values, detalle: '' }), movementHref(values));
});
