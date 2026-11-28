import { useEffect, useState } from 'react';
import { api, ApiError } from '../../shared/api/client';

/** Commerce-wide label choice: with or without price. Whoever prints sees it; whoever manages the catalog changes it. */
export function LabelSettings() {
  const [showPrice, setShowPrice] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    let current = true;
    void api.labelSettings().then((data) => { if (current) setShowPrice(data.showPrice); })
      .catch(() => { if (current) setError('No pudimos leer el ajuste de etiquetas.'); });
    return () => { current = false; };
  }, []);

  async function change(next: boolean) {
    if (busy) return;
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const saved = await api.saveLabelSettings({ showPrice: next });
      setShowPrice(saved.showPrice);
      setMessage(saved.showPrice ? 'Las etiquetas van a salir con precio.' : 'Las etiquetas van a salir sin precio.');
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 403 ? 'Solo quien administra el catálogo puede cambiar este ajuste.'
        : cause instanceof Error ? cause.message : 'No pudimos guardar el ajuste.');
    } finally {
      setBusy(false);
    }
  }

  return <section className="label-settings-card" aria-labelledby="label-settings-title">
    <h3 id="label-settings-title">Etiquetas</h3>
    <label className="label-settings-switch">
      <input type="checkbox" role="switch" checked={showPrice ?? true} disabled={showPrice === null || busy}
        onChange={(event) => void change(event.target.checked)} />
      <span><b>Imprimir el precio en las etiquetas</b>
        <small>Si lo apagás, las etiquetas salen solo con nombre, talle, color y código de barras. Útil si cambiás precios seguido.</small></span>
    </label>
    {message && <p role="status">{message}</p>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
