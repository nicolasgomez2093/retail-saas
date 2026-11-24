import { useId, useState } from 'react';
import { shiftDay } from './salesWindow';

export type DayValue = { date: string; value: number; label: string; detail?: string };

function dayLabel(day: string) {
  const [, month, date] = day.split('-');
  return date + '/' + month;
}

function weekday(day: string) {
  return new Date(day + 'T12:00:00Z').toLocaleDateString('es-AR', { weekday: 'short', timeZone: 'UTC' }).replace('.', '');
}

/** Columns per day. Ranges up to 31 days show every day, empty ones at zero; longer ranges show the days with data. */
export function DailyColumns({ days, fromDate, toDate, title, emptyText }: {
  days: DayValue[]; fromDate: string; toDate: string; title: string; emptyText: string;
}) {
  const uid = useId();
  const [active, setActive] = useState<number | null>(null);
  const span = fromDate && toDate ? Math.round((Date.parse(toDate) - Date.parse(fromDate)) / 86400000) + 1 : 0;
  const byDay = new Map(days.map((day) => [day.date, day]));
  const series = span > 0 && span <= 31
    ? Array.from({ length: span }, (_, index) => {
      const date = shiftDay(fromDate, index);
      return byDay.get(date) ?? { date, value: 0, label: '—' };
    })
    : [...days].sort((a, b) => a.date.localeCompare(b.date));
  const max = Math.max(0, ...series.map((day) => day.value));
  if (!series.length || max <= 0) return <p className="rc-empty">{emptyText}</p>;
  const ticks = series.length <= 7 ? series.map((_, index) => index)
    : [0, Math.floor((series.length - 1) / 2), series.length - 1];
  const shown = active === null ? null : series[active];
  return <figure className="rc-columns" aria-labelledby={uid}>
    <figcaption id={uid} className="sr-only">{title}</figcaption>
    <div className="rc-plot" onPointerLeave={() => setActive(null)}>
      <ol className="rc-bars" style={{ ['--rc-count' as string]: series.length }}>
        {series.map((day, index) => <li key={day.date}>
          <button type="button" className="rc-hit" aria-label={`${dayLabel(day.date)}: ${day.label}${day.detail ? ', ' + day.detail : ''}`}
            onPointerEnter={() => setActive(index)} onFocus={() => setActive(index)} onBlur={() => setActive(null)}
            data-active={active === index || undefined}>
            <span className="rc-bar" style={{ height: day.value > 0 ? Math.max(2, (day.value / max) * 100) + '%' : 0 }} />
          </button>
        </li>)}
      </ol>
      {shown && active !== null && <div className="rc-tip" role="status"
        style={{ left: `calc(${((active + 0.5) / series.length) * 100}% )` }}
        data-edge={active < series.length / 4 ? 'start' : active > (series.length * 3) / 4 ? 'end' : undefined}>
        <b>{shown.label}</b><span>{weekday(shown.date)} {dayLabel(shown.date)}{shown.detail ? ' · ' + shown.detail : ''}</span>
      </div>}
    </div>
    <div className="rc-axis" aria-hidden="true" style={{ ['--rc-count' as string]: series.length }}>
      {ticks.map((index) => <span key={index} style={{ gridColumn: index + 1 }}>{dayLabel(series[index].date)}</span>)}
    </div>
  </figure>;
}

export type BarRow = { key: string; label: string; value: number; display: string; sub?: string; href?: string };

/** Horizontal bars for a breakdown; each row keeps its own text, so the bar is never the only carrier. */
export function BarList({ rows, emptyText, label }: { rows: BarRow[]; emptyText: string; label: string }) {
  const max = Math.max(0, ...rows.map((row) => row.value));
  if (!rows.length) return <p className="rc-empty">{emptyText}</p>;
  return <ul className="rc-list" aria-label={label}>
    {rows.map((row) => <li key={row.key}>
      <div className="rc-row">
        <span className="rc-name">{row.href ? <a href={row.href}>{row.label}</a> : row.label}{row.sub && <small>{row.sub}</small>}</span>
        <b>{row.display}</b>
      </div>
      <span className="rc-track" aria-hidden="true"><span style={{ width: max > 0 && row.value > 0 ? Math.max(1.5, (row.value / max) * 100) + '%' : 0 }} /></span>
    </li>)}
  </ul>;
}
