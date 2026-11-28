import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const file = await readFile(new URL('./src/app/routes.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(file, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { resolveRoute, routeHref } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'));

test('los enlaces anteriores resuelven la misma vista que su equivalente canónico', () => {
  const aliases = {
    ventas: 'ventas/historial',
    'configuracion/precios': 'productos/precios',
    stock: 'inventario/existencias',
    compras: 'compras/ordenes',
    'compras/pendientes': 'compras/ordenes?vista=pendientes',
    proveedores: 'compras/proveedores',
    'clientes?vista=fiados': 'finanzas/fiados',
    caja: 'finanzas/caja',
    reportes: 'informes/ventas',
    configuracion: 'administracion',
    'configuracion/usuarios': 'administracion/usuarios',
    'configuracion/sucursales': 'administracion/sucursales',
    'configuracion/tienda': 'administracion/tienda-online',
  };
  for (const view of ['movimientos', 'ajustes', 'conteos', 'transferencias', 'valuacion']) {
    aliases['stock/' + view] = 'inventario/' + view;
  }
  for (const [legacy, canonical] of Object.entries(aliases)) {
    const href = '#/' + canonical;
    const parsed = resolveRoute('#/' + legacy);
    assert.equal(parsed.href, href);
    assert.deepEqual(parsed, resolveRoute(href));
  }
});

test('conserva parámetros y traslada solo los que identifican una vista', () => {
  assert.equal(resolveRoute('#/stock?ubicacion=abc&bajo=1').href, '#/inventario/existencias?ubicacion=abc&bajo=1');
  assert.equal(resolveRoute('#/reportes?dia=2026-01-01').href, '#/informes/ventas?dia=2026-01-01');
  assert.equal(resolveRoute('#/compras?proveedor=abc').href, '#/compras/ordenes?proveedor=abc');
  assert.equal(resolveRoute('#/compras/pendientes?proveedorId=abc').href, '#/compras/ordenes?proveedor=abc&vista=pendientes');
  assert.equal(resolveRoute('#/clientes?vista=fiados&q=Ana%20Sol').href, '#/finanzas/fiados?q=Ana+Sol');
  assert.equal(routeHref('compras', new URLSearchParams('proveedor=abc&vista=pendientes')), '#/compras/ordenes?proveedor=abc');
  assert.equal(resolveRoute('#/desconocida').href, '#/inicio');
});
