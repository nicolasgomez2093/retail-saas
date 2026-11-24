import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const file = await readFile(new URL('./src/features/catalog/catalogQuery.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(file, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { readCatalogQuery: read, updateCatalogHash: update, isCatalogHash } =
  await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));
const uuid = 'abcdef12-1234-1234-1234-123456789abc';

test('U01 · codificación, Unicode y filtros canónicos', () => {
  for (const q of ['%', '%FF', '%ED%A0%80', '%00', encodeURIComponent('😀'.repeat(161))]) {
    assert.equal(read('#/productos?q=' + q).errors[0].field, 'q');
  }
  assert.deepEqual(read('#/productos?q=' + encodeURIComponent('😀'.repeat(160))).errors, []);
  assert.equal(read('#/productos?%71=Ana+Sol').values.q, 'Ana Sol');
  assert.equal(read('#/productos?q=%C2%85X%C2%85').values.q, 'X');
  assert.equal(read('#/productos?q=%EF%BB%BFX').values.q, '\uFEFFX');
  assert.equal(read('#/productos?brandId=' + uuid).values.brandId, uuid);
  assert.equal(read('#/productos?brandId=' + uuid.toUpperCase()).errors[0].field, 'brandId');
  assert.equal(read('#/productos?categoryId=no').errors[0].field, 'categoryId');
  assert.equal(read('#/productos?br%FFand=x').errors[0].field, 'encoding');
  assert.equal(isCatalogHash('#/productos/precios?q=x'), false);
});

test('U02 · duplicados y corrección puntual', () => {
  assert.deepEqual(read('#/productos?q=Ana+Sol&%71=Ana%20Sol').errors, []);
  for (const q of ['q=&q=x', 'q=A&q=a', 'q=cafe&q=café', 'q=café&q=cafe%CC%81']) {
    assert.equal(read('#/productos?' + q).errors[0].field, 'q');
  }
  const hash = '#/productos?custom=%FF&categoryId=' + uuid + '&q=%&page=0';
  const fixed = update(hash, { q: '' });
  assert.equal(fixed, '#/productos?custom=%FF&categoryId=' + uuid + '&page=0');
  assert.equal(read(fixed).errors[0].field, 'page');
  assert.deepEqual(read(update(fixed, { page: 1 })).errors, []);
  assert.equal(update('#/productos?%FF=bad&q=x', { encoding: '' }), '#/productos?q=x');
});

test('U03 · límites, offset y round trip', () => {
  for (const page of ['0', '4002', '1.5', '-1', '9007199254740993', '', '+1']) {
    assert.equal(read('#/productos?page=' + page).errors[0].field, 'page');
  }
  assert.equal(read('#/productos?page=1').offset, 0);
  assert.equal(read('#/productos?page=4001').offset, 100000);
  assert.deepEqual(read('#/productos?page=01&page=1').errors, []);
  const hash = update('#/productos?q=café&keep=a?b', { page: 4001 });
  assert.equal(read(hash).values.q, 'café');
  assert.equal(update(hash, { page: 4001 }), hash);
});
