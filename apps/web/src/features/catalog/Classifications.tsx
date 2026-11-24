import { useState, type FormEvent } from 'react';
import { api, type ClassificationKind, type Classifications, type ClassificationIDs, type Term } from '../../shared/api/client';

export type ClassificationReadStates = Record<ClassificationKind, 'loading' | 'ready' | 'error' | 'denied'>;
export function ClassificationFields({ values, current, filtering = false, states, onRetry }: {
  values: Classifications;
  current?: ClassificationIDs;
  filtering?: boolean;
  states?: ClassificationReadStates;
  onRetry?: (kind: ClassificationKind, button: HTMLButtonElement) => void;
}) {
  return <>{(['categories', 'brands'] as const).map(kind => {
    const field = kind === 'categories' ? 'categoryId' : 'brandId';
    const label = kind === 'categories' ? 'Categoría' : 'Marca';
    const plural = kind === 'categories' ? 'categorías' : 'marcas';
    const state = states?.[kind] ?? 'ready';
    const unavailable = state === 'error' || state === 'denied';
    const empty = state === 'loading' ? 'Consultando ' + plural + '…' : unavailable ? label + ' no disponible'
      : !values[kind].length ? 'Sin ' + plural : filtering ? 'Todas' : 'Sin ' + label.toLowerCase();
    return <label key={kind}>{label}
      <select name={field} defaultValue={current?.[field] ?? ''}>
        <option value="">{empty}</option>
        {values[kind].map(term => <option key={term.id} value={term.id}>{term.name}</option>)}
      </select>
      {unavailable && <span className="auxiliary-status"><span>{state === 'denied' ? 'No tenés permiso para consultar ' + plural + '.' : 'Consulta no disponible. Podés continuar sin cambiar este dato.'}</span>
        {state === 'error' && onRetry && <button type="button" onClick={event => onRetry(kind,event.currentTarget)}>Reintentar {plural}</button>}</span>}
    </label>;
  })}</>;
}

export function ClassificationSummary({ values, product }: { values: Classifications; product: ClassificationIDs }) {
  const category = values.categories.find((term) => term.id === product.categoryId);
  const brand = values.brands.find((term) => term.id === product.brandId);
  if (!category && !brand) return null;
  return <p>{[category?.name, brand?.name].filter(Boolean).join(' · ')}</p>;
}

const labels = {
  categories: { heading: 'Categorías', create: 'Nueva categoría', edit: 'Editar categoría', saved: 'Categoría guardada.' },
  brands: { heading: 'Marcas', create: 'Nueva marca', edit: 'Editar marca', saved: 'Marca guardada.' },
};

export function ClassificationEditor({ kind, values, onSaved }: {
  kind: ClassificationKind;
  values: Term[];
  onSaved: (kind: ClassificationKind, term: Term) => void;
}) {
  const [selected, setSelected] = useState<Term | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const label = labels[kind];

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const name = String(new FormData(form).get('name')).trim();
    setMessage('');
    setError('');
    setBusy(true);
    try {
      const term = await api.saveClassification(kind, name, selected?.id);
      onSaved(kind, term);
      form.reset();
      setSelected(null);
      setMessage(label.saved);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'No pudimos guardar el cambio.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-label={label.heading}>
      <h4>{label.heading}</h4>
      {message && <p role="status">{message}</p>}
      {error && <p role="alert">{error}</p>}
      <form key={selected?.id ?? 'new'} aria-label={selected ? label.edit : label.create} onSubmit={(event) => void save(event)}>
        <label>Nombre<input name="name" maxLength={120} defaultValue={selected?.name} required disabled={busy} /></label>
        <div className="actions">
          <button disabled={busy}>{selected ? 'Guardar cambio' : label.create}</button>
          {selected && <button type="button" disabled={busy} onClick={() => { setSelected(null); setError(''); }}>Cancelar</button>}
        </div>
      </form>
      {values.length > 0 && <ul className="catalog-items">{values.map((term) => (
        <li key={term.id}>
          <button type="button" disabled={busy} className="catalog-link" onClick={() => {
            setSelected(term);
            setMessage('');
            setError('');
          }}>
            Editar {term.name}
          </button>
        </li>
      ))}</ul>}
    </section>
  );
}
