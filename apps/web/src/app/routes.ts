const paths = {
  inicio: 'inicio',
  gestion: 'gestion',
  vender: 'vender',
  ventas: 'ventas/historial',
  productos: 'productos',
  'configuracion/precios': 'productos/precios',
  stock: 'inventario/existencias',
  'stock/movimientos': 'inventario/movimientos',
  'stock/ajustes': 'inventario/ajustes',
  'stock/conteos': 'inventario/conteos',
  'stock/transferencias': 'inventario/transferencias',
  'stock/valuacion': 'inventario/valuacion',
  compras: 'compras/ordenes',
  'compras/pendientes': 'compras/ordenes',
  proveedores: 'compras/proveedores',
  clientes: 'clientes',
  fiados: 'finanzas/fiados',
  caja: 'finanzas/caja',
  reportes: 'informes/ventas',
  configuracion: 'administracion',
  'configuracion/sucursales': 'administracion/sucursales',
  'configuracion/usuarios': 'administracion/usuarios',
  tienda: 'tienda-online',
} as const;

export type Destination = keyof typeof paths;
const lookup = new Map<string, Destination>();
for (const destination of Object.keys(paths) as Destination[]) {
  lookup.set(destination, destination);
  if (destination !== 'compras/pendientes') lookup.set(paths[destination], destination);
}
// Earlier address, when the storefront lived under Configuración.
lookup.set('administracion/tienda-online', 'tienda');
lookup.set('configuracion/tienda', 'tienda');

export function routeHref(destination: Destination, query?: URLSearchParams) {
  const params = new URLSearchParams(query);
  if (destination === 'compras/pendientes') params.set('vista', 'pendientes');
  if (destination === 'compras' && params.get('vista') === 'pendientes') params.delete('vista');
  const search = params.toString();
  return '#/' + paths[destination] + (search ? '?' + search : '');
}

export function resolveRoute(hash: string) {
  const [path, search] = hash.startsWith('#/') ? hash.slice(2).split(/\?(.*)/s, 2) : ['', ''];
  const params = new URLSearchParams(search);
  let destination = lookup.get(path) ?? 'inicio';
  if (!lookup.has(path)) return { destination, query: '', href: routeHref(destination) };
  if (destination === 'clientes' && params.get('vista') === 'fiados') destination = 'fiados';
  if (destination === 'fiados' && params.get('vista') === 'fiados') params.delete('vista');
  if (destination === 'compras' && params.get('vista') === 'pendientes') destination = 'compras/pendientes';
  if ((destination === 'compras' || destination === 'compras/pendientes' || destination === 'proveedores') && params.has('proveedorId')) {
    if (!params.has('proveedor')) params.set('proveedor', params.get('proveedorId') ?? '');
    params.delete('proveedorId');
  }
  const href = routeHref(destination, params);
  return { destination, query: href.split('?', 2)[1] ?? '', href };
}
