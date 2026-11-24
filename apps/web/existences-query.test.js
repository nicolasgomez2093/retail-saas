import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./src/features/inventory/existencesQuery.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { readExistencesHash: read, existencesHref, existenceSearch, isExistencesHash } =
  await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));
const id = 'abcdef12-1234-1234-1234-123456789abc';

test('Existencias valida escapes, UTF-8, Unicode, enumeraciones, UUID y límites', () => {
  for (const query of ['q=%', 'q=%FF', 'q=%00', 'q=%ED%A0%80', 'q=' + 'é'.repeat(161),
    'page=4002', 'page=0', 'page=1.5', 'ubicacion=missing', 'brandId=' + id.toUpperCase(), 'saldo=unknown', 'bajo=2', 'producto=unknown']) {
    assert.ok(read('#/inventario/existencias?' + query).errors.length, query);
  }
  assert.equal(read('#/inventario/existencias?q=' + 'é'.repeat(160)).errors.length, 0);
  assert.equal(read('#/stock?page=4001').values.page, '4001');
});

test('Existencias rechaza filtros contradictorios e intentos de elegir sucursal en URL', () => {
  assert.equal(read('#/stock?q=Ana+Sol&q=Ana%20Sol&page=01&page=1').errors.length, 0);
  for (const query of ['q=A&q=a', 'q=café&q=cafe%CC%81', 'page=1&page=2', 'store_id=' + id]) {
    assert.ok(read('#/stock?' + query).errors.length, query);
  }
});

test('Existencias conserva los enlaces de Inicio, filtros y la navegación de detalle', () => {
  const { values, errors } = read('#/stock?bajo=1&ubicacion=' + id + '&q=SKU-001&page=2');
  assert.deepEqual(errors, []);
  assert.ok(isExistencesHash('#/stock?bajo=1'));
  assert.ok(!isExistencesHash('#/stock/ajustes'));
  const detail = existencesHref({ ...values, detalle: id });
  assert.deepEqual(read(detail).values, { ...values, detalle: id });
  assert.equal(existenceSearch(read(detail).values, false), existenceSearch(values, false));
  assert.equal(existencesHref(read(detail).values), detail);
});
