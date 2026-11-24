import { useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { api, ApiError, type BarcodeMatch, type StockLocation, type Store } from '../../shared/api/client';
import type { PriceAccess } from '../pricing/VariantPrice';

type ScanResult =
  | { state: 'waiting' }
  | { state: 'loading' | 'missing'; code: string }
  | { state: 'found'; code: string; match: BarcodeMatch }
  | { state: 'invalid' | 'denied' }
  | { state: 'error'; code: string; message: string };

export function BarcodeScanner({ access, children }: {
  access: PriceAccess;
  children: (match: BarcodeMatch) => ReactNode;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState<ScanResult>({ state: 'waiting' });
  const button = useRef<HTMLButtonElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const pending = useRef<{ code: string } | null>(null);

  useLayoutEffect(() => () => { pending.current = null; }, []);

  function close() {
    pending.current = null;
    setResult({ state: 'waiting' });
    setOpen(false);
    button.current?.focus();
  }

  async function lookup(code: string) {
    if (!access.isCurrent() || pending.current?.code === code) return;
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$/.test(code)) {
      pending.current = null;
      setResult({ state: 'invalid' });
      return;
    }
    const attempt = { code };
    pending.current = attempt;
    setResult({ state: 'loading', code });
    const current = () => pending.current === attempt && access.isCurrent();
    try {
      const match = await api.barcode(code);
      if (current()) setResult({ state: 'found', code, match });
    } catch (cause) {
      if (!current()) return;
      const status = cause instanceof ApiError ? cause.status : undefined;
      if (status === 401) access.onSessionExpired();
      else if (status === 403) setResult({ state: 'denied' });
      else if (status === 404) setResult({ state: 'missing', code });
      else setResult({ state: 'error', code, message: status === 0
        ? 'No pudimos conectar para buscar el código.' : 'No pudimos consultar el código.' });
    } finally {
      if (pending.current === attempt) pending.current = null;
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!open || !input.current) return;
    const code = input.current.value;
    input.current.select();
    void lookup(code);
  }

  useLayoutEffect(() => {
    if (open && document.activeElement === button.current && access.isCurrent()) input.current?.focus();
  }, [open, access]);

  return <section className="catalog-scanner" aria-label="Modo escáner" onKeyDown={(event) => {
    if (open && event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  }}>
    <button ref={button} type="button" className="button-secondary" aria-expanded={open} aria-controls={id}
      onClick={() => {
        if (open) close();
        else {
          button.current?.focus();
          setOpen(true);
        }
      }}>{open ? 'Cerrar escáner' : 'Activar escáner'}</button>
    <div id={id} hidden={!open}>
      <form className="catalog-scanner-form" aria-label="Buscar código de barras" onSubmit={submit}>
        <label>Código de barras
          <input ref={input} name="barcode" type="search" autoComplete="off" autoCapitalize="none" spellCheck={false}
            maxLength={80} required />
        </label>
        <button>Buscar código</button>
      </form>
      <p role="status">
        {result.state === 'waiting' && 'Esperando lectura. Escaneá o escribí un código y presioná Enter.'}
        {result.state === 'loading' && 'Buscando código ' + result.code + '…'}
        {result.state === 'missing' && 'No encontramos el código ' + result.code + '.'}
        {result.state === 'found' && 'Código encontrado: ' + result.code}
      </p>
      {result.state === 'invalid' && <p role="alert">Ingresá un código de barras válido, sin espacios.</p>}
      {result.state === 'denied' && <p role="alert">No tenés permiso para consultar este código.</p>}
      {result.state === 'error' && <div>
        <p role="alert">{result.message}</p>
        <button type="button" className="button-secondary" onClick={() => void lookup(result.code)}>Reintentar código</button>
      </div>}
      {result.state === 'found' && <section aria-label="Resultado del código de barras">{children(result.match)}</section>}
    </div>
  </section>;
}

export function ScannedStock({ store, variantId, revision, access }: {
  store: Store; variantId: string; revision: number; access: PriceAccess;
}) {
  const [locations, setLocations] = useState<StockLocation[]>();
  const [locationId, setLocationId] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    let current = true;
    if (!access.isCurrent()) return;
    void api.locations(store.id).then(({ items }) => {
      if (current && access.isCurrent()) setLocations(items.filter((location) => location.isSellable));
    }).catch((cause: unknown) => {
      if (!current || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      else setError(cause instanceof ApiError && cause.status === 403 ? 'No tenés permiso para consultar stock.' : 'Stock no disponible.');
    });
    return () => { current = false; };
  }, [store.id, access]);

  const location = locations?.find((item) => item.id === locationId) ?? locations?.[0];
  if (error) return <p role="alert">{error}</p>;
  if (!locations) return <p role="status">Consultando ubicaciones de {store.name}…</p>;
  if (!location) return <p>Sin ubicaciones vendibles en {store.name}.</p>;
  return <>
    <label>Ubicación de stock en {store.name}
      <select value={location.id} onChange={(event) => setLocationId(event.target.value)}>
        {locations.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select>
    </label>
    <StockAmount key={JSON.stringify([location.id, variantId, revision])} storeId={store.id} locationId={location.id}
      variantId={variantId} access={access} />
  </>;
}

function StockAmount({ storeId, locationId, variantId, access }: {
  storeId: string; locationId: string; variantId: string; access: PriceAccess;
}) {
  const [text, setText] = useState('Consultando stock…');
  useEffect(() => {
    let current = true;
    if (!access.isCurrent()) return;
    void api.stockBalances(storeId, locationId).then(({ items }) => {
      if (!current || !access.isCurrent()) return;
      const balance = items.find((item) => item.variantId === variantId);
      setText(balance ? 'Disponible en esta ubicación: ' + balance.available : 'Sin saldo registrado en esta ubicación.');
    }).catch((cause: unknown) => {
      if (!current || !access.isCurrent()) return;
      if (cause instanceof ApiError && cause.status === 401) access.onSessionExpired();
      else setText(cause instanceof ApiError && cause.status === 403 ? 'No tenés permiso para consultar stock.' : 'Stock no disponible.');
    });
    return () => { current = false; };
  }, [storeId, locationId, variantId, access]);
  return <p role="status">{text}</p>;
}
