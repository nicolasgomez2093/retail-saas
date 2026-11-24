import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, useId } from 'react';
import { api, ApiError, type CashRegisters, type Customer, type Reservation, type Sale, type SaleInput, type SalePaymentMethod, type SaleTicket, type StockLocation, type Store } from '../../shared/api/client';
import { decimalInput, decimalText, minorUnits } from '../../shared/lib/decimal';
import { InventoryVariantPicker, type FavoriteProduct, type Selected } from '../inventory/InventoryVariantPicker';
import { CustomerPicker } from './CustomerPicker';
import { Reservations } from './Reservations';
import { Receivables } from './Receivables';
import { paymentLabels, paymentMethods, SalePaymentBreakdown } from './SalePayments';
import { SalesHistory } from './SalesHistory';
import { posPrice } from './priceDisplay';

type CartLine = Selected & { quantity: string; price: string | null; discountPercent?: string };

function validDiscount(value = '') {
  return value === '' || (/^\d{1,2}$/.test(value) && Number(value) <= 80);
}

function lineTotal(line: CartLine) {
  if (line.price === null || !validQuantity(line.quantity) || !validDiscount(line.discountPercent)) return 0n;
  const unit = minorUnits(line.price);
  const discount = unit * BigInt(Number(line.discountPercent ?? '')) / 100n;
  return (unit - discount) * BigInt(line.quantity);
}
type PaymentField = 'cash' | 'allocations' | 'customer' | 'method';

class PaymentIssue extends Error {
  constructor(message: string, readonly field: PaymentField, readonly missing?: bigint, readonly allocationMethod?: SalePaymentMethod) {
    super(message);
  }
}

function readCash(raw: string, required: bigint, shortfallMessage: string) {
  let amount: string;
  try {
    amount = decimalInput(raw);
  } catch (cause) {
    throw new PaymentIssue(cause instanceof Error ? cause.message : 'Ingresá el efectivo recibido.', 'cash');
  }
  const difference = minorUnits(amount) - required;
  if (difference < 0n) throw new PaymentIssue(shortfallMessage, 'cash', -difference);
  return { amount, difference };
}

type SaleAttempt = { input: SaleInput; key: string };
type CheckoutMethod = SalePaymentMethod | 'mixed' | 'creditSale';

function readFavorites(key: string): FavoriteProduct[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? '[]');
    return Array.isArray(value) ? value.filter((item): item is FavoriteProduct =>
      item !== null && typeof item === 'object' && typeof item.id === 'string' && typeof item.name === 'string',
    ) : [];
  } catch {
    return [];
  }
}

export function PointOfSale({ stores, settingsRevision, historyRevision, onSold, onOperationPending, view, favoriteScope, customerAccounts, onCartChanged, active = true }: {
  active?: boolean;
  onCartChanged: (storeId: string, summary: { units: number; total: string }) => void;
  customerAccounts: boolean;
  favoriteScope: [tenantId: string, userId: string];
  stores: Store[]; settingsRevision: number; historyRevision: number; onSold: () => void; view: 'checkout' | 'history' | 'customers';
  onOperationPending: (pending: boolean, origin: 'vender' | 'ventas' | 'clientes') => void;
}) {
  const [selected, setSelected] = useState('');
  const [pending, setPending] = useState(false);
  const picked = stores.some((store) => store.id === selected) ? selected : stores[0]?.id;
  const storeId = pending && selected ? selected : picked;
  if (!storeId) return null;

  const favoriteKey = 'retail:pos-favorites:v1:' + JSON.stringify([...favoriteScope, storeId]);
  return <StorePOS key={favoriteKey} favoriteKey={favoriteKey} storeId={storeId} stores={stores} onStore={setSelected} onSold={onSold}
    settingsRevision={settingsRevision} historyRevision={historyRevision} view={view} active={active} customerAccounts={customerAccounts} onCartChanged={onCartChanged}
    onPending={(locked, origin = 'vender') => {
      setSelected(storeId);
      setPending(locked);
      onOperationPending(locked, origin);
    }} />;
}

