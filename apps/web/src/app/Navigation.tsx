import { resolveRoute, routeHref, type Destination } from './routes';
import { useEffect, useSyncExternalStore } from 'react';
import { stockViews } from '../features/inventory/Inventory';
import { api } from '../shared/api/client';
import {
  checkNavigationAccess, navigationAccess, navigationAccessKey, navigationAccessVersion, subscribeNavigationAccess,
} from '../shared/api/navigationAccess';

type AccessResource = 'cash' | 'stock' | 'purchases' | 'suppliers' | 'stores' | 'users' | 'prices' | 'storefront';
type Entry = { label: string; route: Destination; access?: AccessResource };
type Group = { id: string; label: string; entries: Entry[] };
const stockEntries: Entry[] = stockViews.map(([route, label]) => ({ label, route, access: 'stock' }));
const groups: Group[] = [
  { id: 'inicio', label: 'Inicio', entries: [{ label: 'Inicio', route: 'inicio' }] },
  { id: 'productos', label: 'Productos', entries: [
    { label: 'Catálogo', route: 'productos' },
  ] },
  { id: 'vender', label: 'Vender', entries: [
    { label: 'Punto de venta', route: 'vender', access: 'cash' }, { label: 'Historial de ventas', route: 'ventas', access: 'cash' },
  ] },
  { id: 'stock', label: 'Inventario', entries: stockEntries },
  { id: 'clientes', label: 'Clientes', entries: [{ label: 'Directorio', route: 'clientes', access: 'cash' }, { label: 'Fiados y abonos', route: 'fiados', access: 'cash' }] },
  { id: 'compras', label: 'Compras', entries: [
    { label: 'Compras', route: 'compras', access: 'purchases' },
    { label: 'Proveedores', route: 'proveedores', access: 'suppliers' },
  ] },
  { id: 'caja', label: 'Caja', entries: [{ label: 'Caja', route: 'caja', access: 'cash' }] },
  { id: 'reportes', label: 'Reportes', entries: [{ label: 'Reportes', route: 'reportes', access: 'cash' }] },
  { id: 'tienda', label: 'Tienda online', entries: [{ label: 'Tienda online', route: 'tienda', access: 'storefront' }] },
  // The hub behind «Más» on phones, plus the administration pages under Configuración.
  { id: 'gestion', label: 'Administración', entries: [
    { label: 'Más opciones', route: 'gestion' },
    { label: 'Listas de precios', route: 'configuracion/precios', access: 'prices' },
    { label: 'Configuración', route: 'configuracion' },
    { label: 'Sucursales', route: 'configuracion/sucursales', access: 'stores' },
    { label: 'Usuarios y permisos', route: 'configuracion/usuarios', access: 'users' },
  ] },
];
const mobileEntries: Entry[] = [
  { label: 'Inicio', route: 'inicio' }, { label: 'Vender', route: 'vender', access: 'cash' },
  { label: 'Productos', route: 'productos' }, { label: 'Inventario', route: 'stock', access: 'stock' },
];

export function navigationContext(destination: Destination) {
  const menuRoute = destination === 'compras/pendientes' ? 'compras' : destination;
  const group = groups.find((candidate) => candidate.entries.some((entry) => entry.route === menuRoute)) ?? groups[0];
  const entry = group.entries.find((candidate) => candidate.route === menuRoute) ?? groups[0].entries[0];
  return { route: destination, menuRoute, group: group.id, groupLabel: group.label, entry,
    label: destination === 'compras/pendientes' ? 'Pendientes de recepción' : entry.label };
}

export function useRouteAccess(context: ReturnType<typeof navigationContext>, branchId?: string) {
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  return entryAccess(context.entry, branchId);
}

const reportLinks: Entry[] = [
  { label: 'Valuación de inventario', route: 'stock/valuacion', access: 'stock' },
  { label: 'Consultar órdenes de compra', route: 'compras', access: 'purchases' },
  { label: 'Consultar fiados y abonos', route: 'fiados', access: 'cash' },
];

export function ContextLinks({ branchId }: { branchId?: string }) {
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  useEffect(() => {
    for (const { access } of reportLinks) if (access) probe(access, branchId);
  }, [branchId]);
  const links = reportLinks.filter((entry) => entryAccess(entry, branchId) === 'allowed');
  if (!links.length) return null;
  return <nav className="report-links" aria-label="Consultas relacionadas">
    {links.map((entry) => <a key={entry.route} href={routeHref(entry.route)}>{entry.label}</a>)}
  </nav>;
}

