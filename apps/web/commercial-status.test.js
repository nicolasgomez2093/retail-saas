import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./src/features/catalog/commercialStatus.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { commercialStatus } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));

test('preparación comercial requiere precio confirmado, stock positivo y actividad', () => {
  assert.equal(commercialStatus({ active: true, price: '0.00', stock: 2 }).label, 'Lista para vender');
  assert.equal(commercialStatus({ active: true, price: null, stock: 2 }).label, 'Sin precio');
  assert.equal(commercialStatus({ active: true, price: '15.00', stock: 0 }).label, 'Sin stock');
  assert.equal(commercialStatus({ active: true, price: '15.00', stock: -1 }).ready, false);
  assert.equal(commercialStatus({ active: false, price: '15.00', stock: 2 }).label, 'Inactiva');
});

test('lecturas desconocidas o fallidas no se convierten en disponibilidad ni cero', () => {
  for (const input of [
    { active: true, price: undefined, stock: 3 },
    { active: true, price: '0.00', stock: undefined },
    { active: true, price: '15.00', stock: 3, error: true },
  ]) {
    assert.equal(commercialStatus(input).label, 'No verificada');
    assert.equal(commercialStatus(input).ready, false);
  }
  assert.equal(commercialStatus({ active: true, price: '0.00', stock: null }).label, 'Sin saldo registrado');
});
