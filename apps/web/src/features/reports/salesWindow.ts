import { api } from '../../shared/api/client';

export function dateKey(date: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
  }).formatToParts(date);
  return ['year', 'month', 'day'].map((type) => parts.find((part) => part.type === type)?.value).join('-');
}

export function shiftDay(day: string, offset: number) {
  const date = new Date(day + 'T00:00:00Z');
  date.setUTCDate(date.getUTCDate() + offset);
  return date.toISOString().slice(0, 10);
}

export async function loadSalesWindow(storeId: string, days: number) {
  const now = new Date();
  const utcDay = dateKey(now, 'UTC');
  const report = await api.salesMarginReport(storeId, 1, 0, {
    fromDate: shiftDay(utcDay, -days), toDate: shiftDay(utcDay, 1), query: '',
  });
  return { report, today: dateKey(now, report.timezone) };
}
