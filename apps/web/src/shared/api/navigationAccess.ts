export type Access = 'checking' | 'allowed' | 'denied' | 'error';

const endpoints = new Set([
  '/cash-registers', '/purchase-orders', '/suppliers', '/stores/admin', '/users', '/price-lists', '/storefront',
]);
const results = new Map<string, Access>();
const pending = new Map<string, number>();
const listeners = new Set<() => void>();
let version = 0;
let session = 0;

function notify() {
  version++;
  listeners.forEach((listener) => listener());
}

export function resetNavigationAccess() {
  session++;
  results.clear();
  pending.clear();
  notify();
}

export function subscribeNavigationAccess(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function navigationAccessVersion() { return version; }
export function navigationAccess(key: string) { return results.get(key); }

export function trackNavigationRead(path: string, method = 'GET') {
  if (method !== 'GET') return;
  const [endpoint, search] = path.split('?', 2);
  if (!endpoints.has(endpoint)) return;
  const branch = new URLSearchParams(search).get('store_id');
  const key = navigationAccessKey(endpoint, branch);
  const currentSession = session;
  pending.set(key, (pending.get(key) ?? 0) + 1);
  return (status: number) => {
    if (currentSession !== session) return;
    pending.set(key, (pending.get(key) ?? 1) - 1);
    const access = status >= 200 && status < 300 ? 'allowed'
      : status === 401 || status === 403 || status === 404 ? 'denied' : 'error';
    // A failed filter or transient read does not change a confirmed permission.
    if (access === 'error' && (results.get(key) === 'allowed' || results.get(key) === 'denied')) return;
    if (results.get(key) !== access) {
      results.set(key, access);
      notify();
    }
  };
}

export function navigationAccessKey(endpoint: string, branch?: string | null) {
  return endpoint + (branch ? ':' + branch : '');
}

export function checkNavigationAccess(key: string, load: () => Promise<unknown>, retry = false) {
  if (pending.get(key) || (results.has(key) && !retry)) return;
  results.set(key, 'checking');
  notify();
  void load().catch(() => { /* The normal request records its result. */ });
}
