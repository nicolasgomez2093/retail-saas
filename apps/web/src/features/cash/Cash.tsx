import { useEffect, useRef, useState, type FormEvent, useId } from 'react';
import {
  api, ApiError, type CashRegister, type CashRegisters, type CashSession, type CashMovement,
  type CashMovementInput, type Store,
} from '../../shared/api/client';
import { posPrice } from '../pos/priceDisplay';
import { decimalInput } from '../../shared/lib/decimal';

type Retry = { value: string; key: string };

function registerStatus(register: CashRegister) {
  if (register.openSessionId) return 'Abierta';
  return register.occupied ? 'Ocupada por otra sesión' : 'Cerrada';
}
function message(error: unknown) {
  return error instanceof Error ? error.message : 'No pudimos completar la operación de caja.';
}

function useRegisters(storeId: string, salesRevision: number) {
  const [data, setData] = useState<CashRegisters | null>(null);
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [loadError, setLoadError] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    void api.cashRegisters(storeId).then((registers) => {
      if (!active) return;
      setData(registers);
      setAllowed(true);
      setLoadError('');
    }).catch((cause: unknown) => {
      if (!active) return;
      if (cause instanceof ApiError && cause.status === 403) {
        setAllowed(false);
        setData(null);
      } else {
        setLoadError(message(cause));
      }
    });
    return () => { active = false; };
  }, [storeId, revision, salesRevision]);
  return { data, setData, allowed, loadError, revision, refresh: () => setRevision((value) => value + 1) };
}

export function Cash({ stores, salesRevision, onCashChanged }: { stores: Store[]; salesRevision: number; onCashChanged: () => void }) {
  const [selection, setSelection] = useState('');
  const storeId = stores.some((store) => store.id === selection) ? selection : stores[0]?.id;
  if (!storeId) return null;
  return <StoreCash key={storeId} storeId={storeId} stores={stores} onStore={setSelection} salesRevision={salesRevision} onCashChanged={onCashChanged} />;
}