function accessKey(resource: AccessResource, branchId?: string) {
  switch (resource) {
    case 'cash': return navigationAccessKey('/cash-registers', branchId);
    case 'purchases': return navigationAccessKey('/purchase-orders', branchId);
    case 'suppliers': return '/suppliers';
    case 'stores': return '/stores/admin';
    case 'users': return '/users';
    case 'prices': return '/price-lists';
    case 'storefront': return '/storefront';
    case 'stock': return '';
  }
}

function entryAccess(entry: Entry, branchId?: string) {
  const resource = entry.access;
  if (!resource) {
    if (entry.route === 'configuracion') {
      const outcomes = [navigationAccess('/stores/admin'), navigationAccess('/users')];
      if (outcomes.includes('allowed')) return 'allowed';
      if (outcomes.every((value) => value === 'denied')) return 'denied';
      return 'checking';
    }
    return 'allowed';
  }
  if (resource === 'stock') return branchId ? 'allowed' : 'denied';
  if ((resource === 'cash' || resource === 'purchases') && !branchId) return 'denied';
  return navigationAccess(accessKey(resource, branchId)) ?? 'checking';
}

function probe(resource: AccessResource, branchId?: string, retry = false) {
  if (resource === 'stock') return;
  const key = accessKey(resource, branchId);
  switch (resource) {
    case 'cash': if (branchId) checkNavigationAccess(key, () => api.cashRegisters(branchId), retry); break;
    case 'purchases': if (branchId) checkNavigationAccess(key, () => api.purchaseOrders(branchId, 0, {
      status: '', query: '', fromDate: '', toDate: '',
    }), retry); break;
    case 'suppliers': checkNavigationAccess(key, () => api.suppliers('', 0), retry); break;
    case 'stores': checkNavigationAccess(key, api.managedStores, retry); break;
    case 'users': checkNavigationAccess(key, api.users, retry); break;
    case 'prices': checkNavigationAccess(key, api.priceLists, retry); break;
    case 'storefront': checkNavigationAccess(key, api.storefront, retry); break;
  }
}
const menuIcons: Partial<Record<Destination, string>> = {
  inicio: '⌂', productos: '▦', vender: '＋', stock: '▤', clientes: '♧',
  gestion: '⋯', caja: '◴', compras: '◈', reportes: '▥', configuracion: '⚙', tienda: '◎',
};
const desktopEntries: Entry[] = [
  { label: 'Inicio', route: 'inicio' }, { label: 'Productos', route: 'productos' },
  { label: 'Vender', route: 'vender', access: 'cash' }, { label: 'Inventario', route: 'stock', access: 'stock' },
  { label: 'Clientes', route: 'clientes', access: 'cash' }, { label: 'Caja', route: 'caja', access: 'cash' },
  { label: 'Compras', route: 'compras', access: 'purchases' }, { label: 'Reportes', route: 'reportes', access: 'cash' },
  { label: 'Tienda online', route: 'tienda', access: 'storefront' },
];

export function SalesShortcut({ branchId, primary = false, label = 'Ir a vender', icon }: { branchId?: string; primary?: boolean; label?: string; icon?: string }) {
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  if (entryAccess(mobileEntries[1], branchId) !== 'allowed') return null;
  return <a className={primary ? 'primary-link' : 'primary-link workspace-shortcut'} href="#/vender">{icon && <span aria-hidden="true">{icon}</span>}{label}</a>;
}

