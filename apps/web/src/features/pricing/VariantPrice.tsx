import { useEffect, useId, useState } from 'react';
import { api, ApiError, type ResolvedPrice, type Store } from '../../shared/api/client';

export type PriceAccess = {
  isCurrent: () => boolean;
  onSessionExpired: () => void;
};

function PriceRow({ store, variantId, access, secondary = false }: { store: Store; variantId: string; access: PriceAccess; secondary?: boolean }) {
  const [result, setResult] = useState<ResolvedPrice>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    if (!access.isCurrent()) return;
    void api.resolvePrice(store.id, variantId).then((price) => {
      if (active && access.isCurrent()) setResult(price);
    }).catch((cause: unknown) => {
      if (!active || !access.isCurrent()) return;
      const status = cause instanceof ApiError ? cause.status : undefined;
      if (status === 401) access.onSessionExpired();
      else setError(status === 403 ? 'No tenés permiso para consultar este precio.'
        : status === 404 ? 'Precio no disponible.'
        : status === 0 ? 'No pudimos conectar para consultar el precio.'
        : 'No pudimos consultar el precio.');
    });
    return () => { active = false; };
  }, [store.id, variantId, attempt, access]);

  const amount = result?.amount;
  return <div className="catalog-branch-price" data-secondary={secondary || undefined}>
    {!secondary && <strong className="catalog-price-store">{store.name}</strong>}
    {error ? <div className="catalog-price-error">
      <p role="alert">{error}</p>
      <button type="button" className="button-secondary" aria-label={'Reintentar precio en ' + store.name} onClick={() => {
        setError('');
        setAttempt((current) => current + 1);
      }}>Reintentar</button>
    </div> : <div className="catalog-price-reading" role="status">
      {secondary && <span className="pos-sr-only">Precio en {store.name}: </span>}
      {result && <strong className="catalog-price-amount">{result.amount === null ? '—' : result.currency + ' ' + result.amount.replace('.', ',')}</strong>}
      <span>{!result ? 'Consultando precio…' : amount === null ? 'Sin precio configurado' : 'Configurado'}</span>
    </div>}
  </div>;
}

export function VariantPrice({ variantId, store, stores, revision, access }: {
  variantId: string; store?: Store; stores: Store[]; revision: number; access: PriceAccess;
}) {
  const local = stores.find((candidate) => candidate.id === store?.id);
  if (!store) return <p>No hay una sucursal activa para consultar el precio.</p>;
  if (!local) return <p role="status">No tenés acceso a la sucursal seleccionada.</p>;
  return <BranchPrices key={JSON.stringify([variantId, local.id])} store={local} stores={stores}
    variantId={variantId} revision={revision} access={access} />;
}

function BranchPrices({ store, stores, variantId, revision, access }: {
  store: Store; stores: Store[]; variantId: string; revision: number; access: PriceAccess;
}) {
  const id = useId();
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const alternatives = stores.filter((candidate) => candidate.id !== store.id);

  return <section className="catalog-branch-prices" aria-label="Precios por sucursal">
    <h4>Precio en sucursal activa</h4>
    <PriceRow key={revision} store={store} variantId={variantId} access={access} />
    {alternatives.length > 0 && <>
      <button type="button" className="button-secondary" aria-expanded={expanded} aria-controls={id}
        onClick={() => {
          if (expanded) setSelected([]);
          setExpanded(!expanded);
        }}>{expanded ? 'Cerrar comparación' : 'Comparar precios'}</button>
      <div id={id} hidden={!expanded}>
        {expanded && <fieldset>
          <legend>Otras sucursales autorizadas</legend>
          {alternatives.map((candidate) => <div className="catalog-comparison-row" key={candidate.id}>
            <label className="catalog-price-choice">
              <input type="checkbox" checked={selected.includes(candidate.id)} onChange={(event) => {
                const checked = event.target.checked;
                setSelected((current) => checked ? [...current, candidate.id] : current.filter((value) => value !== candidate.id));
              }} /> {candidate.name} ({candidate.code})
            </label>
            {selected.includes(candidate.id) && <PriceRow key={revision} store={candidate} variantId={variantId} access={access} secondary />}
          </div>)}
        </fieldset>}
      </div>
    </>}
  </section>;
}
