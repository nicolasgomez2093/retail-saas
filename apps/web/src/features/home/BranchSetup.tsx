import { useEffect, useState } from 'react';
import { api, ApiError, type Store, type StockLocation } from '../../shared/api/client';

type Key = 'location' | 'register' | 'session' | 'list' | 'merchandise';
type Check = {
  status: 'complete' | 'pending' | 'denied' | 'error' | 'unknown';
  note?: string;
  link?: { href: string; label: string };
};

const statusLabels = {
  complete: 'Verificado',
  pending: 'Pendiente confirmado',
  denied: 'Sin permiso para verificar',
  error: 'Error de consulta',
  unknown: 'No verificable',
} satisfies Record<Check['status'], string>;

function unavailable(error: unknown): Check {
  if (error instanceof ApiError) {
    if (error.status === 403) return { status: 'denied' };
    if (error.status === 401) return { status: 'unknown', note: 'Volvé a iniciar sesión para verificar.' };
    if (error.status === 404) return { status: 'unknown', note: 'La información no está disponible para esta sucursal.' };
  }
  return { status: 'error' };
}

const probeLimit = 12;

async function checkMerchandise(storeId: string, locations: StockLocation[], active: () => boolean): Promise<Check> {
  const sellable = new Set(locations.filter((location) => location.isSellable).map((location) => location.id));
  const link = { href: '#/productos', label: 'Revisar productos' };
  if (!sellable.size) return { status: 'pending', note: 'Primero necesitás un depósito vendible.' };

  const { items } = await api.stockBalances(storeId, '');
  const candidates = [...new Set(items.filter((row) =>
    row.active && row.available > 0 && sellable.has(row.locationId)).map((row) => row.variantId))];
  if (!candidates.length) return { status: 'pending', note: 'No hay variantes activas con stock disponible en depósitos vendibles.', link };

  let failed: Check | undefined;
  const reasons = new Set<string>();
  const sample = candidates.slice(0, probeLimit);
  for (let offset = 0; offset < sample.length && active(); offset += 4) {
    const prices = await Promise.allSettled(sample.slice(offset, offset + 4).map((id) => api.resolvePrice(storeId, id)));
    if (prices.some((price) => price.status === 'fulfilled' && price.value.reason === 'configured' && price.value.amount !== null)) {
      return { status: 'complete', note: 'Se confirmó al menos una variante activa con stock disponible en una ubicación vendible y un precio aplicable.' };
    }
    for (const price of prices) {
      if (price.status === 'rejected') {
        // A lookup can return 404 if a candidate changed after stock was loaded.
        const check = price.reason instanceof ApiError && price.reason.status === 404
          ? { status: 'unknown' as const, note: 'Una variante dejó de estar disponible para consulta.' }
          : unavailable(price.reason);
        if (!failed || check.status === 'error') failed = check;
      } else {
        reasons.add(price.value.reason);
      }
    }
  }

  if (failed) return failed;
  if (candidates.length > probeLimit) {
    return { status: 'unknown', note: 'No se confirmó un precio entre las primeras 12 variantes con stock. Quedan variantes por revisar.', link };
  }
  if (reasons.size === 1 && reasons.has('no_list')) {
    return { status: 'pending', note: 'La consulta de precios confirmó que falta una lista asignada.', link };
  }
  if (reasons.size === 1 && reasons.has('no_price')) {
    return { status: 'pending', note: 'No se encontró un precio aplicable para las variantes con stock consultadas.', link };
  }
  return { status: 'unknown', note: 'Las consultas no confirmaron una condición de precios consistente.', link };
}

function SetupRow({ label, check }: { label: string; check?: Check }) {
  return <li className="setup-row">
    <strong>{label}</strong>
    <span className="setup-status" data-status={check?.status}>{check ? statusLabels[check.status] : 'Verificando…'}</span>
    {check?.note && <small>{check.note}</small>}
    {check?.link && <a href={check.link.href}>{check.link.label}</a>}
  </li>;
}