function StoreCash({ storeId, stores, onStore, salesRevision, onCashChanged }: {
  storeId: string; stores: Store[]; onStore: (id: string) => void; salesRevision: number; onCashChanged: () => void;
}) {
  const instanceId = useId();
  const { data, setData, allowed, loadError, revision, refresh } = useRegisters(storeId, salesRevision);
  const [selection, setSelection] = useState('');
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const register = data?.items.find((item) => item.id === selection) ?? data?.items[0];

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const fields = new FormData(event.currentTarget);
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const item = await api.createCashRegister(storeId, String(fields.get('code')), String(fields.get('name')));
      setData((current) => current && { ...current, items: [...current.items, item].sort((a, b) => a.code.localeCompare(b.code)) });
      setSelection(item.id);
      setCreating(false);
      setNotice('Caja creada.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  if (allowed === false) return <p>No tenés acceso a Caja en esta sucursal.</p>;
  if (!data) return loadError ? <div>
    <p role="alert">{loadError}</p>
    <button type="button" onClick={refresh}>Reintentar</button>
  </div> : <p role="status">Cargando cajas…</p>;
  return (
    <section aria-labelledby={instanceId + "-cash-title"}>
      <h2 id={instanceId + "-cash-title"}>Cajas de la sucursal</h2>
      {stores.length > 1 && <label>Sucursal de caja
        <select name="cashStore" value={storeId} onChange={(event) => onStore(event.target.value)}>
          {stores.map((store) => <option key={store.id} value={store.id}>{store.name} ({store.code})</option>)}
        </select>
      </label>}
      {loadError && <p role="alert">{loadError}</p>}
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <div className="actions cash-controls">
        <button type="button" className="cash-secondary" onClick={() => { refresh(); onCashChanged(); }}>Actualizar cajas</button>
        {data.canManage && !creating && <button type="button" onClick={() => setCreating(true)}>Nueva caja</button>}
      </div>
      {creating && <form aria-label="Nueva caja" onSubmit={(event) => void create(event)}>
        <label>Código<input name="code" required maxLength={40} disabled={busy} /></label>
        <label>Nombre<input name="name" required maxLength={160} disabled={busy} /></label>
        <div className="actions">
          <button disabled={busy}>Guardar caja</button>
          <button type="button" disabled={busy} onClick={() => setCreating(false)}>Cancelar</button>
        </div>
      </form>}
      {!register ? <p>Todavía no hay cajas en esta sucursal.</p> : (
        <div className="cash-workspace">
          <ul className="cash-register-grid" aria-label="Cajas registradoras">
            {data.items.map((item) => <li key={item.id}>
              <button type="button" className="cash-register-card" aria-pressed={register.id === item.id}
                onClick={() => setSelection(item.id)}>
                <span><strong>{item.name}</strong><small>{item.code}</small></span>
                <span className={'cash-state ' + (item.openSessionId ? 'cash-state-open' : item.occupied ? 'cash-state-busy' : '')}>
                  {registerStatus(item)}
                </span>
              </button>
            </li>)}
          </ul>
          <div className="cash-workspace-session"><RegisterSession key={register.id} storeId={storeId} register={register} currency={data.currency}
            timezone={data.timezone} revision={revision + salesRevision} onSession={(session) => {
              setData((current) => current && {
                ...current, items: current.items.map((item) => item.id === session.registerId ? {
                  ...item, occupied: session.status === 'OPEN', openSessionId: session.status === 'OPEN' ? session.id : null,
                } : item),
              });
              onCashChanged();
            }} /></div>
        </div>
      )}
    </section>
  );
}

function RegisterSession({ storeId, register, currency, timezone, revision, onSession }: {
  storeId: string; register: CashRegister; currency: string; timezone: string; revision: number;
  onSession: (session: CashSession) => void;
}) {
  const [items, setItems] = useState<CashSession[]>([]);
  const [selected, setSelected] = useState('');
  const [opening, setOpening] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const retry = useRef<Retry | null>(null);
  const selectedId = selected || register.openSessionId;
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });

  useEffect(() => {
    let active = true;
    void api.cashSessions(storeId, register.id).then((result) => {
      if (active) setItems(result.items);
    }).catch((cause: unknown) => { if (active) setError(message(cause)); });
    return () => { active = false; };
  }, [storeId, register.id, revision]);

  function remember(session: CashSession) {
    setItems((current) => [session, ...current.filter((item) => item.id !== session.id)]
      .sort((a, b) => Date.parse(b.openedAt) - Date.parse(a.openedAt) || b.id.localeCompare(a.id)).slice(0, 50));
  }

  async function open(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const amount = decimalInput(fields.get('openingCash'));
      if (retry.current?.value !== amount) retry.current = { value: amount, key: crypto.randomUUID() };
      const session = await api.openCash(storeId, register.id, amount, retry.current.key);
      retry.current = null;
      remember(session);
      setSelected(session.id);
      onSession(session);
      setOpening(false);
      setNotice(session.status === 'OPEN' ? 'Caja abierta.' : 'Apertura recuperada del historial.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="cash-session-heading">
        <h3>{register.name}</h3>
        {items.length > 0 && <label>Sesiones recientes
          <select name="cashSession" value={selectedId ?? ''} onChange={(event) => setSelected(event.target.value)}>
            <option value="">Elegí una sesión</option>
            {items.map((item) => <option key={item.id} value={item.id}>{dates.format(new Date(item.openedAt))}
              {' · '}{item.status === 'OPEN' ? 'Abierta' : 'Cerrada'}</option>)}
          </select>
        </label>}
      </div>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {register.occupied && !register.openSessionId && <p>Caja ocupada por otra sesión.</p>}
      {!register.occupied && !opening && <button type="button" onClick={() => setOpening(true)}>Abrir caja</button>}
      {opening && !register.occupied && <form aria-label="Abrir caja" onSubmit={(event) => void open(event)}>
        <label>Saldo inicial en {currency}<input name="openingCash" inputMode="decimal" maxLength={19} required disabled={busy} /></label>
        <div className="actions">
          <button disabled={busy}>Confirmar apertura</button>
          <button type="button" disabled={busy} onClick={() => setOpening(false)}>Cancelar</button>
        </div>
      </form>}
      {selectedId && <SessionDetail key={selectedId} id={selectedId} storeId={storeId} currency={currency}
        timezone={timezone} revision={revision} onClosed={(session) => {
          remember(session);
          setSelected(session.id);
          onSession(session);
        }} />}
    </>
  );
}

