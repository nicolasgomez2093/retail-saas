import { useState, type FormEvent } from 'react';
import { api, ApiError } from '../../shared/api/client';

const uncertainImport = 'No pudimos confirmar el resultado de la importación. El servidor podría haber procesado el archivo. Buscá los productos por nombre o SKU en el listado del catálogo antes de volver a intentar.';

export function CatalogCSV({ onImported }: { onImported: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  async function perform(operation: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await operation();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos completar la operación.');
    } finally {
      setBusy(false);
    }
  }

  function download(kind: 'template' | 'export') {
    void perform(async () => {
      const data = await api.downloadCatalogCSV(kind);
      const url = URL.createObjectURL(data);
      const link = document.createElement('a');
      link.href = url;
      link.download = kind === 'template' ? 'plantilla-catalogo.csv' : 'catalogo.csv';
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    });
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const file = new FormData(form).get('csv');
    if (!(file instanceof File) || file.size === 0) {
      setError('Seleccioná un archivo CSV con datos.');
      return;
    }
    if (file.size > 2 * 1024 * 1024) {
      setError('El archivo debe tener hasta 2 MB.');
      return;
    }
    void perform(async () => {
      let result: Awaited<ReturnType<typeof api.importCatalogCSV>>;
      try {
        result = await api.importCatalogCSV(file);
      } catch (cause) {
        if (cause instanceof ApiError && cause.status >= 400 && cause.status < 500 && cause.status !== 408) {
          throw cause;
        }
        setError(uncertainImport);
        return;
      }
      if (!result || !Number.isInteger(result.products) || result.products < 0 ||
          !Number.isInteger(result.variants) || result.variants < 0) {
        setError(uncertainImport);
        return;
      }
      form.reset();
      setNotice(`Altas confirmadas. Productos: ${result.products}. Variantes: ${result.variants}.`);
      try {
        await onImported();
      } catch {
        setError('El archivo se importó. Volvé a buscar para actualizar el listado.');
      }
    });
  }

  return (
    <section className="catalog-csv-tools" aria-label="Importar y exportar CSV">
      <h3>CSV</h3>
      <div className="actions">
        <button type="button" disabled={busy} onClick={() => download('template')}>Descargar plantilla</button>
        <button type="button" disabled={busy} onClick={() => download('export')}>Exportar catálogo</button>
      </div>
      <p>La carga crea productos y variantes. Podés importar hasta 1000 filas por archivo. Si alguna fila falla, se cancela toda la carga.</p>
      <form aria-label="Importar catálogo CSV" onSubmit={submit}>
        <label>Archivo CSV<input name="csv" type="file" accept=".csv,text/csv" disabled={busy} required /></label>
        <button disabled={busy}>{busy ? 'Procesando…' : 'Importar catálogo'}</button>
      </form>
      {notice && <p role="status">{notice}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
