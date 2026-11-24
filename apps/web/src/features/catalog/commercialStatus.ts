/** A presentation of the last confirmed readings, never a checkout authorization. */
export function commercialStatus({ active, price, stock, error = false }: {
  active: boolean; price: string | null | undefined; stock: number | null | undefined; error?: boolean;
}) {
  if (!active) return { label: 'Inactiva', tone: 'inactive', ready: false };
  if (error || price === undefined || stock === undefined) return { label: 'No verificada', tone: 'unknown', ready: false };
  if (price === null) return { label: 'Sin precio', tone: 'warning', ready: false };
  if (stock === null) return { label: 'Sin saldo registrado', tone: 'unknown', ready: false };
  if (stock <= 0) return { label: 'Sin stock', tone: 'warning', ready: false };
  return { label: 'Lista para vender', tone: 'ready', ready: true };
}
