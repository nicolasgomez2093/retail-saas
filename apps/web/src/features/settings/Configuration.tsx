import { routeHref } from '../../app/routes';
import type { Store } from '../../shared/api/client';
import { BranchSetup } from '../home/BranchSetup';
import { useState } from 'react';
import { StoreAdmin } from '../stores/StoreAdmin';
import { UserAdmin } from '../users/UserAdmin';
import { PricingAdmin } from '../pricing/PricingAdmin';

export const configurationViews = [
  ['configuracion/sucursales', 'Sucursales', 'Administrá los datos y el estado de tus locales.'],
  ['configuracion/usuarios', 'Usuarios', 'Gestioná el equipo y sus accesos por sucursal.'],
  ['configuracion/precios', 'Precios', 'Creá listas, editá precios y asigná listas a sucursales.'],
] as const;

type ConfigurationView = 'configuracion' | typeof configurationViews[number][0];
export type ConfigurationAccess = 'loading' | 'allowed' | 'denied' | 'error';

export function Configuration({ view, branch, storeRevision, onStoresChanged, onPricesChanged }: {
  view?: ConfigurationView; branch?: Store; storeRevision: number; onStoresChanged: () => void; onPricesChanged: () => void;
}) {
  const [storesAccess, setStoresAccess] = useState<ConfigurationAccess>('loading');
  const [usersAccess, setUsersAccess] = useState<ConfigurationAccess>('loading');
  const [pricingAccess, setPricingAccess] = useState<ConfigurationAccess>('loading');
  const access = {
    'configuracion/sucursales': storesAccess,
    'configuracion/usuarios': usersAccess,
    'configuracion/precios': pricingAccess,
  };
  const visible = configurationViews.filter(([id]) => access[id] === 'allowed' || access[id] === 'error');
  const loading = Object.values(access).includes('loading');

  return <div hidden={!view}>
    {view === 'configuracion' && <>
      {branch && (storesAccess === 'allowed' || usersAccess === 'allowed') && <BranchSetup key={branch.id} branch={branch} storesAllowed={storesAccess === 'allowed'} />}
      {loading && <p role="status">Consultando accesos de administración…</p>}
      {!loading && visible.length === 0 && <p>No tenés acceso a las opciones de administración.</p>}
      <div className="quick-actions">
        {visible.map(([id, label, description]) => <a key={id} className="quick-card" href={routeHref(id)}>
          <strong>{label}<span aria-hidden="true">↗</span></strong>
          <span>{access[id] === 'error' ? 'Hay un problema de conexión. Abrí la sección para reintentar.' : description}</span>
        </a>)}
      </div>
    </>}
    {view && view !== 'configuracion' && access[view] === 'denied' &&
      <p role="status">No tenés permiso para administrar {configurationViews.find(([id]) => id === view)?.[1].toLocaleLowerCase('es-AR')}.</p>}
    <div className="feature-panel" hidden={view !== 'configuracion/sucursales' || storesAccess === 'denied'}>
      <StoreAdmin onSaved={onStoresChanged} onAccessChange={setStoresAccess} />
    </div>
    <div className="feature-panel" hidden={view !== 'configuracion/usuarios' || usersAccess === 'denied'}>
      <UserAdmin storeRevision={storeRevision} onAccessChange={setUsersAccess} />
    </div>
    <div className="feature-panel" hidden={view !== 'configuracion/precios' || pricingAccess === 'denied'}>
      <PricingAdmin storeRevision={storeRevision} onSaved={onPricesChanged} onAccessChange={setPricingAccess} />
    </div>
  </div>;
}
