import { useEffect, useId, useRef, useState } from 'react';

import { api, ApiError, type ExistenceAccess, type ExistencePage, type ExistenceRow } from '../../shared/api/client';
import { decimalText, minorUnits } from '../../shared/lib/decimal';
import { formatAmount, parsePrice } from '../catalog/quickProduct';
import { Modal } from '../purchases/PurchaseParts';

export const moneyOf = (currency: string, canonical: string) => currency + ' ' + formatAmount(canonical);
const units = (value: number) => value.toLocaleString('es-AR') + (value === 1 ? ' unidad' : ' unidades');

/**
 * What the stock of the filtered results is worth: at cost (only for people
 * who can see costs) and at the branch selling price. Units without a cost or
 * price are counted apart instead of silently valued at zero.
 */
export function ValueSummary({ summary, access }: { summary: ExistencePage['summary']; access: ExistenceAccess }) {
  const currency = summary.currency ?? 'ARS';
  const total = summary.units ?? 0;
  const withoutCost = summary.unitsWithoutCost ?? 0;
  const withoutPrice = summary.unitsWithoutPrice ?? 0;
  const cost = summary.costValue ?? null;
  const retail = summary.retailValue ?? '0.00';
  const margin = access.canSeeCosts && cost !== null && withoutCost === 0 && withoutPrice === 0 && minorUnits(retail) > 0n
    ? minorUnits(retail) - minorUnits(cost) : null;
  return <div className="existences-values" aria-label="Valor del stock de los resultados filtrados">
    {access.canSeeCosts && cost !== null && <div className="existences-value" data-incomplete={withoutCost > 0 || undefined}>
      <span>Costo del inventario</span>
      <strong>{moneyOf(currency, cost)}</strong>
      <small>{total === 0 ? 'Sin unidades en stock' : withoutCost === 0 ? units(total) + ' al costo promedio'
        : withoutCost === total ? 'Ninguna unidad tiene costo cargado todavía' : units(withoutCost) + ' sin costo no suman'}</small>
      {withoutCost > 0 && access.canSetCosts && <small className="existences-value-hint">Cargalo con «Cargar costo» en cada producto.</small>}
    </div>}
    <div className="existences-value">
      <span>Valor a precio de venta</span>
      <strong>{moneyOf(currency, retail)}</strong>
      <small>{total === 0 ? 'Sin unidades en stock' : withoutPrice === 0 ? units(total) + ' a precio de lista' : units(withoutPrice) + ' sin precio no suman'}</small>
    </div>
    {access.canSeeCosts && <div className="existences-value">
      <span>Ganancia potencial</span>
      <strong>{margin === null ? '—' : moneyOf(currency, decimalText(margin))}</strong>
      <small>{margin === null ? 'Se calcula cuando todas las unidades tienen costo y precio'
        : 'Si se vende todo a precio de lista · ' + Math.round(Number(margin * 1000n / minorUnits(retail)) / 10) + ' % del precio'}</small>
    </div>}
  </div>;
}

export function CostCell({ row, currency, canSet, onSet }: {
  row: ExistenceRow; currency: string; canSet: boolean; onSet: (opener: HTMLButtonElement) => void;
}) {
  const stock = (row.onHand ?? 0) > 0;
  if (row.unitCost) return <>
    {moneyOf(currency, row.unitCost)}
    {stock && row.costValue && <small>Total {moneyOf(currency, row.costValue)}</small>}
    {canSet && stock && <button type="button" className="existences-cost-action" onClick={(event) => onSet(event.currentTarget)}
      aria-label={'Cambiar costo de ' + row.sku}>Cambiar</button>}
  </>;
  if (!stock) return <span className="existences-muted">—</span>;
  return <>
    <span className="existences-muted">Sin costo</span>
    {canSet && <button type="button" className="existences-cost-action" onClick={(event) => onSet(event.currentTarget)}
      aria-label={'Cargar costo de ' + row.sku}>Cargar</button>}
  </>;
}

/**
 * Loads the unit cost of the units each variant has today in the branch.
 * Only variants with stock are listed; the next purchases keep averaging.
 */
