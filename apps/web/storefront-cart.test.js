import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const file = await readFile(new URL('./src/storefront/cart.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(file, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const cart = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));

const line = (variantId, price, quantity = 1, extra = {}) => ({
  variantId, productId: 'p-' + variantId, name: 'Remera Lino Hombre', size: 'M', color: 'Blanco', price, imageId: 'img', quantity, ...extra,
});

test('suma en centavos sin errores de punto flotante', () => {
  const lines = [line('a', '0.10', 3), line('b', '0.20'), line('c', '25000.00', 2)];
  assert.equal(cart.total(lines), 5000050n);
  assert.equal(cart.money(cart.total(lines)), '$ 50.000,50');
  assert.equal(cart.money(2500000n), '$ 25.000');
  assert.equal(cart.money(1234567n, 'USD'), 'USD 12.345,67');
  assert.throws(() => cart.cents('1e3'));
});

test('agregar la misma variante suma cantidades con tope', () => {
  let lines = cart.addLine([], line('a', '100', 2));
  lines = cart.addLine(lines, line('a', '120', 3));
  assert.equal(lines.length, 1);
  assert.equal(lines[0].quantity, 5);
  assert.equal(lines[0].price, '120', 'toma el precio más reciente');
  lines = cart.addLine(lines, line('a', '120', 50));
  assert.equal(lines[0].quantity, cart.maxQuantity);
  lines = cart.addLine(lines, line('b', '50'));
  assert.equal(cart.units(lines), cart.maxQuantity + 1);
  assert.deepEqual(cart.setQuantity(lines, 'a', 0).map((item) => item.variantId), ['b']);
  assert.equal(cart.setQuantity(lines, 'b', 4).find((item) => item.variantId === 'b').quantity, 4);
});

test('descarta carritos guardados inválidos', () => {
  assert.deepEqual(cart.parseCart('no es json'), []);
  assert.deepEqual(cart.parseCart('{"a":1}'), []);
  const stored = JSON.stringify([line('a', '100'), { ...line('b', '1,5') }, { ...line('c', '10'), quantity: 0 }, { variantId: 'd' }]);
  assert.deepEqual(cart.parseCart(stored).map((item) => item.variantId), ['a']);
});

test('arma el mensaje de WhatsApp con talle, color, cantidades y total', () => {
  const text = cart.orderMessage({
    store: 'La E', currency: 'ARS', customer: '  Juan  ', note: '', link: 'https://retail.example/tienda/lae',
    lines: [line('a', '25000.00', 2), line('b', '18000.00', 1, { name: 'Gorra Trucker', size: 'U', color: 'Negro' })],
  });
  assert.equal(text, [
    '¡Hola La E! Quiero hacer este pedido:',
    '',
    '1. Remera Lino Hombre — Talle M · Blanco',
    '   2 × $ 25.000 = $ 50.000',
    '2. Gorra Trucker — Talle U · Negro',
    '   1 × $ 18.000 = $ 18.000',
    '',
    '*Total: $ 68.000*',
    'Nombre: Juan',
    '',
    'Pedido armado en https://retail.example/tienda/lae',
  ].join('\n'));
  const url = cart.whatsappURL('+54 9 11 5555-6666', text);
  assert.ok(url.startsWith('https://wa.me/5491155556666?text='));
  assert.equal(decodeURIComponent(url.split('?text=')[1]), text);
});

test('una variante sin talle ni color no agrega detalle', () => {
  assert.equal(cart.variantLabel({ size: '', color: '' }), '');
  assert.equal(cart.variantLabel({ size: '40', color: '' }), 'Talle 40');
});