function StorePOS({ storeId, stores, onStore, onSold, settingsRevision, historyRevision, onPending, view, favoriteKey, customerAccounts, onCartChanged, active }: {
  active: boolean;
  onCartChanged: (storeId: string, summary: { units: number; total: string }) => void;
  customerAccounts: boolean;
  favoriteKey: string; storeId: string; stores: Store[]; onStore: (id: string) => void; onSold: () => void;
  settingsRevision: number; historyRevision: number; view: 'checkout' | 'history' | 'customers';
  onPending: (pending: boolean, origin?: 'vender' | 'ventas' | 'clientes') => void;
}) {
  const instanceId = useId();
  const [registers, setRegisters] = useState<CashRegisters | null>(null);
  const [allLocations, setAllLocations] = useState<StockLocation[]>([]);
  const [sessionId, setSessionId] = useState('');
  const [locationId, setLocationId] = useState('');
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [compactPane, setCompactPane] = useState<'products' | 'transaction'>('products');
  const catalogScroll = useRef(0);
  const checkoutHeading = useRef<HTMLHeadingElement>(null);
  const resultHeading = useRef<HTMLHeadingElement>(null);
  const searchPanel = useRef<HTMLDivElement>(null);
  const cartHeading = useRef<HTMLHeadingElement>(null);
  const [favorites, setFavorites] = useState(() => readFavorites(favoriteKey));
  function toggleFavorite(product: FavoriteProduct) {
    const next = favorites.some(({ id }) => id === product.id)
      ? favorites.filter(({ id }) => id !== product.id)
      : [...favorites, { id: product.id, name: product.name }];
    setFavorites(next);
    try {
      if (next.length) localStorage.setItem(favoriteKey, JSON.stringify(next));
      else localStorage.removeItem(favoriteKey);
    } catch {
      // Keep the current session usable when browser storage is unavailable.
    }
  }
  const [cart, setCart] = useState<CartLine[]>([]);
  const [method, setMethod] = useState<CheckoutMethod>('cash');
  const [customer, setCustomer] = useState<Customer | null>(null);
  const [allocations, setAllocations] = useState<Partial<Record<SalePaymentMethod, string>>>({});
  const [tender, setTender] = useState('');
  const [sale, setSale] = useState<Sale | null>(null);
  const [announceSale, setAnnounceSale] = useState(false);
  const [ticket, setTicket] = useState<{ data: SaleTicket; Dialog: typeof import('./TicketDialog').TicketDialog } | null>(null);
  const ticketOpener = useRef<HTMLButtonElement | null>(null);
  const [ticketBusy, setTicketBusy] = useState(false);
  const [ticketError, setTicketError] = useState('');
  const ticketGeneration = useRef(0);
  useLayoutEffect(() => {
    ticketGeneration.current++; setTicket(null); setTicketError(''); setTicketBusy(false);
  }, [active, view]);
  const [busy, setBusy] = useState(false);
  const [paymentIssue, setPaymentIssue] = useState<PaymentIssue | null>(null);
  const [rejection, setRejection] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const [collectionLocked, setCollectionLocked] = useState(false);
  const [reservationLocked, setReservationLocked] = useState(false);
  const [returnLocked, setReturnLocked] = useState(false);
  const [reservation, setReservation] = useState<Reservation | null>(null);
  const [error, setError] = useState('');
  type SetupRead = { status: 'loading' | 'ready' | 'error' | 'denied' | 'session'; message?: string };
  const [setup, setSetup] = useState<{ cash: SetupRead; locations: SetupRead }>({ cash: { status: 'loading' }, locations: { status: 'loading' } });
  const [setupAttempt, setSetupAttempt] = useState(0);
  const attempt = useRef<SaleAttempt | null>(null);
  const confirming = useRef(false);
  const retryButton = useRef<HTMLButtonElement>(null);
  const rejectionNotice = useRef<HTMLParagraphElement>(null);

  useLayoutEffect(() => {
    if (!active) return;
    let current = true;
    setSetup({ cash: { status: 'loading' }, locations: { status: 'loading' } });
    function failure(resource: 'cash' | 'locations', cause: unknown) {
      if (!current) return;
      const status = cause instanceof ApiError ? cause.status : 0;
      const state: SetupRead = status === 401 ? { status: 'session', message: 'La sesión venció. Volvé a iniciar sesión.' }
        : status === 403 ? { status: 'denied', message: resource === 'cash' ? 'No tenés permiso para consultar caja.' : 'No tenés permiso para consultar ubicaciones.' }
        : { status: 'error', message: resource === 'cash' ? 'No pudimos consultar las cajas.' : 'No pudimos consultar las ubicaciones.' };
      setSetup(previous => ({ ...previous, [resource]: state }));
    }
    void api.cashRegisters(storeId).then(cash => {
      if (!current) return;
      setRegisters(cash); setSetup(previous => ({ ...previous, cash: { status: 'ready' } }));
    }).catch(cause => failure('cash', cause));
    void api.locations(storeId).then(stock => {
      if (!current) return;
      setAllLocations(stock.items); setSetup(previous => ({ ...previous, locations: { status: 'ready' } }));
    }).catch(cause => failure('locations', cause));
    return () => { current = false; };
  }, [active, storeId, settingsRevision, setupAttempt]);

  useLayoutEffect(() => {
    const heading = resultHeading.current;
    if (sale && heading?.getClientRects().length) heading.focus();
  }, [sale]);

  useLayoutEffect(() => {
    if (busy || view !== 'checkout') return;
    const target = uncertain ? retryButton.current : rejection ? rejectionNotice.current : null;
    if (target?.getClientRects().length && document.activeElement === document.body) target.focus();
  }, [busy, rejection, uncertain, view]);

  const locations = allLocations.filter((location) => location.isSellable);
  const sessions = registers?.items.filter((item) => item.openSessionId) ?? [];
  const activeSession = sessionId ? sessions.find((item) => item.openSessionId === sessionId) : sessions[0];
  const activeLocation = locationId ? locations.find((item) => item.id === locationId) : locations[0];
  const setupReady = active && setup.cash.status === 'ready' && setup.locations.status === 'ready';
  const canSell = Boolean(setupReady && activeSession?.openSessionId && activeLocation);
  const locked = busy || uncertain || collectionLocked || reservationLocked || returnLocked || !setupReady;
  const currency = registers?.currency ?? '';
  const total = cart.reduce((value, line) => value + lineTotal(line), 0n);
  const listSubtotal = cart.reduce((value, line) =>
    line.price !== null && validQuantity(line.quantity)
      ? value + minorUnits(line.price) * BigInt(line.quantity) : value, 0n);
  const deposited = reservation?.deposit ? minorUnits(reservation.deposit.amount) : 0n;
  const due = total - deposited;
  const prepaid = deposited > 0n && due === 0n;
  const onCredit = method === 'creditSale';
  const splitPayments = method === 'mixed' || onCredit;
  const showTender = !prepaid && (method === 'cash' || (splitPayments && Boolean(allocations.cash?.trim())));
  const cartValid = cart.length > 0 && cart.every((line) => line.price !== null && validQuantity(line.quantity) &&
    validDiscount(line.discountPercent) && (registers?.canDiscount || !Number(line.discountPercent)));

  function message(cause: unknown) {
    return cause instanceof Error ? cause.message : 'No pudimos completar la venta.';
  }

  function display(price: string) {
    return currency + ' ' + posPrice(price);
  }

  async function add(selected: Selected) {
    if (locked || !activeSession?.openSessionId || !activeLocation) return;
    setSessionId(activeSession.openSessionId);
    setLocationId(activeLocation.id);
    setError('');
    setSale(null);
    const existing = cart.find((line) => line.id === selected.id);
    if (existing) {
      setCart((previous) => previous.map((line) => line.id === selected.id && Number(line.quantity) < 2147483647
        ? { ...line, quantity: String((validQuantity(line.quantity) ? Number(line.quantity) : 0) + 1) }
        : line));
      return;
    }
    if (cart.length >= 100) {
      setError('El carrito admite hasta 100 variantes.');
      return;
    }
    setBusy(true);
    try {
      const quote = await api.resolvePrice(storeId, selected.id);
      setCart((previous) => [...previous, { ...selected, quantity: '1', price: quote.amount }]);
      if (quote.amount === null) setError('La variante seleccionada está sin precio para esta sucursal.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  async function refreshPrices() {
    const prices = await Promise.all(cart.map((line) => api.resolvePrice(storeId, line.id)));
    setCart((previous) => previous.map((line) => {
      const index = cart.findIndex((original) => original.id === line.id);
      return index < 0 ? line : { ...line, price: prices[index].amount };
    }));
  }

  async function confirm(current: SaleAttempt) {
    if (confirming.current || attempt.current !== current) return;
    confirming.current = true;
    setBusy(true);
    onPending(true);
    setError('');
    setPaymentIssue(null);
    setRejection('');
    try {
      const confirmed = await api.confirmSale(current.input, current.key);
      attempt.current = null;
      setUncertain(false);
      setCart([]);
      setReservation(null);
      setTender('');
      setAllocations({});
      setMethod('cash');
      setCustomer(null);
      setCheckoutOpen(false);
      setSale(confirmed);
      setAnnounceSale(true);
      onSold();
      onPending(false);
    } catch (cause) {
      const ambiguous = !(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500;
      setUncertain(ambiguous);
      if (ambiguous) {
        setError('No se pudo confirmar el resultado. Reintentá la misma venta para comprobar si se registró.');
      } else {
        attempt.current = null;
        onPending(false);
        const guidance = cause.code === 'sale_stock_insufficient'
          ? ' Revisá las cantidades o quitá la variante sin disponibilidad desde el carrito.'
          : cause.code === 'sale_location_unavailable' ? ' Elegí un depósito vendible para continuar.' : '';
        setRejection(message(cause) + guidance);
        if (cause.code === 'reservation_deposit_changed') onSold();
        if (cause.code === 'sale_price_changed' || cause.code === 'sale_price_missing') {
          try { await refreshPrices(); } catch {
            setRejection('El precio cambió. Quitá las variantes y volvé a agregarlas para consultar los importes actuales.');
          }
        }
      }
    } finally {
      confirming.current = false;
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!checkoutOpen || locked || attempt.current || !activeSession?.openSessionId || !activeLocation || !cartValid || due < 0n) return;
    try {
      const input: SaleInput = {
        storeId, cashSessionId: activeSession.openSessionId, locationId: activeLocation.id, reservationId: reservation?.id,
        expectedDeposit: reservation?.deposit?.amount,
        items: cart.map((line) => ({ variantId: line.id, quantity: Number(line.quantity), expectedUnitPrice: line.price ?? '', discountPercent: Number(line.discountPercent || 0) || undefined })),
        ...paymentInput(),
      };
      const next = { input, key: crypto.randomUUID() };
      attempt.current = next;
      void confirm(next);
    } catch (cause) {
      const issue = cause instanceof PaymentIssue ? cause : new PaymentIssue(message(cause), 'method');
      setPaymentIssue(issue);
      const form = event.currentTarget;
      const target = issue.allocationMethod === 'cash' ? 'allocation-cash' : issue.allocationMethod ?? issue.field;
      requestAnimationFrame(() => form.parentElement?.querySelector<HTMLElement>('[data-payment-focus="' + target + '"]')?.focus());
    }
  }

  function paymentInput() {
    if (due < 0n) throw new PaymentIssue('La seña supera el total vigente de la venta.', 'method');
    if (prepaid) return { payments: [] };
    if (method === 'cash') return { cashReceived: readCash(tender, due, 'El efectivo recibido no cubre el total.').amount };
    if (due === 0n) throw new PaymentIssue('Registrá la venta de importe cero con la opción Efectivo.', 'method');
    if (method !== 'mixed' && method !== 'creditSale') return { payments: [{ method, amount: decimalText(due) }] };

    const { payments, paid } = paymentAllocations();
    if (!onCredit && payments.length < 2) throw new PaymentIssue('Ingresá importes para al menos dos medios de pago.', 'allocations');
    if (onCredit && !customer) throw new PaymentIssue('Elegí un cliente para la venta fiada.', 'customer');
    if (onCredit && paid >= due) throw new PaymentIssue('La venta fiada debe dejar un saldo pendiente.', 'allocations');
    if (!onCredit && paid !== due) {
      throw new PaymentIssue('La suma de los pagos debe coincidir con el total de la venta.', 'allocations');
    }
    const applied = payments.map((payment) => {
      if (payment.method !== 'cash') return payment;
      const cashReceived = readCash(tender, minorUnits(payment.amount), 'El efectivo recibido no cubre la parte en efectivo.').amount;
      return { ...payment, cashReceived };
    });
    if (onCredit && customer) return { payments: applied, onCredit: true as const, customerId: customer.id };
    return { payments: applied };
  }

  function paymentAllocations() {
    const payments = paymentMethods.flatMap((entry) => {
      const raw = allocations[entry]?.trim();
      if (!raw) return [];
      try {
        const amount = decimalInput(raw);
        return minorUnits(amount) === 0n ? [] : [{ method: entry, amount }];
      } catch (cause) {
        throw new PaymentIssue(paymentLabels[entry] + '. ' + message(cause), 'allocations', undefined, entry);
      }
    });
    return { payments, paid: payments.reduce((sum, payment) => sum + minorUnits(payment.amount), 0n) };
  }

  function allocationOverview() {
    if (!splitPayments || !cartValid || due < 0n || prepaid) return null;
    try {
      return paymentAllocations();
    } catch {
      return null;
    }
  }

  async function loadReservation(item: Reservation) {
    if (locked) return;
    if (cart.length) {
      setError('Vaciá el carrito antes de cargar la reserva.');
      return;
    }
    if (!activeSession?.openSessionId || !locations.some((location) => location.id === item.locationId)) {
      setError('Necesitás una caja abierta y la ubicación vendible de la reserva.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const prices = await Promise.all(item.items.map((line) => api.resolvePrice(storeId, line.variantId)));
      setCheckoutOpen(false);
      setReservation(item);
      setLocationId(item.locationId);
      setSessionId(activeSession.openSessionId);
      setMethod('cash');
      setTender('');
      setAllocations({});
      setCustomer(null);
      setSale(null);
      setCart(item.items.map((line, index) => ({
        ...line, id: line.variantId, quantity: String(line.quantity), price: prices[index].amount,
      })));
      showTransaction(false);
      if (prices.some((price) => price.amount === null)) setError('La reserva incluye una variante sin precio para esta sucursal.');
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  async function openTicket(id: string, opener: HTMLButtonElement) {
    if (ticketBusy || !active) return;
    const generation = ++ticketGeneration.current;
    ticketOpener.current = opener;
    setAnnounceSale(false);
    setTicketBusy(true);
    setTicketError('');
    try {
      const [data, { TicketDialog }] = await Promise.all([api.saleTicket(storeId, id), import('./TicketDialog')]);
      if (generation === ticketGeneration.current) setTicket({ data, Dialog: TicketDialog });
    } catch (cause) {
      if (generation === ticketGeneration.current) setTicketError(message(cause));
    } finally {
      if (generation === ticketGeneration.current) setTicketBusy(false);
    }
  }

  function closeTicket() {
    ticketGeneration.current++;
    const opener = ticketOpener.current;
    ticketOpener.current = null;
    setTicket(null);
    requestAnimationFrame(() => {
      if (opener?.isConnected && opener.getClientRects().length) opener.focus();
    });
  }

  const paymentVisible = checkoutOpen && cart.length > 0;
  const canCheckout = canSell && cartValid && due >= 0n;

  function selectPayment(next: CheckoutMethod) {
    setMethod(next);
    setPaymentIssue(null);
    setRejection('');
    setTender('');
    setAllocations({});
    setCustomer(reservation ? { id: reservation.customerId, name: reservation.customerName, phone: '' } : null);
  }

  function cashOverview(cashAssigned?: string) {
    if (!showTender || !tender.trim() || !cartValid || due < 0n) return null;
    if (method !== 'cash' && cashAssigned === undefined) return null;
    try {
      const cashAmount = cashAssigned === undefined ? due : minorUnits(cashAssigned);
      return readCash(tender, cashAmount, method === 'cash'
        ? 'El efectivo recibido no cubre el total.'
        : 'El efectivo recibido no cubre la parte en efectivo.');
    } catch (cause) {
      return cause instanceof PaymentIssue ? cause : null;
    }
  }

  function changeQuantity(id: string, delta: number) {
    setCart((previous) => previous.map((line) => {
      if (line.id !== id || !validQuantity(line.quantity)) return line;
      const next = Number(line.quantity) + delta;
      return next >= 1 && next <= 2147483647 ? { ...line, quantity: String(next) } : line;
    }));
  }

  function focusProductSearch() {
    const input = 'posQuery';
    searchPanel.current?.querySelector<HTMLInputElement>('input[name=' + input + ']')?.focus();
  }

  function showTransaction(payment: boolean) {
    if (compactPane === 'products' && window.matchMedia('(max-width: 900px)').matches) {
      catalogScroll.current = window.scrollY;
    }
    setCompactPane('transaction');
    requestAnimationFrame(() => {
      const heading = payment ? checkoutHeading.current : cartHeading.current;
      if (payment && uncertain && retryButton.current?.getClientRects().length) retryButton.current.focus();
      else heading?.focus();
    });
  }

  function showCatalog(focusSearch = false) {
    setCompactPane('products');
    requestAnimationFrame(() => {
      if (focusSearch) focusProductSearch();
      else searchPanel.current?.querySelector<HTMLElement>('.pos-catalog-title')?.focus({ preventScroll: true });
      if (window.matchMedia('(max-width: 900px)').matches && !focusSearch) {
        window.scrollTo(0, catalogScroll.current);
      }
    });
  }

  function openCheckout() {
    setPaymentIssue(null);
    setRejection('');
    setCheckoutOpen(true);
    showTransaction(true);
  }

  const cartUnits = cart.reduce((units, line) => units + (validQuantity(line.quantity) ? Number(line.quantity) : 0), 0);
  const cartTotalLabel = !cart.length ? display('0.00') : cartValid ? display(decimalText(total)) : 'Total pendiente';
  useEffect(() => {
    onCartChanged(storeId, { units: cartUnits, total: cartTotalLabel });
  }, [onCartChanged, storeId, cartUnits, cartTotalLabel]);
  const discountSummary = cartValid && listSubtotal > total && <dl className="pos-discount-summary">
    <div><dt>Subtotal</dt><dd>{display(decimalText(listSubtotal))}</dd></div>
    <div><dt>Descuento</dt><dd>− {display(decimalText(listSubtotal - total))}</dd></div>
  </dl>;
  const allocation = checkoutOpen ? allocationOverview() : null;
  const difference = allocation ? due - allocation.paid : null;
  const cash = paymentVisible ? cashOverview(allocation?.payments.find((payment) => payment.method === 'cash')?.amount) : null;
  const cashIssue = paymentIssue?.field === 'cash' ? paymentIssue : cash instanceof PaymentIssue ? cash : null;
  const checkoutBlocked = !canSell ? 'Elegí una caja abierta y un depósito vendible.'
    : cart.some((line) => line.price === null) ? 'Quitá los productos sin precio o volvé a agregarlos cuando tengan precio en la lista activa.'
    : !cartValid ? 'Corregí las cantidades o descuentos marcados para continuar.'
    : due < 0n ? 'Revisá el importe de la seña antes de cobrar.' : '';
  const checkoutBlockId = instanceId + '-checkout-blocked';
  const sessionErrorId = sessionId && !activeSession ? instanceId + '-session-error' : undefined;
  const locationErrorId = locationId && !activeLocation ? instanceId + '-location-error' : undefined;
  const paymentErrorId = instanceId + '-payment-error';
  if (active && [setup.cash, setup.locations].some(read => read.status === 'denied' || read.status === 'session')) {
    return <div role="alert" className="pos-operation-error"><p>{[setup.cash,setup.locations].find(read => read.status === 'session' || read.status === 'denied')?.message}</p>
      <button type="button" className="pos-secondary" onClick={() => setSetupAttempt(value => value + 1)}>Verificar acceso</button></div>;
  }
  return (
    <>
        {active && (setup.cash.status === 'loading' || setup.locations.status === 'loading') && <p role="status">Consultando el puesto de venta…</p>}
        {active && (['cash','locations'] as const).map(resource => setup[resource].status === 'error' && <p className="auxiliary-status" key={resource}>{setup[resource].message} Las operaciones que lo necesitan están bloqueadas hasta verificarlo. <button type="button" onClick={() => setSetupAttempt(value => value + 1)}>Reintentar {resource === 'cash' ? 'cajas' : 'ubicaciones'}</button></p>)}

      <section className="pos-workspace" hidden={view !== 'checkout'} aria-labelledby={instanceId + "-pos-title"}>
        <h2 id={instanceId + "-pos-title"} className="pos-sr-only">Punto de venta</h2>
        <p className="pos-sr-only" role="status">
          {sale && announceSale && `Venta N.º ${sale.number} registrada por ${display(sale.total)}.`}
        </p>
        {error && !uncertain && !paymentVisible && <p role="alert" className="pos-operation-error">{error}</p>}
        <div className="pos-context">
          {stores.length > 1 && <label>Sucursal de venta
            <select name="posStore" value={storeId} disabled={locked} onChange={(event) => onStore(event.target.value)}>
              {stores.map((item) => <option key={item.id} value={item.id}>{item.name} ({item.code})</option>)}
            </select>
          </label>}
          {setup.cash.status === 'ready' && registers && !sessions.length && <p role="status">Abrí una sesión de caja para empezar a vender. <a href="#/finanzas/caja">Ir a Caja</a></p>}
          {setup.locations.status === 'ready' && !locations.length && <p role="status">Esta sucursal todavía no tiene una ubicación vendible.</p>}
          {registers && (
            <>
              {sessionErrorId && <p role="alert" id={sessionErrorId}>La caja seleccionada ya no está disponible. Elegí otra para continuar.</p>}
              {locationErrorId && <p role="alert" id={locationErrorId}>La ubicación seleccionada ya no está disponible. Elegí otra para continuar.</p>}
              <label>Caja
                <select name="posSession" aria-invalid={Boolean(sessionErrorId)} aria-describedby={sessionErrorId} value={activeSession?.openSessionId ?? ''} disabled={locked || !setupReady || !sessions.length}
                  onChange={(event) => setSessionId(event.target.value)}>
                  {!activeSession && <option value="">{setup.cash.status === 'ready' ? 'Sin cajas abiertas' : 'Caja no verificada'}</option>}
                  {sessions.map((item) => <option key={item.id} value={item.openSessionId ?? ''}>{item.name} ({item.code})</option>)}
                </select>
              </label>
              <label>Salida de stock
                <select name="posLocation" aria-invalid={Boolean(locationErrorId)} aria-describedby={locationErrorId} value={activeLocation?.id ?? ''} disabled={locked || !setupReady || !locations.length || Boolean(reservation)}
                  onChange={(event) => setLocationId(event.target.value)}>
                  {!activeLocation && <option value="">{setup.locations.status === 'ready' ? 'Sin ubicaciones vendibles' : 'Ubicaciones no verificadas'}</option>}
                  {locations.map((item) => <option key={item.id} value={item.id}>{item.name} ({item.code})</option>)}
                </select>
              </label>
  
            </>
          )}
        </div>
        {(canSell || cart.length > 0 || reservation) && <div className={paymentVisible ? 'pos-layout pos-layout-checkout' : 'pos-layout'} data-pos-pane={compactPane}>
          <div className="pos-surface pos-catalog-surface" ref={searchPanel}>
            <div className="pos-catalog-heading">
              <h3 className="pos-catalog-title" tabIndex={-1}>Agregar productos</h3>
            </div>
            {canSell && !reservation && <InventoryVariantPicker forSale storeId={storeId} checkoutOpen={paymentVisible} disabled={locked} favorites={favorites} onToggleFavorite={toggleFavorite} onSelect={(selected) => {
              if (!cart.length) setCheckoutOpen(false);
              return add(selected);
            }} />}
            {reservation && <p>Estás cobrando una reserva.</p>}
          </div>
          <div className="pos-surface pos-cart">
            <div className="pos-cart-heading" hidden={paymentVisible}>
              <h3 ref={cartHeading} tabIndex={-1}>Carrito{cart.length > 0 ? ' (' + cart.length + ')' : ''}</h3>
              {!paymentVisible && <button type="button" className="pos-secondary pos-compact-products" onClick={() => showCatalog()}>Ver productos</button>}
              {cart.length > 0 && !reservation && <button type="button" className="pos-secondary" disabled={locked} onClick={() => {
                if (window.confirm('¿Vaciar el carrito?')) {
                  setCart([]);
                  setCheckoutOpen(false);
                  setTender('');
                  setAllocations({});
                  setMethod('cash');
                  setCustomer(null);
                  setError('');
                  showCatalog(true);
                }
              }}>Vaciar carrito</button>}
            </div>
            <p className="pos-sr-only" role="status">
              {cart.length > 0 && (cartValid
                ? `Total del carrito: ${display(decimalText(total))}`
                : 'El total del carrito está pendiente. Revisá las cantidades y los precios.')}
            </p>
            {!cart.length && <div className="pos-empty-cart"><h4>El carrito está vacío</h4>
              <p>Escaneá un código o buscá un producto para empezar.</p>
            </div>}
            {cart.length > 0 && <>
              <div className="pos-cart-body">
                <form id={instanceId + "-pos-checkout"} aria-label="Cobrar venta" className="inventory-form" onSubmit={submit}
                  onKeyDown={(event) => {
                    if (paymentVisible && event.key === 'Enter' && event.target instanceof HTMLInputElement) event.preventDefault();
                  }}>
                  {reservation && <div className="actions">
                    <strong>Reserva de {reservation.customerName}</strong>
                    <button type="button" disabled={locked} onClick={() => {
                      setReservation(null);
                      setCart([]);
                      setMethod('cash');
                      setCustomer(null);
                      setTender('');
                      setAllocations({});
                      setError('');
                    }}>Quitar reserva del carrito</button>
                  </div>}
                  <ul className="catalog-items pos-lines">{cart.map((line) => {
                    const invalidQuantity = !validQuantity(line.quantity);
                    const unauthorizedDiscount = !registers?.canDiscount && Boolean(Number(line.discountPercent));
                    const discountError = unauthorizedDiscount ? 'Tu usuario no tiene permiso para aplicar descuentos.'
                      : !validDiscount(line.discountPercent) ? 'Ingresá un porcentaje entero entre 0 y 80.' : '';
                    const lineErrorId = instanceId + '-line-' + line.id;
                    return <li key={line.id}>
                    <div className="pos-line-identity">
                      {line.photoURL && <img className="pos-line-image" src={line.photoURL} alt="" />}
                      <div className="pos-line-description">
                        <strong>{line.product}</strong>
                        <div className="pos-line-meta">
                          <small>{[line.size, line.color, line.sku].filter(Boolean).join(' · ')}</small>
                          {line.price === null
                            ? <small className="pos-line-price-status">Sin precio</small>
                            : <small>Unidad {display(line.price)}</small>}
                        </div>
                      </div>
                    </div>
                    {line.price !== null && validQuantity(line.quantity) && validDiscount(line.discountPercent) &&
                      <strong className="pos-line-subtotal">
                        <span className="pos-sr-only">Importe de línea </span>
                        {display(decimalText(lineTotal(line))).split('.').map((group, index) =>
                          <span key={index}>{index > 0 && <>.<wbr /></>}{group}</span>)}
                      </strong>}
                    <div className="pos-line-controls">
                      <div className="pos-line-actions">
                        <button type="button" className="pos-secondary" aria-label={'Restar unidad de ' + line.sku}
                          disabled={locked || Boolean(reservation) || !validQuantity(line.quantity) || Number(line.quantity) <= 1}
                          onClick={() => changeQuantity(line.id, -1)}>−</button>
                        <input type="number" aria-label={'Cantidad de ' + line.sku} inputMode="numeric"
                          value={line.quantity} min={1} max={2147483647} step={1} required disabled={locked || Boolean(reservation)}
                          aria-invalid={invalidQuantity} aria-describedby={invalidQuantity ? lineErrorId + '-quantity' : undefined}
                          onChange={(event) => setCart((previous) => previous.map((item) =>
                            item.id === line.id ? { ...item, quantity: event.target.value } : item))} />
                        <button type="button" className="pos-secondary" aria-label={'Sumar unidad de ' + line.sku}
                          disabled={locked || Boolean(reservation) || !validQuantity(line.quantity) || Number(line.quantity) >= 2147483647}
                          onClick={() => changeQuantity(line.id, 1)}>+</button>
                      </div>
                      {(registers?.canDiscount || Boolean(line.discountPercent)) && <label className="pos-line-discount">
                        <span aria-hidden="true">Dto.</span><span className="pos-sr-only">Descuento porcentual de {line.sku}</span>
                        <input name={'discount-' + line.id} type="number" inputMode="numeric" min={0}
                          max={registers?.canDiscount ? 80 : 0} step={1} value={line.discountPercent ?? ''}
                          disabled={locked || paymentVisible || line.price === null}
                          aria-invalid={Boolean(discountError)} aria-describedby={discountError ? lineErrorId + '-discount' : undefined}
                          onChange={(event) => {
                            const discountPercent = event.target.value;
                            setCart((previous) => previous.map((item) => item.id === line.id ? { ...item, discountPercent } : item));
                            setAllocations({});
                            setTender('');
                          }} />
                        <span aria-hidden="true">%</span>
                      </label>}
                      <button type="button" className="pos-secondary pos-remove-line" disabled={locked || Boolean(reservation)}
                        title={'Quitar ' + line.sku} aria-label={'Quitar ' + line.sku}
                        onClick={() => setCart((previous) => previous.filter((item) => item.id !== line.id))}>Quitar</button>
                    </div>
                    {(invalidQuantity || discountError) && <div className="pos-line-validation">
                      {invalidQuantity && <p id={lineErrorId + '-quantity'} className="pos-validation-note">Ingresá una cantidad entera mayor que cero dentro del rango permitido.</p>}
                      {discountError && <p id={lineErrorId + '-discount'} className="pos-validation-note">{discountError}</p>}
                    </div>}
                  </li>;
                  })}</ul>
                  {!paymentVisible && reservation?.deposit && <>
                    <p>Seña aplicada <strong>{display(reservation.deposit.amount)}</strong></p>
                    {cartValid && (due >= 0n
                      ? <p>Saldo a cobrar <strong>{display(decimalText(due))}</strong></p>
                      : <p role="alert">La seña supera el total vigente de la venta.</p>)}
                  </>}
                  <div hidden={!paymentVisible} className="pos-payment">
                    <div className="pos-payment-heading">
                      <h4 tabIndex={-1} ref={checkoutHeading}>Cobro{reservation ? ' de reserva' : ''}</h4>
                      <div className="pos-payment-navigation">
                        <button type="button" className="pos-secondary pos-compact-products" onClick={() => showCatalog()}>Ver productos</button>
                        <button type="button" className="pos-secondary" disabled={locked} onClick={() => {
                          setCheckoutOpen(false);
                          showTransaction(false);
                        }}>Volver al carrito</button>
                      </div>
                    </div>
                    <details className="pos-payment-summary">
                      <summary>Resumen de compra · {cartUnits} {cartUnits === 1 ? 'unidad' : 'unidades'}</summary>
                      <ul className="catalog-items">
                        {cart.map((line) => <li key={line.id}>
                          <span>{line.product}<small>{[line.size, line.color, line.sku].filter(Boolean).join(' · ')} · {line.quantity} u.</small></span>
                          <strong>{line.price === null ? 'Sin precio' : display(decimalText(lineTotal(line)))}</strong>
                        </li>)}
                      </ul>
                      {discountSummary}
                    </details>
                    <div className="pos-payment-total">
                      <strong>Total a cobrar</strong>
                      <strong>{canCheckout ? display(decimalText(due)) : 'Pendiente'}</strong>
                      {reservation?.deposit && <small>Seña aplicada {display(reservation.deposit.amount)} sobre {display(decimalText(total))}</small>}
                    </div>
                    {!prepaid && <>
                      <div className="pos-payment-choices" role="group" aria-label="Medio de pago" tabIndex={-1}
                        data-payment-focus="method" aria-describedby={paymentIssue?.field === 'method' ? paymentErrorId : undefined}>
                        {([...paymentMethods, 'mixed', 'creditSale'] as const).map((entry) =>
                          <button key={entry} type="button" className="pos-secondary" aria-pressed={method === entry} disabled={locked}
                            onClick={() => { if (method !== entry) selectPayment(entry); }}>
                            {entry === 'mixed' ? 'Pago mixto' : entry === 'creditSale' ? 'Fiado' : paymentLabels[entry]}
                          </button>)}
                      </div>
                      {paymentIssue?.field === 'method' && <p id={paymentErrorId} role="alert" className="pos-operation-error">{paymentIssue.message}</p>}
                    </>}
                    {prepaid && <p className="pos-payment-note">La seña cubre el total de la reserva.</p>}
                    {!prepaid && splitPayments && <fieldset className="pos-payment-allocations" tabIndex={-1} data-payment-focus="allocations"
                      aria-describedby={paymentIssue?.field === 'allocations' ? paymentErrorId : undefined}>
                      <legend>{onCredit ? 'Anticipo (opcional)' : 'Importes por medio de pago'}</legend>
                      {paymentMethods.map((entry) => <label key={entry}>Importe en {paymentLabels[entry].toLowerCase()}
                        <input inputMode="decimal" maxLength={19} disabled={locked} value={allocations[entry] ?? ''}
                          data-payment-focus={entry === 'cash' ? 'allocation-cash' : entry}
                          aria-invalid={paymentIssue?.allocationMethod === entry}
                          aria-describedby={paymentIssue?.allocationMethod === entry ? paymentErrorId : undefined}
                          onChange={(event) => {
                            setAllocations((previous) => ({ ...previous, [entry]: event.target.value }));
                            setPaymentIssue(null);
                          }} />
                      </label>)}
                      {allocation && difference !== null && <dl className="pos-payment-balances">
                        <div><dt>{onCredit ? 'Anticipo asignado' : 'Total asignado'}</dt><dd>{display(decimalText(allocation.paid))}</dd></div>
                        <div><dt>{difference < 0n ? 'Excedente' : onCredit ? 'Saldo a fiar' : 'Pendiente'}</dt>
                          <dd>{display(decimalText(difference < 0n ? -difference : difference))}</dd></div>
                      </dl>}
                      {paymentIssue?.field === 'allocations' && <p id={paymentErrorId} role="alert" className="pos-operation-error">{paymentIssue.message}</p>}
                    </fieldset>}
                    {showTender && <div className="pos-cash-fields">
                      <label>Efectivo recibido
                        <input name="cashReceived" inputMode="decimal" value={tender} maxLength={19} disabled={locked} data-payment-focus="cash"
                          aria-invalid={Boolean(cashIssue)} aria-describedby={cashIssue ? instanceId + '-cash-error' : undefined}
                          onChange={(event) => {
                            setTender(event.target.value);
                            setPaymentIssue(null);
                          }} />
                      </label>
                      <p className="pos-change">Vuelto estimado <strong>{cash && !(cash instanceof PaymentIssue) ? display(decimalText(cash.difference)) : '—'}</strong></p>
                      {cashIssue && <p id={instanceId + '-cash-error'} role="alert" className="pos-operation-error">
                        {cashIssue.message}{cashIssue.missing !== undefined && ' Faltan ' + display(decimalText(cashIssue.missing)) + '.'}
                      </p>}
                    </div>}
                  </div>
                </form>
                {paymentVisible && onCredit && !reservation && <div className="pos-payment-customer" tabIndex={-1} data-payment-focus="customer"
                  aria-describedby={paymentIssue?.field === 'customer' ? paymentErrorId : undefined}>
                  {paymentIssue?.field === 'customer' && <p id={paymentErrorId} role="alert" className="pos-operation-error">{paymentIssue.message}</p>}
                  <CustomerPicker storeId={storeId} disabled={locked} selection={customer} onSelect={(selected) => {
                    setCustomer(selected);
                    setPaymentIssue(null);
                  }} />
                </div>}
                {paymentVisible && error && !uncertain && <p role="alert" className="pos-operation-error">{error}</p>}
                {paymentVisible && rejection && <p ref={rejectionNotice} tabIndex={-1} role="alert" className="pos-operation-error pos-payment-rejection"><strong>Venta rechazada.</strong> {rejection}</p>}
              </div>
              <div className="pos-cart-footer">
                {!paymentVisible && discountSummary}
                <p className="pos-total">{paymentVisible ? 'Total a cobrar' : 'Total'} <strong>{cartValid && (!paymentVisible || due >= 0n) ? display(decimalText(paymentVisible ? due : total)) : 'Pendiente'}</strong></p>
                {checkoutBlocked && <p id={checkoutBlockId} className="pos-validation-note" role="status">{checkoutBlocked}</p>}
                <div role="status" className="pos-payment-progress">{busy && confirming.current ? 'Confirmando la venta…' : ''}</div>
                {!paymentVisible && <button type="button" className="pos-primary" disabled={locked || !canCheckout}
                  aria-describedby={checkoutBlocked ? checkoutBlockId : undefined} onClick={openCheckout}>Ir a cobrar</button>}
                {paymentVisible && !uncertain && <div className="actions">
                  <button type="submit" className="pos-primary" form={instanceId + "-pos-checkout"} disabled={locked || !canCheckout} aria-describedby={checkoutBlocked ? checkoutBlockId : undefined}>Confirmar venta</button>
                </div>}
                {uncertain && <div className="pos-retry">
                  {error && <p role="alert"><strong>Resultado incierto.</strong> {error}</p>}
                  <button ref={retryButton} type="button" disabled={busy} onClick={() => {
                    if (attempt.current) void confirm(attempt.current);
                  }}>Reintentar confirmación</button>
                </div>}
              </div>
            </>}
          </div>
          <div className="pos-compact-access">
            <button type="button" className="pos-secondary" onClick={() => showTransaction(paymentVisible)}>
              <span>{uncertain ? 'Resolver cobro' : 'Ver carrito y cobrar'} · {cartUnits} {cartUnits === 1 ? 'unidad' : 'unidades'}</span>
              <strong>{!cart.length ? display('0.00') : cartValid ? display(decimalText(total)) : 'Total pendiente'}</strong>
            </button>
          </div>
        </div>}
        {sale && <section className="pos-result" aria-label="Resultado de la venta">
          <h3 ref={resultHeading} tabIndex={-1}>Venta confirmada N.º {sale.number}</h3>
          <dl className="catalog-data">
            <dt>Total</dt><dd>{display(sale.total)}</dd>
            <SalePaymentBreakdown sale={sale} display={display} />
          </dl>
          <div className="actions">
            <button type="button" disabled={ticketBusy} onClick={(event) => void openTicket(sale.id, event.currentTarget)}>Imprimir ticket</button>
            <button type="button" className="pos-secondary" onClick={() => {
              setSale(null);
              showCatalog(true);
            }}>Nueva venta</button>
          </div>
        </section>}
        {registers && <Reservations storeId={storeId} locations={locations} registers={registers} revision={historyRevision}
          disabled={busy || uncertain || collectionLocked || returnLocked || Boolean(reservation)} onPending={(pending) => { setReservationLocked(pending); onPending(pending); }}
          onChanged={onSold} onCheckout={(item) => void loadReservation(item)} />}
      </section>
      <section hidden={view !== 'history'} aria-label="Historial de ventas">
        {!registers && setup.cash.status === 'loading' && <p role="status">Cargando historial…</p>}
        {registers && <SalesHistory active={active && view === 'history'} storeId={storeId} timezone={registers.timezone} revision={historyRevision}
          ticketBusy={ticketBusy} locations={allLocations} registers={registers} disabled={busy || uncertain || collectionLocked || reservationLocked}
          onReturnPending={(pending) => { setReturnLocked(pending); onPending(pending, 'ventas'); }}
          onReturned={onSold} onTicket={(id, opener) => void openTicket(id, opener)} />}
      </section>
      <section hidden={view !== 'customers'} aria-label="Clientes y fiados">
        {!registers && setup.cash.status === 'loading' && <p role="status">Cargando clientes…</p>}
        {registers && <Receivables active={active && view === 'customers'} showAccounts={customerAccounts} key={storeId} storeId={storeId} revision={historyRevision} ticketBusy={ticketBusy} registers={registers}
          disabled={busy || uncertain || reservationLocked || returnLocked} onPending={(pending) => { setCollectionLocked(pending); onPending(pending, 'clientes'); }}
          onCollected={onSold} onTicket={(id, opener) => void openTicket(id, opener)} />}
      </section>
      {ticketError && <p role="alert">{ticketError}</p>}
      {ticket && <ticket.Dialog key={ticket.data.sale.id} ticket={ticket.data} onClose={closeTicket} />}
    </>
  );
}

function validQuantity(value: string) {
  const quantity = Number(value);
  return Number.isInteger(quantity) && quantity >= 1 && quantity <= 2147483647;
}