export function Navigation({ destination, branchId, user, busy, onLogout, onCurrentLink }: {
  destination: ReturnType<typeof navigationContext>; branchId?: string; user: string; busy: boolean;
  onLogout: () => void; onCurrentLink: () => void;
}) {
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  function menuHref(route: Destination) {
    const hash = window.location.hash;
    const current = resolveRoute(hash);
    if ((route === 'productos' || route === 'stock' || route === 'stock/movimientos') && current.destination === route) return hash;
    if (current.destination === route || (route === 'compras' && current.destination === 'compras/pendientes')) return current.href;
    return routeHref(route);
  }


  useEffect(() => {
    const handle = window.setTimeout(() => {
      probe('cash', branchId);
      probe('purchases', branchId);
      probe('stores', branchId);
      probe('users', branchId);
      probe('storefront', branchId);
      if (destination.entry.access) probe(destination.entry.access, branchId);
      if (destination.route === 'configuracion' || destination.route === 'gestion') {
        probe('stores', branchId);
        probe('users', branchId);
      }
      for (const group of groups) {
        if (group.id === destination.group) {
          for (const { access } of group.entries) {
            if (access) probe(access, branchId);
          }
        }
      }
    }, 0);
    return () => window.clearTimeout(handle);
  }, [branchId, destination.group, destination.route, destination.entry.access]);

  function renderEntry(entry: Entry, mobile = false) {
    const access = entryAccess(entry, branchId);
    const route = entry.route;
    const direct = destination.menuRoute === route;
    const group = navigationContext(route).group;
    const contextual = route !== 'gestion' && group !== 'gestion' && destination.group === group;
    // Desktop: administration pages light up Configuración. Phones: «Más» covers every section without its own tab.
    const management = mobile
      ? route === 'gestion' && !mobileEntries.some(candidate => navigationContext(candidate.route).group === destination.group)
      : route === 'configuracion' && destination.group === 'gestion' && destination.menuRoute !== 'gestion';
    return <a key={route} href={menuHref(route)} className={mobile ? undefined : 'navigation-group'}
      aria-current={direct || contextual || management ? 'page' : undefined}
      aria-disabled={access !== 'allowed' || undefined}
      onClick={event => {
        if (access !== 'allowed') { event.preventDefault(); return; }
        if (window.location.hash === event.currentTarget.hash) onCurrentLink();
      }}>
      <span className="reference-nav-icon" aria-hidden="true">{menuIcons[route]}</span>{entry.label}
    </a>;
  }

  const initials = user.trim().split(/\s+/).slice(0, 2).map(part => [...part][0]).join('').toUpperCase() || 'U';
  return <>
    <aside className="sidebar retail-sidebar reference-sidebar">
      <div className="brand" aria-label="Retail">retail<span className="commercial-brand-dot">•</span></div>
      <nav id="retail-navigation" className="retail-navigation" aria-label="Navegación principal">
        {desktopEntries.map(entry => renderEntry(entry))}
      </nav>
      <footer className="reference-sidebar-footer">
        {renderEntry({ label: 'Configuración', route: 'configuracion' })}
        <details className="navigation-account">
          <summary aria-label={'Cuenta de ' + user}><span aria-hidden="true">{initials}</span><strong>{user}</strong></summary>
          <button type="button" disabled={busy} onClick={onLogout}>Cerrar sesión</button>
        </details>
      </footer>
    </aside>
    <nav className="mobile-bottom-nav retail-bottom-nav reference-bottom" aria-label="Accesos rápidos">
      {[...mobileEntries, { label: 'Más', route: 'gestion' } as Entry].map(entry => renderEntry(entry, true))}
    </nav>
  </>;
}

export function ManagementHub({ branchId }: { branchId?: string }) {
  useSyncExternalStore(subscribeNavigationAccess, navigationAccessVersion);
  const cards: (Entry & { description: string })[] = [
    { route: 'caja', access: 'cash', label: 'Caja y movimientos', description: 'Apertura, cierre y arqueo' },
    { route: 'compras', access: 'purchases', label: 'Compras y proveedores', description: 'Pedidos y recepción' },
    { route: 'reportes', access: 'cash', label: 'Reportes', description: 'Ventas e indicadores' },
    { route: 'productos', label: 'Catálogo', description: 'Artículos, variantes y precios' },
    { route: 'clientes', access: 'cash', label: 'Clientes', description: 'Contactos y saldos' },
    { route: 'stock', access: 'stock', label: 'Inventario', description: 'Existencias y ajustes' },
    { route: 'tienda', access: 'storefront', label: 'Tienda online', description: 'Catálogo público y pedidos por WhatsApp' },
  ];
  return <>
    <p className="reference-management-intro">Herramientas para operar y administrar tu comercio.</p>
    <nav className="commercial-management reference-management" aria-label="Operaciones de gestión">
      {cards.map(card => {
        const access = entryAccess(card, branchId);
        return <a key={card.route} href={routeHref(card.route)} aria-disabled={access !== 'allowed' || undefined}
          onClick={event => {
            if (access !== 'allowed') event.preventDefault();
            if (access === 'error' && card.access) probe(card.access, branchId, true);
          }}>
          <span aria-hidden="true">{card.route === 'caja' ? '◴' : menuIcons[card.route]}</span>
          <strong>{card.label} →</strong><small>{card.description}</small>
        </a>;
      })}
    </nav>
    <ContextualAdministration branchId={branchId} />
  </>;
}

function ContextualAdministration({ branchId }: { branchId?: string }) {
  const entries: Entry[] = [
    { label: 'Listas de precios', route: 'configuracion/precios', access: 'prices' },
    { label: 'Configuración', route: 'configuracion' },
  ];
  const allowed = entries.filter(entry => entryAccess(entry, branchId) === 'allowed');
  if (!allowed.length) return null;
  return <nav className="report-links" aria-label="Administración del comercio">
    {allowed.map(entry => <a key={entry.route} href={routeHref(entry.route)}>{entry.label}</a>)}
  </nav>;
}
