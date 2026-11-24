import type { QuickProductResult } from '../../shared/api/client';

// A purchase in progress can ask the existing quick product creation for a
// new product and receive it back, instead of keeping a second creation form
// inside Compras. The purchase screen stays mounted (hidden) while the person
// is in Productos, so its draft survives the round trip.
type Listener = (result: QuickProductResult) => void;
const listeners = new Set<Listener>();
const watchers = new Set<() => void>();
let requested = false;
let version = 0;

function changed() {
  version++;
  for (const watcher of watchers) watcher();
}

export const productHandoff = {
  /** Called by the purchase screen right before opening the quick creation. */
  request() { requested = true; changed(); },
  cancel() { if (requested) { requested = false; changed(); } },
  /** True while a purchase is waiting for the product being created. */
  active() { return requested && listeners.size > 0; },
  deliver(result: QuickProductResult) {
    requested = false;
    for (const listener of listeners) listener(result);
    changed();
  },
  listen(listener: Listener) {
    listeners.add(listener);
    changed();
    return () => { listeners.delete(listener); if (!listeners.size) requested = false; changed(); };
  },
  subscribe(watcher: () => void) { watchers.add(watcher); return () => { watchers.delete(watcher); }; },
  version() { return version; },
};
