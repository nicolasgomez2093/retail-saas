import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./src/shared/lib/httpErrorMessage.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { httpErrorMessage } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));

test('fallos de infraestructura y permisos no exponen detalles internos', () => {
  for (const status of [500, 502, 503]) assert.equal(httpErrorMessage(status, 'SQLSTATE 08006: internal tenant data'), 'El servicio no está disponible en este momento.');
  assert.equal(httpErrorMessage(403, 'permission denied for relation'), 'No tenés permiso para realizar esta operación.');
  assert.equal(httpErrorMessage(401, 'invalid session cookie'), 'La sesión venció. Volvé a iniciar sesión.');
});

test('validaciones y conflictos comerciales conservan su explicación', () => {
  assert.equal(httpErrorMessage(409, 'El saldo cambió. Revisá el ajuste.'), 'El saldo cambió. Revisá el ajuste.');
  assert.equal(httpErrorMessage(400, 'Ingresá el motivo.'), 'Ingresá el motivo.');
  assert.equal(httpErrorMessage(404), 'No pudimos completar la solicitud.');
});