export function BranchSetup({ branch, storesAllowed }: { branch: Store; storesAllowed: boolean }) {
  const [retry, setRetry] = useState(0);
  const [checks, setChecks] = useState<Partial<Record<Key, Check>>>({});
  const [team, setTeam] = useState<Check>();

  useEffect(() => {
    let active = true;
    const save = (results: Partial<Record<Key, Check>>) => {
      if (active) setChecks((previous) => ({ ...previous, ...results }));
    };
    void api.locations(branch.id).then((data) => {
      const sellable = data.items.some((location) => location.isSellable);
      save({ location: sellable ? { status: 'complete' } : {
        status: 'pending', note: 'No hay un depósito habilitado para vender.',
        link: data.canManage ? { href: '#/inventario/existencias', label: 'Configurar depósito' } : undefined,
      } });
      if (active) void checkMerchandise(branch.id, data.items, () => active).then((merchandise) => {
        save({ merchandise });
      }).catch((error: unknown) => save({ merchandise: unavailable(error) }));
    }).catch((error: unknown) => save({ location: unavailable(error), merchandise: unavailable(error) }));

    void api.cashRegisters(branch.id).then(({ items, canManage }) => {
      const accessible = items.some((register) => register.openSessionId !== null);
      const free = items.some((register) => !register.occupied);
      save({
        register: items.length ? { status: 'complete' } : {
          status: 'pending', link: canManage ? { href: '#/finanzas/caja', label: 'Configurar caja' } : undefined,
        },
        session: accessible ? { status: 'complete', note: 'La consulta muestra una sesión abierta dentro de tu alcance.' } : {
          status: 'pending',
          note: !items.length ? 'Primero necesitás una caja.' : free ? 'Todavía no hay una sesión disponible para operar.' : 'Las cajas están ocupadas por otras sesiones.',
          link: free ? { href: '#/finanzas/caja', label: 'Ir a Caja' } : undefined,
        },
      });
    }).catch((error: unknown) => save({ register: unavailable(error), session: unavailable(error) }));

    void api.priceLists().then(({ stores }) => {
      const assignment = stores.find(({ id }) => id === branch.id);
      save({ list: !assignment ? { status: 'unknown', note: 'La sucursal no aparece en la consulta de listas.' } : assignment.listId ? {
        status: 'complete', note: 'Se verificó la asignación. Los precios aplicables se comprueban por variante.',
      } : { status: 'pending', link: { href: '#/productos/precios', label: 'Asignar lista' } } });
    }).catch((error: unknown) => save({ list: unavailable(error) }));

    void api.users().then(({ stores, items }) => {
      const count = items.filter((user) => user.status === 'active' && user.storeIds.includes(branch.id)).length;
      if (active) setTeam({ status: 'complete', note: count === 1 ? '1 integrante activo visible' : count + ' integrantes activos visibles',
        link: stores.some(({ id }) => id === branch.id) ? { href: '#/administracion/usuarios', label: 'Gestionar equipo' } : undefined });

    }).catch((error: unknown) => { if (active) setTeam(unavailable(error)); });
    return () => { active = false; };
  }, [branch.id, retry]);

  const results = Object.values(checks);
  const completed = 1 + results.filter(({ status }) => status === 'complete').length;
  const pending = results.filter(({ status }) => status === 'pending').length;
  const unverifiable = results.filter(({ status }) => status === 'unknown' || status === 'denied').length;
  const errors = results.filter(({ status }) => status === 'error').length;
  const checking = 5 - results.length;
  const summary = [
    completed + (completed === 1 ? ' verificada' : ' verificadas'),
    pending && pending + (pending === 1 ? ' pendiente' : ' pendientes'),
    unverifiable && unverifiable + (unverifiable === 1 ? ' no verificable' : ' no verificables'),
    errors && errors + (errors === 1 ? ' con error de consulta' : ' con errores de consulta'),
    checking && checking + ' en consulta',
  ].filter(Boolean).join(' · ');
  const failed = team?.status === 'error' || errors > 0;

  return <details className="branch-setup feature-panel" open>
    <summary><strong>Preparar sucursal</strong><span>{summary}</span></summary>
    <p>Se revisan seis condiciones con los datos disponibles. El punto de venta confirma si podés operar.</p>
    <div className="setup-groups">
      <section aria-label="Configuración de la sucursal">
        <h3>Configuración</h3>
        <ul>
          <SetupRow label="Sucursal activa" check={{ status: 'complete', link: storesAllowed ? { href: '#/administracion/sucursales', label: 'Ver sucursales' } : undefined }} />
          <SetupRow label="Depósito vendible" check={checks.location} />
          <SetupRow label="Caja configurada" check={checks.register} />
          <SetupRow label="Lista de precios asignada" check={checks.list} />
        </ul>
      </section>
      <section aria-label="Operación y mercadería">
        <h3>Operación de caja</h3>
        <ul><SetupRow label="Sesión de caja disponible" check={checks.session} /></ul>
        <h3 className="setup-merchandise">Mercadería</h3>
        <ul><SetupRow label="Mercadería lista para vender" check={checks.merchandise} /></ul>
        <p className="setup-team">Equipo opcional · {team?.note ?? (team ? statusLabels[team.status] : 'Verificando acceso…')}
          {team?.link && <> · <a href={team.link.href}>{team.link.label}</a></>}</p>
      </section>
    </div>
    {failed && <button type="button" className="button-secondary" onClick={() => {
      setChecks({});
      setTeam(undefined);
      setRetry((value) => value + 1);
    }}>Volver a verificar</button>}
  </details>;
}