function SessionDetail({ id, storeId, currency, timezone, revision, onClosed }: {
  id: string; storeId: string; currency: string; timezone: string; revision: number; onClosed: (session: CashSession) => void;
}) {
  const [session, setSession] = useState<CashSession | null>(null);
  const [movements, setMovements] = useState<CashMovement[]>([]);
  const [movementTotals, setMovementTotals] = useState<{ income?: string; outflow?: string; count?: number }>({});
  const [refresh, setRefresh] = useState(0);
  const [busy, setBusy] = useState(false);
  const [closing, setClosing] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const retry = useRef<Retry | null>(null);
  const dates = new Intl.DateTimeFormat('es-AR', { dateStyle: 'short', timeStyle: 'short', timeZone: timezone });
  const display = (value: string) => currency + '\u00a0' + posPrice(value);

  useEffect(() => {
    let active = true;
    void Promise.all([api.cashSession(storeId, id), api.cashMovements(storeId, id)]).then(([info, history]) => {
      if (!active) return;
      setSession(info);
      setMovements(history.items);
      setMovementTotals({ income: history.income, outflow: history.outflow, count: history.count });
    }).catch((cause: unknown) => { if (active) setError(message(cause)); });
    return () => { active = false; };
  }, [storeId, id, refresh, revision]);

  async function perform(operation: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await operation();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  function move(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const fields = new FormData(form);
    void perform(async () => {
      const input: CashMovementInput = { storeId, amount: decimalInput(fields.get('amount')),
        reason: String(fields.get('reason')).trim(), type: fields.get('type') === 'manual_out' ? 'manual_out' : 'manual_in' };
      if (input.amount === '0.00') throw new Error('Ingresá un importe mayor a cero.');
      const value = JSON.stringify(input);
      if (retry.current?.value !== value) retry.current = { value, key: crypto.randomUUID() };
      await api.addCashMovement(id, input, retry.current.key);
      retry.current = null;
      form.reset();
      setNotice('Movimiento registrado.');
      setRefresh((current) => current + 1);
    });
  }

  function close(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    void perform(async () => {
      const result = await api.closeCash(storeId, id, decimalInput(fields.get('declaredCash')));
      setSession(result);
      setClosing(false);
      setNotice('Caja cerrada.');
      onClosed(result);
      setRefresh((current) => current + 1);
    });
  }

  return (
    <section className="cash-session" aria-label="Detalle de sesión de caja" aria-busy={busy || (!session && !error)}>
      {!session && !error && <p role="status">Cargando sesión…</p>}
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {session && (
        <>
          <h4>{session.status === 'OPEN' ? 'Sesión abierta' : 'Sesión cerrada'}</h4>
          <dl className="cash-session-totals">
            <div><dt>Saldo inicial</dt><dd>{display(session.openingCash)}</dd></div>
            {movementTotals.income !== undefined && <div><dt>Ingresos en efectivo</dt><dd>{display(movementTotals.income)}</dd></div>}
            {movementTotals.outflow !== undefined && <div><dt>Egresos en efectivo</dt><dd>{display(movementTotals.outflow)}</dd></div>}
            <div><dt>Efectivo esperado</dt><dd>{display(session.expectedCash)}</dd></div>
            {session.declaredCash !== null && <div><dt>Efectivo contado</dt><dd>{display(session.declaredCash)}</dd></div>}
            {session.difference !== null && <div><dt>Diferencia</dt><dd>{display(session.difference)}</dd></div>}
          </dl>
          <div className="commercial-cash-columns">
            <section className="commercial-cash-movements">
          <h4>Movimientos de efectivo</h4>
          {(movementTotals.count ?? 0) > movements.length && <p>Se muestran los últimos {movements.length} de {movementTotals.count} movimientos.</p>}
          {movements.length === 0 ? <p>Todavía no hay movimientos para esta sesión.</p> : <ul className="cash-movements">
            {movements.map((item) => <li key={item.id}>
              <span>{item.reason}<small>{dates.format(new Date(item.createdAt))}</small></span>
              <strong>{display(item.amount)}</strong>
            </li>)}
          </ul>}
            </section>
          {session.status === 'OPEN' && (
            <div className="cash-operation-stack">
              <details className="cash-movement-form">
                <summary>Registrar ingreso o egreso</summary>
                <form aria-label="Movimiento manual de caja" onSubmit={move}>
                  <label>Tipo<select name="type" disabled={busy}><option value="manual_in">Ingreso</option><option value="manual_out">Egreso</option></select></label>
                  <label>Importe en {currency}<input name="amount" inputMode="decimal" maxLength={19} required disabled={busy} /></label>
                  <label>Motivo<input name="reason" maxLength={300} required disabled={busy} /></label>
                  <button disabled={busy}>Registrar movimiento</button>
                </form>
              </details>
              {!closing ? <button type="button" className="cash-controls" disabled={busy} onClick={() => setClosing(true)}>Hacer arqueo y cerrar</button>
                : <form aria-label="Cerrar caja" className="cash-controls" onSubmit={close}>
                  <h4>Arqueo de caja</h4>
                  <label>Efectivo contado en {currency}<input name="declaredCash" inputMode="decimal" maxLength={19} required disabled={busy} /></label>
                  <div className="actions">
                    <button disabled={busy}>Confirmar cierre</button>
                    <button type="button" disabled={busy} onClick={() => setClosing(false)}>Volver</button>
                  </div>
                </form>}
            </div>
          )}

          </div>
        </>
      )}
    </section>
  );
}
