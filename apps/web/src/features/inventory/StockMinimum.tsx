import { useRef, useState, type FormEvent } from 'react';
import { ApiError, api } from '../../shared/api/client';

const maxUnits = 2147483647;

export function StockMinimum({ storeId, locationId, variantId, minimum, version, disabled, onSaved, onRefresh, onBusy }: {
  storeId: string; locationId: string; variantId: string; minimum: number | null; version: string | null;
  disabled: boolean; onSaved: () => void; onRefresh: () => void; onBusy: (busy: boolean) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [stale, setStale] = useState(false);
  const sending = useRef(false);
  const conflictVersion = useRef<string | null | undefined>(undefined);
  const reviewed = !stale || version !== conflictVersion.current;

  async function save(value: number | null) {
    if (disabled || !reviewed || sending.current) return;
    sending.current = true;
    onBusy(true);
    setError('');
    setNotice('');
    setStale(false);
    try {
      await api.setStockMinimum(storeId, locationId, variantId, value, version ?? 'unrecorded');
      setNotice(value === null ? 'Mínimo quitado.' : 'Mínimo guardado.');
      setDraft(value === null ? '' : String(value));
      onSaved();
    } catch (cause) {
      const code = cause instanceof ApiError ? cause.code : undefined;
      const status = cause instanceof ApiError ? cause.status : undefined;
      if (code === 'minimum_stale' || status === undefined || status === 0 || status >= 500) {
        conflictVersion.current = version;
        setStale(true);
        setError(code === 'minimum_stale'
          ? 'Otra operación cambió el mínimo. Consultá el valor vigente y revisá tu propuesta.'
          : 'No se pudo confirmar el cambio. Consultá el mínimo actual antes de volver a guardar.');
      } else setError(status === 401 ? 'La sesión venció.' : status === 403 ? 'Ya no tenés permiso para modificar el mínimo.' :
        'No pudimos guardar el mínimo. Revisá la ubicación, la variante y el valor.');
    } finally {
      sending.current = false;
      onBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const raw = (draft ?? String(minimum ?? '')).trim();
    const units = Number(raw);
    if (!/^\d+$/.test(raw) || units > maxUnits) {
      setError('Ingresá un mínimo entero entre 0 y 2.147.483.647.');
      return;
    }
    void save(units);
  }

  return <form className="adjustment-form" aria-label="Configurar stock mínimo" noValidate onSubmit={submit}>
    <label>Mínimo en unidades
      <input name="minimum" inputMode="numeric" value={draft ?? minimum ?? ''} onChange={event => { setDraft(event.target.value); setNotice(''); }}
        placeholder="Sin mínimo" disabled={disabled} aria-describedby={error ? 'minimum-error' : undefined} />
    </label>
    {notice && <p className="adjustment-notice" role="status">{notice}</p>}
    {error && <p id="minimum-error" className="adjustment-notice" role="alert">{error}</p>}
    <div className="adjustment-actions">
      <button disabled={disabled || !reviewed}>Guardar mínimo</button>
      {minimum !== null && <button className="button-secondary" type="button" disabled={disabled || !reviewed} onClick={() => void save(null)}>Quitar mínimo</button>}
      {stale && <button className="button-secondary" type="button" disabled={disabled} onClick={() => {
        onRefresh();
        setStale(false);
        setError('Revisá el valor actualizado antes de volver a guardar.');
      }}>Consultar mínimo actual</button>}
    </div>
  </form>;
}
