export function sellableAvailability(
  locations: readonly { id: string; isSellable: boolean }[],
  balances: readonly { locationId: string; variantId: string; available: number; active: boolean }[],
) {
  const sellable = new Set(locations.filter((location) => location.isSellable).map((location) => location.id));
  const totals = new Map<string, number>();
  for (const balance of balances) {
    if (!balance.active || !sellable.has(balance.locationId)) continue;
    totals.set(balance.variantId, (totals.get(balance.variantId) ?? 0) + balance.available);
  }
  return totals;
}