export function CostDialog({ storeId, currency, title, rows, returnTo, onClose, onSaved }: {
  storeId: string; currency: string; title: string; rows: ExistenceRow[]; returnTo: HTMLElement | null;
  onClose: () => void; onSaved: (message: string) => void;
}) {
  const uid = useId();
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(rows.map((row) => [row.variantId!, row.unitCost ? formatAmount(row.unitCost) : ''])));
  const [all, setAll] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const first = useRef<HTMLInputElement>(null);
  useEffect(() => { first.current?.focus(); }, []);

  const parsed = rows.map((row) => ({ row, value: parsePrice(values[row.variantId!] ?? '') }));
  const invalid = parsed.filter((entry) => entry.value === undefined);
  const filled = parsed.filter((entry): entry is { row: ExistenceRow; value: string } => typeof entry.value === 'string');
  const changed = filled.filter((entry) => entry.value !== entry.row.unitCost);
  const total = filled.reduce((sum, entry) => sum + minorUnits(entry.value) * BigInt(entry.row.onHand ?? 0), 0n);
  const allValue = parsePrice(all);

  async function submit() {
    if (busy) return;
    if (invalid.length) { setError('Revisá los costos marcados: usá números, por ejemplo 9500 o 9.500,50.'); return; }
    if (!changed.length) { setError('Completá el costo de al menos una variante.'); return; }
    setBusy(true);
    setError('');
    try {
      await api.setInventoryCosts(storeId, changed.map((entry) => ({ variantId: entry.row.variantId!, unitCost: entry.value })), reason.trim());
      onSaved(changed.length === 1 ? 'Costo cargado para ' + changed[0].row.sku + '.' : 'Costo cargado para ' + changed.length + ' variantes.');
    } catch (cause) {
      setError(cause instanceof ApiError && cause.code === 'inventory_cost_without_stock'
        ? 'Una de las variantes ya no tiene unidades en stock. Actualizá Existencias y volvé a intentar.'
        : cause instanceof ApiError && cause.status === 403 ? 'Tu usuario no puede cargar costos. Pedíselo a quien administra el comercio.'
          : cause instanceof Error ? cause.message : 'No pudimos guardar los costos.');
    } finally {
      setBusy(false);
    }
  }

  return <Modal wide title={title} returnTo={returnTo} onClose={onClose} busy={busy}
    footer={<>
      <span className="existences-cost-total" aria-live="polite">{filled.length ? 'Stock valuado: ' + moneyOf(currency, decimalText(total)) : ''}</span>
      <button type="button" className="pu-btn" disabled={busy} onClick={onClose}>Cancelar</button>
      <button type="button" className="pu-btn primary" disabled={busy} onClick={() => void submit()}>{busy ? 'Guardando…' : 'Guardar costos'}</button>
    </>}>
    <p className="pu-hint">El costo se aplica a las unidades que hay hoy en la sucursal. Las próximas compras lo promedian con lo que llegue.</p>
    {rows.length > 1 && <div className="existences-cost-all">
      <label className="pu-label">Mismo costo para todas
        <input value={all} inputMode="decimal" disabled={busy} placeholder="Ej. 9.500" onChange={(event) => setAll(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter' && typeof allValue === 'string') { event.preventDefault(); setValues(Object.fromEntries(rows.map((row) => [row.variantId!, all]))); } }} />
      </label>
      <button type="button" className="pu-btn" disabled={busy || typeof allValue !== 'string'}
        onClick={() => setValues(Object.fromEntries(rows.map((row) => [row.variantId!, all])))}>Aplicar a todas</button>
    </div>}
    <table className="existences-cost-table">
      <caption className="pos-sr-only">Costo unitario por variante</caption>
      <thead><tr><th scope="col">Variante</th><th scope="col">En stock</th><th scope="col">Costo unitario</th></tr></thead>
      <tbody>{parsed.map(({ row, value }, index) => <tr key={row.variantId}>
        <th scope="row"><b>{row.product}</b><small>{[row.size && 'Talle ' + row.size, row.color && 'Color ' + row.color, row.sku].filter(Boolean).join(' · ')}</small></th>
        <td>{(row.onHand ?? 0).toLocaleString('es-AR')} u.</td>
        <td>
          <span className="pu-pay-amount-field" data-invalid={value === undefined || undefined}><span aria-hidden="true">{currency}</span>
            <input ref={index === 0 ? first : undefined} id={uid + row.variantId} value={values[row.variantId!] ?? ''} inputMode="decimal" autoComplete="off"
              disabled={busy} aria-label={'Costo unitario de ' + row.sku} aria-invalid={value === undefined || undefined} placeholder="Sin costo"
              onChange={(event) => { setValues((current) => ({ ...current, [row.variantId!]: event.target.value })); setError(''); }}
              onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void submit(); } }} /></span>
          {typeof value === 'string' && <small>Total {moneyOf(currency, decimalText(minorUnits(value) * BigInt(row.onHand ?? 0)))}</small>}
        </td>
      </tr>)}</tbody>
    </table>
    <label className="pu-label existences-cost-reason">Motivo (opcional)
      <input value={reason} maxLength={300} disabled={busy} placeholder="Ej. costo de la factura del proveedor" onChange={(event) => setReason(event.target.value)} />
    </label>
    {error && <p className="pu-alert" role="alert">{error}</p>}
  </Modal>;
}
