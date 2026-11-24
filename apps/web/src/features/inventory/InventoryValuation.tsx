import { useEffect, useState, useId } from 'react';
import { api, type InventoryValuation as Valuation } from '../../shared/api/client';

function displayValue(currency: string, amount: string | null) {
  return amount === null ? 'No disponible' : `${currency} ${amount.replace('.', ',')}`;
}

export function InventoryValuation({ storeId, revision }: { storeId: string; revision: number }) {
  const instanceId = useId();
  const [result, setResult] = useState<{ key: string; valuation?: Valuation; error?: string } | null>(null);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const key = `${storeId}:${revision}`;
  const current = result?.key === key ? result : null;

  useEffect(() => {
    let active = true;
    void api.inventoryValuation(storeId).then((valuation) => {
      if (active) setResult({ key, valuation });
    }).catch((cause: unknown) => {
      if (active) setResult({ key, error: cause instanceof Error ? cause.message : 'No pudimos consultar la valuación.' });
    });
    return () => { active = false; };
  }, [key, storeId]);

  async function downloadCSV() {
    setExporting(true);
    setExportError('');
    try {
      const file = await api.downloadInventoryValuationCSV(storeId);
      const url = URL.createObjectURL(file);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'valuacion-inventario.csv';
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : 'No pudimos descargar la valuación.');
    } finally {
      setExporting(false);
    }
  }

  const value = current?.valuation;
  return <section aria-labelledby={instanceId + "-inventory-valuation-title"}>
    <h3 id={instanceId + "-inventory-valuation-title"}>Valuación de la sucursal</h3>
    {!current && <p role="status">Consultando valuación…</p>}
    {current?.error && <p role="alert">{current.error}</p>}
    {value && <>
      <p>Stock físico de todas las ubicaciones activas, incluidas las unidades reservadas.</p>
      <button type="button" disabled={exporting} onClick={() => void downloadCSV()}>
        {exporting ? 'Preparando CSV…' : 'Descargar valuación CSV'}
      </button>
      {exportError && <p role="alert">{exportError}</p>}
      <dl>
        <dt>Unidades con costo</dt><dd>{value.unitsWithCost.toLocaleString('es-AR')}</dd>
        <dt>Unidades sin costo</dt><dd>{value.unitsWithoutCost.toLocaleString('es-AR')}</dd>
        {value.unitsWithoutCost > 0 && value.knownValue !== null && <>
          <dt>Valor de las unidades con costo</dt><dd>{displayValue(value.currency, value.knownValue)}</dd>
        </>}
        <dt>Valuación total</dt><dd>{value.totalValue === null ? 'No disponible por falta de costos' : displayValue(value.currency, value.totalValue)}</dd>
      </dl>
      {value.locations.length > 0 && <div className="inventory-table">
        <table>
          <caption>Valuación por ubicación</caption>
          <thead><tr>
            <th scope="col">Ubicación</th><th scope="col">Unidades con costo</th><th scope="col">Unidades sin costo</th>
            <th scope="col">Valor conocido</th><th scope="col">Valuación total</th>
          </tr></thead>
          <tbody>{value.locations.map((location) => <tr key={location.locationId}>
            <th scope="row">{location.name} ({location.code})</th>
            <td>{location.unitsWithCost.toLocaleString('es-AR')}</td>
            <td>{location.unitsWithoutCost.toLocaleString('es-AR')}</td>
            <td>{displayValue(value.currency, location.knownValue)}</td>
            <td>{displayValue(value.currency, location.totalValue)}</td>
          </tr>)}</tbody>
        </table>
      </div>}
    </>}
  </section>;
}
