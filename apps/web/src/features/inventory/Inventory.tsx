import { useEffect, useState, type FormEvent, useId } from 'react';
import {
  ApiError, api, type LocationPage, type Store,
} from '../../shared/api/client';

import { Adjustments } from './Adjustments';
import { Transfers } from './Transfers';
import { InventoryValuation } from './InventoryValuation';
type AdjustmentContext = {
  context: { scope: number }; scope: number; recoveryScope: string; linkedVariant: string; onAdjustmentBusy: (busy: boolean) => void;
};

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : 'No pudimos completar la operación.';
}

export const stockViews = [
  ['stock', 'Existencias'],
  ['stock/movimientos', 'Movimientos'],
  ['stock/ajustes', 'Ajustes y mínimos'],
  ['stock/conteos', 'Conteos'],
  ['stock/transferencias', 'Transferencias'],
  ['stock/valuacion', 'Valuación'],
] as const;
type StockView = typeof stockViews[number][0];

export function Inventory({ stores, stockRevision, view, active, linkedLocation, onLocationsChanged, onStockChanged, ...adjustment }: {
  stores: Store[]; stockRevision: number; view: StockView; active: boolean; linkedLocation: string; onLocationsChanged: () => void; onStockChanged: () => void;
} & AdjustmentContext) {
  const instanceId = useId();
  const [selection, setSelection] = useState('');
  const store = stores.find(item => item.id === selection) ?? stores[0];
  if (!store) return null;
  const storeId = store.id;

  return (
    <section aria-labelledby={instanceId + "-inventory-title"}>
      <h2 id={instanceId + "-inventory-title"} className="pos-sr-only">Inventario</h2>
      {stores.length > 1 && <label>Sucursal de inventario
        <select name="inventoryStore" value={storeId} onChange={(event) => setSelection(event.target.value)}>
          {stores.map((store) => <option key={store.id} value={store.id}>{store.name} ({store.code})</option>)}
        </select>
      </label>}
      <StoreInventory key={storeId} store={store} {...adjustment} stockRevision={stockRevision} view={view} active={active} linkedLocation={linkedLocation}
        onLocationsChanged={onLocationsChanged} onStockChanged={onStockChanged} />
    </section>
  );
}

function StoreInventory({ store, stockRevision, view, active, linkedLocation, onLocationsChanged, onStockChanged,
  context, scope, recoveryScope, linkedVariant, onAdjustmentBusy }: {
  store: Store; stockRevision: number; view: StockView; active: boolean; linkedLocation: string; onLocationsChanged: () => void; onStockChanged: () => void;
} & AdjustmentContext) {
  const storeId = store.id;
  const [locations, setLocations] = useState<LocationPage | null>(null);
  const [selection, setSelection] = useState({ link: '', id: '' });
  const [creating, setCreating] = useState(false);
  const [locationsScope, setLocationsScope] = useState<number>();
  const [localAdjustmentBusy, setLocalAdjustmentBusy] = useState(false);
  const [locationReload, setLocationReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [locationError, setLocationError] = useState('');
  const [notice, setNotice] = useState('');
  const selectedLocation = selection.link === linkedLocation ? selection.id : linkedLocation;
  const isAdjustment = view === 'stock/ajustes';
  const locationId = locations?.items.find(location => location.id === selectedLocation)?.id ??
    (isAdjustment && selectedLocation ? '' : locations?.items[0]?.id ?? '');
  const usesLocation = isAdjustment;

  useEffect(() => {
    if (!active) { setLocationError(''); setNotice(''); return; }
    setLocationError('');
    let current = true;
    const live = () => current && context.scope === scope;
    void api.locations(storeId).then((result) => {
      if (live()) {
        setLocations(result);
        setLocationsScope(scope);
        setLocationError('');
      }
    }).catch((cause: unknown) => {
      if (live()) {
        const status = cause instanceof ApiError ? cause.status : undefined;
        setLocationError(isAdjustment ? status === 401 ? 'La sesión venció. Volvé a iniciar sesión.' :
          status === 403 ? 'No tenés permiso para consultar ubicaciones.' :
            'No pudimos consultar las ubicaciones de esta sucursal.' : errorMessage(cause));
        setLocationsScope(undefined);
      }
    });
    return () => { current = false; };
  }, [active, storeId, locationReload, context, scope, isAdjustment]);

  async function createLocation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || localAdjustmentBusy) return;
    const fields = new FormData(event.currentTarget);
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const location = await api.createLocation({
        storeId, code: String(fields.get('code')), name: String(fields.get('name')),
        type: fields.get('type') === 'warehouse' ? 'warehouse' : 'sales_floor', isSellable: fields.has('sellable'),
      });
      setLocations((current) => current && { ...current, items: [...current.items, location].sort((a, b) => a.code.localeCompare(b.code)) });
      setSelection({ link: linkedLocation, id: location.id });
      setCreating(false);
      setNotice('Ubicación creada.');
      onLocationsChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  const locationTools = <div className="adjustment-location-tools" hidden={!usesLocation}>
        {locations?.canManage && !creating && <button type="button" className="button-secondary" disabled={localAdjustmentBusy} onClick={() => setCreating(true)}>Nueva ubicación</button>}
        {creating && (
          <form aria-label="Nueva ubicación" className="inventory-form" onSubmit={(event) => void createLocation(event)}>
            <label>Código<input name="code" required maxLength={40} /></label>
            <label>Nombre<input name="name" required maxLength={160} /></label>
            <label>Tipo<select name="type"><option value="sales_floor">Salón</option><option value="warehouse">Depósito</option></select></label>
            <label className="checkbox"><input name="sellable" type="checkbox" /> Ubicación vendible</label>
            <div className="actions">
              <button disabled={busy}>Guardar ubicación</button>
              <button type="button" disabled={busy} onClick={() => setCreating(false)}>Cancelar</button>
            </div>
          </form>
        )}
        {locations && locations.items.length === 0 && <p>Todavía no hay ubicaciones en esta sucursal.</p>}
      </div>;

  return (
    <>
      {error && <p role="alert">{error}</p>}
      {locationError && <p className="auxiliary-status">{locationError}</p>}
      {(!locations || isAdjustment && locationsScope !== scope && locationError) && (locationError ? <button type="button" onClick={() => {
        setLocationError('');
        setLocationReload((value) => value + 1);
      }}>Reintentar</button> : <p role="status">Cargando ubicaciones…</p>)}
      {notice && <p role="status">{notice}</p>}
      {!isAdjustment && locationTools}
      {locations && <Adjustments store={store} locations={locations} locationId={locationId} linkedVariant={linkedVariant}
        context={context} scope={scope} revision={stockRevision} recoveryScope={recoveryScope} locationTools={isAdjustment ? locationTools : undefined} permissionFresh={locationsScope === scope}
        onLocation={id => setSelection({ link: linkedLocation, id })} active={active && isAdjustment}
        onBusy={value => { setLocalAdjustmentBusy(value); onAdjustmentBusy(value); }} onChanged={onStockChanged} />}
      {locations && <div hidden={view !== 'stock/transferencias'}>
        <Transfers storeId={storeId} origins={locations.items} revision={stockRevision} onStockChanged={onStockChanged} />
      </div>}
      {locations && <div hidden={view !== 'stock/valuacion'}>
        <InventoryValuation storeId={storeId} revision={stockRevision} />
      </div>}
    </>
  );
}
