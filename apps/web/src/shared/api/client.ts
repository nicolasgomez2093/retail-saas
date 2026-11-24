import { httpErrorMessage } from '../lib/httpErrorMessage';
import { trackNavigationRead } from './navigationAccess';

export type StaffRole = 'CASHIER' | 'INVENTORY_OPERATOR' | 'STORE_MANAGER';
export type Staff = {
  id: string;
  name: string;
  email: string;
  status: 'active' | 'inactive';
  roles: string[];
  storeIds: string[];
};
export type Directory = { items: Staff[]; stores: Store[]; roles: StaffRole[]; canChangeRoles: boolean };
export type NewStaff = { name: string; email: string; password: string; role: string; storeIds: string[] };
export type StaffChanges = Partial<Pick<Staff, 'name' | 'status' | 'storeIds'>> & { role?: string };

export type Identity = { tenantId: string; userId: string; name: string };
export type SupplierInput = { name: string; email: string; phone: string; taxId: string; contactName: string; allowDuplicateName?: boolean };
export type SupplierStatus = 'active' | 'inactive';
export type Supplier = Omit<SupplierInput, 'allowDuplicateName'> & { id: string; status: SupplierStatus };
export type SupplierPage = { items: Supplier[]; hasMore: boolean; canManage: boolean };
export type PurchaseOrderInput = {
  storeId: string; supplierId: string; reference?: string; items: { variantId: string; quantity: number; unitCost: string | null }[];
};
export type PurchaseOrderStatus = 'OPEN' | 'PARTIALLY_RECEIVED' | 'RECEIVED' | 'CANCELLED';
export type PurchaseOrderItem = PurchaseOrderInput['items'][number] & {
  id: string; productId: string; product: string; sku: string; size: string; color: string; received: number; rejected: number;
};
export type PurchaseOrder = Omit<PurchaseOrderInput, 'items' | 'reference'> & {
  id: string; number: number; reference: string; supplierName: string; currency: string; status: PurchaseOrderStatus; orderedAt: string; createdBy: string;
  cancelledAt: string | null; cancelReason: string; items: PurchaseOrderItem[];
  /** What the order costs at its agreed unit costs; null while a line has no cost. */
  payable: string | null; paid: string; balance: string | null; paymentStatus: SupplierPaymentStatus; payments: SupplierPayment[];
};
export type SupplierPaymentStatus = 'unknown' | 'none' | 'unpaid' | 'partial' | 'paid' | 'credit';
export type SupplierPaymentMethod = 'cash' | 'transfer' | 'check' | 'card' | 'other';
export type SupplierPayment = {
  id: string; method: SupplierPaymentMethod; amount: string; paidOn: string; checkDueOn: string | null; reference: string; note: string;
  createdBy: string; createdAt: string; voidedAt: string | null; voidReason: string;
};
export type SupplierPaymentInput = {
  storeId: string; method: SupplierPaymentMethod; amount: string; paidOn: string; checkDueOn?: string; reference?: string; note?: string;
};
export type PurchaseOrderPage = {
  items: PurchaseOrder[]; hasMore: boolean; canManage: boolean; timezone: string; currency: string;
  summary: { orderCount: number; orderedUnits: number; receivedUnits: number; pendingUnits: number; pendingOrders: number; receivedOrders: number;
    payable: string; paid: string; balance: string; ordersWithBalance: number; ordersWithoutTotal: number; checksToClear: string; checksToClearCount: number };
};
export type RejectionReason = 'damaged' | 'wrong_item' | 'excess' | 'other';
export type PurchaseOrderStatusFilter = '' | 'pending' | PurchaseOrder['status'];
export type PurchaseOrderFilters = { supplierId?: string; status: PurchaseOrderStatusFilter; payment?: '' | 'due' | 'paid'; query: string; fromDate: string; toDate: string };
export type PurchaseReceiptInput = {
  storeId: string; locationId: string; note: string;
  items: { orderItemId: string; accepted: number; rejected: number; unitCost?: string | null; rejectionReason?: RejectionReason }[];
};
export type PurchaseReceipt = {
  id: string; orderId: string; locationId: string; locationName: string; note: string; receivedBy: string; receivedAt: string;
  items: (Omit<PurchaseReceiptInput['items'][number], 'rejectionReason'> & {
    variantId: string; sku: string; product: string; size: string; color: string; unitCost: string | null;
    rejectionReason: RejectionReason | null; stockBefore: number | null; stockAfter: number | null;
  })[];
};
export type PurchaseReceiptPage = {
  items: PurchaseReceipt[]; hasMore: boolean;
  summary: {
    receiptCount: number; acceptedWithCost: number; acceptedWithoutCost: number; rejectedUnits: number;
    knownValue: string | null; totalValue: string | null;
  };
};
export type Store = { id: string; code: string; name: string };
export type ManagedStore = Store & { address: string; status: 'active' | 'inactive' };
export type StoreData = Pick<ManagedStore, 'code' | 'name' | 'address'>;
export type Credentials = { email: string; password: string };
export type LoginChoice = { token: string; commerces: { id: string; name: string; code: string }[] };

export type Term = { id: string; name: string };
export type ClassificationKind = 'categories' | 'brands';
export type Classifications = Record<ClassificationKind, Term[]>;
export type ClassificationIDs = { categoryId: string | null; brandId: string | null };
export type Product = ClassificationIDs & {
  revision: number;
  id: string; name: string; description: string; status: 'active' | 'inactive';
};
export type ProductData = Pick<Product, 'name' | 'description'> & Partial<ClassificationIDs>;
export type AttributeKind = 'sizes' | 'colors' | 'size-groups';
export type AttributeData = {
  name: string; code: string | null; hex: string | null; position: number;
  status: 'active' | 'inactive'; groupId: string | null;
};
export type CatalogAttribute = AttributeData & { id: string; revision: number };
export type PendingVariant = Variant & { productName: string };

export type IdentifierSettings = { skuPrefix: string; skuWidth: number; revision: number };
export type SKUSuggestion = { sku: string; ordinal: number };
export type IdentifierEvent = { value: string; variantId: string; action: 'retired' | 'reused'; internal: boolean; actorId: string | null; reason: string; createdAt: string };
export type VariantCreateData = VariantData & {
  generateInternal?: boolean; reuseRetired?: boolean; reason?: string; confirmSensitive?: boolean;
};
export type BatchAttributeRef = { id: string; revision: number };
export type BatchVariantInput = { size: BatchAttributeRef | null; color: BatchAttributeRef | null;
  sku: string; generateInternal: boolean; status: Variant['status'] };
export type VariantBatchIssue = { row: number; field: string; code: string; message: string;
  existing?: { id: string; sku: string; status: Variant['status']; product?: string } };
export type BatchPreviewRow = { row: number; sku: string; size: string; color: string; issues: VariantBatchIssue[] };
export type VariantBatchResult = { id: string; variants: Variant[] };
export type VariantData = { sku: string; size: string; color: string; barcodes: string[]; sizeId?: string | null; colorId?: string | null };
export type VariantOption = { name: string; value: string };
export type Variant = VariantData & { id: string; productId: string; revision: number; status: 'active' | 'inactive'; internalBarcode?: string | null; options?: VariantOption[] };
export type QuickAttributeRole = 'size' | 'color' | 'option';
export type QuickAttribute = { name: string; role: QuickAttributeRole };
export type QuickVariantInput = {
  values: string[]; sku: string; barcode: { mode: 'internal' | 'existing'; code: string };
  price: string | null; quantity: number;
};
export type QuickProductInput = {
  product: ProductData; storeId: string; locationId: string;
  attributes: QuickAttribute[]; variants: QuickVariantInput[];
};
export type QuickProductResult = {
  product: Product; attributes: QuickAttribute[]; storeId: string; locationId: string | null;
  priceListId: string | null; currency: string; units: number;
  variants: { variant: Variant; price: string | null; quantity: number; movementId: string | null }[];
};
export type ProductPhoto = { id: string; width: number; height: number; primary: boolean };
export type ProductDetail = { product: Product; variants: Variant[]; images: ProductPhoto[] };
export type CatalogPreview = {
  productId: string; photoId: string | null; currency: string | null; minPrice: string | null; maxPrice: string | null;
  pricePartial: boolean; stockReadable: boolean; available: number | null;
};
export type CatalogPage = { items: Product[]; hasMore: boolean; canManage: boolean; previews?: CatalogPreview[] };
export type BarcodeMatch = { product: Product; variant: Variant };

export type PriceList = { id: string; name: string };
export type PriceAssignment = Store & { listId: string | null };
export type PriceConfiguration = { currency: string; lists: PriceList[]; stores: PriceAssignment[] };
export type PriceItem = { amount: string | null };
export type ResolvedPrice = PriceItem & {
  currency: string;
  listId: string | null;
  reason: 'configured' | 'no_list' | 'no_price';
};

export type LocationKind = 'sales_floor' | 'warehouse';
export type LocationInput = { storeId: string; code: string; name: string; type: LocationKind; isSellable: boolean };
export type StockLocation = LocationInput & { id: string };
export type LocationPage = { items: StockLocation[]; canManage: boolean; canAdjust: boolean; canManageMinimums: boolean; timezone: string };
export type ExistenceRow = {
  productId: string; product: string; productStatus: 'active' | 'inactive';
  variantId: string | null; variantStatus: 'active' | 'inactive' | null; sku: string; size: string; color: string;
  onHand: number | null; reserved: number | null; sellable: number | null; lowStock: boolean; recordedLocations: number; minimum: number | null;
  barcode?: string;
  /** Branch average cost and its value for the units on hand; null when unknown or hidden. */
  unitCost?: string | null; costValue?: string | null; unitPrice?: string | null; retailValue?: string | null;
};
export type ExistenceAccess = { canAdjust: boolean; canManageMinimums: boolean; canManageLocations: boolean; canTransfer: boolean; canCatalog: boolean;
  canSeeCosts?: boolean; canSetCosts?: boolean };
export type ExistencePage = {
  items: ExistenceRow[]; hasMore: boolean; access: ExistenceAccess;
  summary: { productsWithStock: number; variantsSellable: number; variantsZero: number; variantsUnrecorded: number; variantsLow: number;
    units?: number; unitsWithoutCost?: number; costValue?: string | null; unitsWithoutPrice?: number; retailValue?: string; currency?: string };
};
export type CostRevision = { variantId: string; previousUnitCost: string | null; unitCost: string; quantityOnHand: number };
export type ExistenceDetail = { item: ExistenceRow; access: ExistenceAccess; currency?: string;
  locations: { id: string; name: string; code: string; active: boolean; isSellable: boolean; onHand: number | null;
    reserved: number | null; sellable: number | null; minimum: number | null; lowStock: boolean;
    revision: string | null; minimumVersion: string | null }[];
};
export type StockBalance = {
  locationId: string; variantId: string; sku: string; product: string; onHand: number; reserved: number; available: number;
  minimum: number | null; lowStock: boolean; active: boolean; currency: string; costStatus: 'known' | 'unknown';
  unitCost: string | null; inventoryValue: string | null;
};
export type StockAdjustment = { storeId: string; locationId: string; variantId: string; delta: number; reason: string; expectedRevision?: string };
export type InventoryValuationAmounts = {
  unitsWithCost: number; unitsWithoutCost: number; knownValue: string | null; totalValue: string | null;
};
export type InventoryValuation = InventoryValuationAmounts & {
  currency: string;
  locations: (InventoryValuationAmounts & { locationId: string; code: string; name: string })[];
};
export type StockMovement = {
  id: string; variantId: string; locationId: string; actorId: string; sku: string; type: string;
  delta: number; before: number; after: number; reason: string; createdAt: string; transferId: string | null; countId: string | null; saleId: string | null; returnId: string | null; exchangeId: string | null; purchaseReceiptId?: string;
};

export type MovementLocation = { id: string; name: string; code: string; storeName: string; active: boolean; historical?: boolean };
export type HistoryMovement = StockMovement & {
  product: string; size: string; color: string; productSnapshot: boolean; actorName: string; location: MovementLocation;
  origin: MovementLocation | null; destination: MovementLocation | null; otherEndHidden: boolean;
  purchaseOrderId: string | null; saleNumber: string | null;
};
export type MovementHistory = { items: HistoryMovement[]; locations: MovementLocation[]; timezone: string; older?: string; newer?: string };
export type MovementHistoryDetail = { item: HistoryMovement; timezone: string };

export type TransferLocation = { id: string; storeId: string; name: string; code: string; storeName: string };
export type TransferStatus = 'DRAFT' | 'DISPATCHED' | 'RECEIVED' | 'CANCELLED';
export type TransferSummary = {
  id: string; origin: TransferLocation; destination: TransferLocation; status: TransferStatus; createdAt: string;
  dispatchedAt: string | null; receivedAt: string | null; cancelledAt: string | null; receiptReason: string;
};
export type TransferQuantity = { variantId: string; quantity: number };
export type Transfer = TransferSummary & { items: (TransferQuantity & { sku: string; received: number | null })[] };
export type TransferPage = { items: TransferSummary[]; destinations: TransferLocation[]; canTransfer: boolean };
export type TransferInput = { storeId: string; originLocationId: string; destinationLocationId: string; items: TransferQuantity[] };

export type CountStatus = 'DRAFT' | 'COMPLETED' | 'CANCELLED';
export type CountSummary = {
  id: string; locationId: string; status: CountStatus; createdBy: string; createdAt: string; completedBy: string | null;
  completedAt: string | null; cancelledAt: string | null; note: string;
  location: string; locationActive: boolean; store: string; creator: string; closer: string | null;
  historicalLabels: boolean; revision: number; lineCount: number; countedCount: number;
};
export type CountLine = {
  variantId: string; sku: string; product: string; size: string; color: string; expected: number;
  counted: number | null; note: string; historicalLabels: boolean; expectedRecorded: boolean;
};
export type InventoryCount = CountSummary & { items: CountLine[]; movements: { id: string; variantId: string; delta: number }[] };
export type CountInput = { storeId: string; locationId: string; variantIds?: string[]; draft?: boolean; note?: string };
export type CountHistory = { items: CountSummary[]; locations: { id: string; name: string; active: boolean }[]; timezone: string; canCount: boolean; hasMore: boolean };
export type CountDraft = { storeId: string; expectedRevision: number; items: { variantId: string; counted: number | null; note: string }[] };
export type CountClose = { storeId: string; expectedRevision: number; references: { variantId: string; stockRevision: string }[] };
export type CountReview = { revision: number; items: { variantId: string; stockRevision: string; current: number | null; reserved: number; counted: number; delta: number; blocked: boolean }[] };

export type CashRegister = { id: string; code: string; name: string; occupied: boolean; openSessionId: string | null };
export type CashRegisters = { items: CashRegister[]; canManage: boolean; canDiscount?: boolean; currency: string; timezone: string };
export type CashSession = {
  id: string; registerId: string; openedBy: string; openedAt: string; openingCash: string; status: 'OPEN' | 'CLOSED';
  expectedCash: string; declaredCash: string | null; difference: string | null; closedBy: string | null; closedAt: string | null;
};
export type CashMovement = {
  id: string; sessionId: string; actorId: string; type: string; amount: string; reason: string; createdAt: string; saleId: string | null;
};
export type CashMovementInput = { storeId: string; type: 'manual_in' | 'manual_out'; amount: string; reason: string };

export type Customer = { id: string; name: string; phone: string };
export type CustomerSummary = { customerId: string; lastPurchase: string | null; balances: { currency: string; amount: string }[] };
export type CustomerPage = { items: Customer[]; hasMore: boolean; summaries?: CustomerSummary[]; timezone?: string };
export type SaleCredit = { id: string; customerId: string; customerName: string; originalAmount: string };
export type Receivable = SaleCredit & {
  saleId: string; saleNumber: string; currency: string; outstandingAmount: string; createdAt: string;
};
/** partial: still owed, with payments or returns already credited. */
export type ReceivableStatus = 'pending' | 'partial' | 'settled';
export type ReceivableFilters = { status: ReceivableStatus; query: string; customerId?: string };
export type ReceivableSummary = {
  currency: string; pendingCount: number; settledCount: number; outstanding: string; collected: string; debtCredits: string;
  paymentsByMethod: { method: SalePaymentMethod; paymentCount: number; amount: string }[];
  topDebtors: { customerId: string; customerName: string; pendingCount: number; outstanding: string }[];
};
export type RecentReceivablePayment = SalePayment & {
  id: string; saleId: string; saleNumber: string; customerName: string; currency: string; createdAt: string;
};
export type ReceivablePage = {
  items: Receivable[]; hasMore: boolean; summary: ReceivableSummary[]; recentPayments: RecentReceivablePayment[];
};
export type ReceivablePaymentInput = { storeId: string; cashSessionId: string; method: SalePaymentMethod; amount: string };
export type ReceivablePayment = SalePayment & {
  id: string; receivableId: string; cashSessionId: string; balanceAfter: string; createdBy: string; createdAt: string;
};
export type ReceivablePaymentPage = { items: ReceivablePayment[]; debtCredits: ReturnDebtCredit[]; hasMore: boolean; outstandingAmount: string };

export type SalePaymentMethod = 'cash' | 'transfer' | 'debit' | 'credit' | 'qr';
export type SalePayment = { method: SalePaymentMethod; amount: string };
export type ReservationDepositInput = { storeId: string; cashSessionId: string; method: SalePaymentMethod; amount: string };
export type ReservationDeposit = SalePayment & { id: string; cashSessionId: string; createdBy: string; createdAt: string };
export type ReservationStatus = 'ACTIVE' | 'FULFILLED' | 'CANCELLED' | 'EXPIRED';
export type ReservationInput = {
  storeId: string; locationId: string; customerId: string; expiresAt?: string;
  items: { variantId: string; quantity: number }[];
};
export type Reservation = Omit<ReservationInput, 'expiresAt' | 'items'> & {
  id: string; locationName: string; customerName: string; status: ReservationStatus;
  createdAt: string; expiresAt: string | null; closedAt: string | null; saleId: string | null; deposit: ReservationDeposit | null;
  items: { variantId: string; quantity: number; product: string; sku: string; size: string; color: string }[];
};

export type SaleInput = {
  storeId: string; cashSessionId: string; locationId: string; reservationId?: string; expectedDeposit?: string;
  items: { variantId: string; quantity: number; expectedUnitPrice: string; discountPercent?: number }[];
} & (
  { cashReceived: string } |
  { payments: (SalePayment & { cashReceived?: string })[]; onCredit?: true; customerId?: string }
);
export type Sale = {
  id: string; storeId: string; cashSessionId: string; locationId: string; number: string; currency: string;
  total: string; cashReceived: string; change: string; createdBy: string; createdAt: string; payments: SalePayment[]; credit?: SaleCredit; deposit?: SalePayment;
  items: { variantId: string; product: string; sku: string; size: string; color: string; quantity: number; unitPrice: string; subtotal: string; discountPercent?: number; listUnitPrice?: string | null }[];
};

export type ReturnDisposition = 'sellable' | 'quarantine';
export type ReturnQuantity = { variantId: string; locationId: string; disposition: ReturnDisposition; quantity: number };
export type ReturnInput = { storeId: string; reason: string; items: ReturnQuantity[]; sourceExchangeId?: string };
export type ReturnDebtCredit = { id: string; returnId: string; amount: string; balanceAfter: string; createdBy: string; createdAt: string };
export type ReturnRefund = { id: string; returnId: string; cashSessionId: string; method: SalePaymentMethod | null; amount: string; createdBy: string; createdAt: string; payments: SalePayment[] };
type ExchangeChargePayment = SalePayment & { cashReceived?: string };
export type ExchangeCharge = {
  cashSessionId: string; method: SalePaymentMethod | null; amount: string; cashReceived?: string; change?: string;
  payments: (ExchangeChargePayment & { change?: string })[];
};
export type ReturnExchangeInput = Pick<SaleInput, 'storeId' | 'locationId' | 'items'> & {
  debtCreditAmount?: string;
  charge?: { cashSessionId: string } & (ExchangeChargePayment | { payments: ExchangeChargePayment[] });
  refund?: { cashSessionId: string; payments: SalePayment[] };
};
export type ReturnExchange = {
  id: string; returnId: string; locationId: string; locationName: string; total: string;
  createdBy: string; createdAt: string; items: Sale['items']; charge?: ExchangeCharge; returnedQuantities?: Record<string, number>;
};
export type SaleReturn = {
  id: string; saleId: string; reason: string; referenceAmount: string; createdBy: string; createdAt: string; sourceExchangeId?: string; exchangeRefundOptions?: SalePayment[]; refund?: ReturnRefund; debtCredits?: ReturnDebtCredit[]; exchange?: ReturnExchange;
  items: (ReturnQuantity & { locationName: string; product: string; sku: string; unitPrice: string })[];
};
export type ReturnHistory = {
  number: string; currency: string; hasMore: boolean; canRefundCash: boolean; refundMethod: SalePaymentMethod | null; refundOptions: SalePayment[]; deposit?: SalePayment; outstandingDebt?: string; returns: SaleReturn[];
  items: { variantId: string; product: string; sku: string; size: string; color: string; sold: number; returned: number; unitPrice: string }[];
};
export type SaleSummary = Pick<Sale, 'id' | 'number' | 'total' | 'currency' | 'createdAt'> & {
  customer?: string | null; units?: number; products?: string; returns?: number; onCredit?: boolean;
  /** Methods charged at checkout, including a reservation deposit. */
  payments?: SalePaymentMethod[];
};
export type SaleHistoryFilters = { number: string; query: string; fromDate: string; toDate: string; method?: SaleMethodFilter };
/** A checkout method, or on_credit for fiado sales; empty keeps every sale. */
export type SaleMethodFilter = '' | SalePaymentMethod | 'on_credit';
/** One complete exchange or return, settled in a single server transaction. */
export type ReturnOperationInput = {
  storeId: string; reason: string; items: ReturnQuantity[];
  exchange?: { locationId: string; items: SaleInput['items'] };
  charge?: ReturnExchangeInput['charge'];
  refund?: { cashSessionId: string; payments: SalePayment[] };
  debtCreditAmount?: string;
};
export type ReturnPreview = {
  currency: string; returnAmount: string; replacementTotal: string; replacements: Sale['items'];
  difference: string; charge: string; debtCredit: string; refund: string; outstandingDebt: string | null; refundOptions: SalePayment[];
};
export type SalesMarginLine = {
  saleId: string; saleNumber: string; soldAt: string; currency: string; variantId: string; sku: string; product: string; quantity: number;
  netSales: string; costStatus: 'known' | 'unknown'; unitCostSnapshot: string | null; costOfGoods: string | null;
  grossProfit: string | null; grossMarginPercent: string | null;
};
export type SalesMarginSummary = {
  currency: string; lineCount: number; unknownCostLines: number;
  netSales: string; netSalesWithCost: string; netSalesWithoutCost: string;
  costOfGoodsWithCost: string | null; grossProfitWithCost: string | null; grossMarginPercentWithCost: string | null;
};
export type TopSoldVariant = { variantId: string; sku: string; product: string; size: string; color: string; unitsSold: number };
export type SalesPaymentTotal = { currency: string; method: SalePaymentMethod | 'on_credit'; amount: string; saleCount: number };
export type SalesCategoryTotal = { date: string; currency: string; categoryId: string | null; categoryName: string | null; netSales: string };
/** Sales netted with the returns and exchange replacements dated in the period. */
export type SalesActivity = {
  currency: string; saleCount: number; unitsSold: number; grossSales: string; averageTicket: string | null;
  returnCount: number; returnedUnits: number; returnedAmount: string;
  exchangeCount: number; exchangedUnits: number; exchangeAmount: string; netUnits: number; netSales: string;
};
export type TopSoldProduct = { currency: string; productId: string; product: string; unitsSold: number; returnedUnits: number; netUnits: number; netSales: string };
export type SalesDailyTotal = { date: string; currency: string; saleCount: number; unitsSold: number; netSales: string };
export type SalesMarginPage = {
  items: SalesMarginLine[]; hasMore: boolean; timezone: string; summary: SalesMarginSummary[]; topVariants: TopSoldVariant[];
  paymentTotals: SalesPaymentTotal[];
  dailyTotals: SalesDailyTotal[]; categoryTotals: SalesCategoryTotal[]; activity: SalesActivity[]; topProducts: TopSoldProduct[];
};
export type SalesMarginFilters = { fromDate: string; toDate: string; query: string; method?: SaleMethodFilter };
export type CollectionSource = 'sale' | 'deposit' | 'receivable' | 'exchange' | 'refund';
/** Money received or refunded in the period, on the day it moved and by its real method. */
export type CollectionsReport = {
  timezone: string; hasMore: boolean;
  methods: { currency: string; method: SalePaymentMethod; collected: string; refunded: string; net: string; count: number }[];
  sources: { currency: string; source: CollectionSource; amount: string; count: number }[];
  daily: { date: string; currency: string; collected: string; refunded: string }[];
  items: { source: CollectionSource; method: SalePaymentMethod; currency: string; amount: string; happenedAt: string;
    saleId: string | null; saleNumber: string | null; customer: string | null }[];
};
export type CollectionsFilters = { fromDate: string; toDate: string; method: '' | SalePaymentMethod };
/** Goods received in the period, valued at the cost captured on each receipt. */
export type PurchasesReport = {
  timezone: string; currency: string; hasMore: boolean;
  summary: { receiptCount: number; receivedOrders: number; units: number; unitsWithCost: number; rejectedUnits: number;
    knownCost: string; placedOrders: number; orderedUnits: number; pendingOrders: number };
  daily: { date: string; receiptCount: number; units: number; knownCost: string }[];
  suppliers: { supplierId: string; supplier: string; receiptCount: number; units: number; knownCost: string }[];
  products: { productId: string; product: string; units: number; knownCost: string }[];
  items: { receiptId: string; orderId: string; orderNumber: number | null; supplier: string; locationName: string; receivedAt: string;
    units: number; rejectedUnits: number; knownCost: string; unitsWithoutCost: number }[];
};
export type SaleHistoryPage = { items: SaleSummary[]; hasMore: boolean };
export type SaleTicket = {
  sale: Sale; merchantName: string; storeCode: string; storeName: string; storeAddress: string; timezone: string;
};

export class ApiError extends Error {
  constructor(public readonly status: number, message: string, public readonly code?: string, public readonly field?: string,
    public readonly rows?: VariantBatchIssue[], public readonly variantIds?: string[],
    /** Existing record reported by a duplicate conflict, such as a supplier with the same name. */
    public readonly existing?: { id: string; name: string }) {
    super(message);
  }
}

async function fetchAPI(path: string, init?: RequestInit): Promise<Response> {
  const finishAccess = trackNavigationRead(path, init?.method);
  let response: Response;
  try {
    response = await fetch('/api/v1' + path, { ...init, credentials: 'same-origin', cache: 'no-store' });
  } catch {
    finishAccess?.(0);
    throw new ApiError(0, 'No pudimos conectar. Verificá tu conexión y volvé a intentar.');
  }
  finishAccess?.(response.status);
  if (!response.ok) {
    const body = await response.json().catch(() => undefined);
    throw new ApiError(response.status, httpErrorMessage(response.status, body?.error?.message), body?.error?.code, body?.error?.field, Array.isArray(body?.error?.rows) ? body.error.rows : undefined,
      Array.isArray(body?.error?.variantIds) ? body.error.variantIds : undefined,
      typeof body?.error?.supplier?.id === 'string' ? body.error.supplier : undefined);
  }
  return response;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetchAPI(path, init);
  return response.status === 204 ? undefined as T : response.json();
}

function json(method: string, body: unknown): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

function imagesPath(productId: string, photoId?: string) {
  return '/products/' + encodeURIComponent(productId) + '/images' +
    (photoId ? '/' + encodeURIComponent(photoId) : '');
}

function salesMarginParams(storeId: string, { fromDate, toDate, query, method }: SalesMarginFilters) {
  return new URLSearchParams({
    store_id: storeId,
    ...(fromDate ? { from_date: fromDate } : {}),
    ...(toDate ? { to_date: toDate } : {}),
    ...(query ? { q: query } : {}),
    ...(method ? { method } : {}),
  });
}

function receivableParams(storeId: string, { status, query, customerId }: ReceivableFilters) {
  return new URLSearchParams({ store_id: storeId, status, q: query, ...(customerId ? { customer_id: customerId } : {}) });
}

function purchaseOrderParams(storeId: string, filters: PurchaseOrderFilters) {
  return new URLSearchParams({
    store_id: storeId, status: filters.status, q: filters.query, from_date: filters.fromDate, to_date: filters.toDate,
    ...(filters.supplierId ? { supplier_id: filters.supplierId } : {}),
    ...(filters.payment ? { payment: filters.payment } : {}),
  });
}

export const api = {
  users: () => request<Directory>('/users'),
  createUser: (user: NewStaff) => request<Staff>('/users', json('POST', user)),
  updateUser: (id: string, changes: StaffChanges) => request<Staff>('/users/' + encodeURIComponent(id), json('PATCH', changes)),
  downloadCatalogCSV: async (kind: 'template' | 'export') => {
    const response = await fetchAPI('/catalog/csv/' + kind);
    return response.blob();
  },
  importCatalogCSV: (file: File) => request<{ products: number; variants: number }>('/catalog/csv/import', {
    method: 'POST', headers: { 'Content-Type': 'text/csv; charset=utf-8' }, body: file,
  }),
  classificationTerms: (kind: ClassificationKind, signal?: AbortSignal) => request<{ items: Term[] }>('/' + kind, { signal }),
  attributes: (kind: AttributeKind) => request<{ items: CatalogAttribute[] }>('/catalog/' + kind),
  saveAttribute: (kind: AttributeKind, data: AttributeData, target?: { id: string; revision: number }) =>
    request<CatalogAttribute>('/catalog/' + kind + (target ? '/' + encodeURIComponent(target.id) : ''),
      json(target ? 'PATCH' : 'POST', target ? { ...data, expectedRevision: target.revision } : data)),
  pendingAttributes: (q: string, offset: number) => request<{ items: PendingVariant[]; hasMore: boolean }>(
    '/catalog/pending-attributes?' + new URLSearchParams({ q, offset: String(offset) })),
  classifications: () => request<Classifications>('/catalog/classifications'),
  saveClassification: (kind: ClassificationKind, name: string, id?: string) =>
    request<Term>('/' + kind + (id ? '/' + encodeURIComponent(id) : ''), json(id ? 'PATCH' : 'POST', { name })),
  suppliers: (query: string, offset: number) => request<SupplierPage>(
    '/suppliers?' + new URLSearchParams({ q: query, offset: String(offset) })),
  createSupplier: (input: SupplierInput) => request<Supplier>('/suppliers', json('POST', input)),
  updateSupplier: (id: string, input: Partial<SupplierInput & { status: SupplierStatus }>) =>
    request<Supplier>('/suppliers/' + encodeURIComponent(id), json('PATCH', input)),
  purchaseOrders: (storeId: string, offset: number, filters: PurchaseOrderFilters) => request<PurchaseOrderPage>(
    '/purchase-orders?' + purchaseOrderParams(storeId, filters) + '&offset=' + offset),
  downloadPurchaseOrdersCSV: async (storeId: string, filters: PurchaseOrderFilters) => {
    const response = await fetchAPI('/purchase-orders/csv?' + purchaseOrderParams(storeId, filters));
    return response.blob();
  },
  createPurchaseOrder: (input: PurchaseOrderInput, key: string) => request<PurchaseOrder>('/purchase-orders', {
    ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
  }),
  purchaseOrder: (storeId: string, orderId: string) => request<PurchaseOrder>(
    '/purchase-orders/' + encodeURIComponent(orderId) + '?' + new URLSearchParams({ store_id: storeId })),
  cancelPurchaseOrder: (orderId: string, storeId: string, reason: string) => request<PurchaseOrder>(
    '/purchase-orders/' + encodeURIComponent(orderId) + '/cancel', json('POST', { storeId, reason })),
  registerSupplierPayment: (orderId: string, input: SupplierPaymentInput, key: string) =>
    request<PurchaseOrder>('/purchase-orders/' + encodeURIComponent(orderId) + '/payments', {
      ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    }),
  voidSupplierPayment: (orderId: string, paymentId: string, storeId: string, reason: string) => request<PurchaseOrder>(
    '/purchase-orders/' + encodeURIComponent(orderId) + '/payments/' + encodeURIComponent(paymentId) + '/void', json('POST', { storeId, reason })),
  purchaseReceipts: (storeId: string, orderId: string, offset: number) => request<PurchaseReceiptPage>(
    '/purchase-orders/' + encodeURIComponent(orderId) + '/receipts?' + new URLSearchParams({ store_id: storeId, offset: String(offset) })),
  downloadPurchaseReceiptsCSV: async (storeId: string, orderId: string) => {
    const response = await fetchAPI('/purchase-orders/' + encodeURIComponent(orderId) + '/receipts/csv?' +
      new URLSearchParams({ store_id: storeId }));
    return response.blob();
  },
  receivePurchaseOrder: (orderId: string, input: PurchaseReceiptInput, key: string) =>
    request<PurchaseReceipt>('/purchase-orders/' + encodeURIComponent(orderId) + '/receipts', {
      ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    }),
  products: (query: string, offset: number, filters?: ClassificationIDs, storeId?: string) => request<CatalogPage>(
    '/products?' + new URLSearchParams({
      q: query, offset: String(offset), limit: '25',
      ...(storeId ? { store_id: storeId } : {}),
      ...(filters?.categoryId ? { categoryId: filters.categoryId } : {}),
      ...(filters?.brandId ? { brandId: filters.brandId } : {}),
    }),
  ),
  productPhotoURL: (productId: string, photoId: string) => '/api/v1' + imagesPath(productId, photoId),
  uploadProductPhoto: (productId: string, file: Blob) => request<{ images: ProductPhoto[] }>(imagesPath(productId), {
    method: 'POST', headers: { 'Content-Type': file.type }, body: file,
  }),
  markProductPhotoPrimary: (productId: string, photoId: string) => request<{ images: ProductPhoto[] }>(
    imagesPath(productId, photoId) + '/primary', { method: 'PUT' },
  ),
  deleteProductPhoto: (productId: string, photoId: string) => request<{ images: ProductPhoto[] }>(
    imagesPath(productId, photoId), { method: 'DELETE' },
  ),
  product: (id: string) => request<ProductDetail>('/products/' + encodeURIComponent(id)),
  createProduct: (product: ProductData) => request<Product>('/products', json('POST', product)),
  quickCreateProduct: (input: QuickProductInput, key: string) => request<QuickProductResult>('/quick-products', {
    ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
  }),
  updateProduct: (id: string, product: Partial<ProductData & Pick<Product, 'status'>> & { expectedRevision: number }) =>
    request<Product>('/products/' + encodeURIComponent(id), json('PATCH', product)),
  skuSuggestion: (after = 0) => request<SKUSuggestion>('/catalog/sku-suggestion?after=' + after),
  identifierSettings: () => request<IdentifierSettings>('/catalog/identifier-settings'),
  saveIdentifierSettings: (data: IdentifierSettings) => request<IdentifierSettings>('/catalog/identifier-settings',
    json('PATCH', { skuPrefix: data.skuPrefix, skuWidth: data.skuWidth, expectedRevision: data.revision })),
  identifierHistory: (kind: 'sku' | 'barcode', value: string) => request<{ items: IdentifierEvent[] }>(
    '/catalog/identifier-history?' + new URLSearchParams({ kind, value })),
  previewVariantBatch: (productId: string, rows: BatchVariantInput[]) =>
    request<{ rows: BatchPreviewRow[] }>('/products/' + encodeURIComponent(productId) + '/variants/preview', json('POST', { rows })),
  createVariantBatch: (productId: string, rows: BatchVariantInput[], key: string) =>
    request<VariantBatchResult>('/products/' + encodeURIComponent(productId) + '/variants/batch', {
      ...json('POST', { rows }), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    }),
  createVariant: (id: string, variant: VariantCreateData, key?: string) =>
    request<Variant>('/products/' + encodeURIComponent(id) + '/variants', {
      ...json('POST', variant), ...(key ? { headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } } : {}),
    }),
  updateVariant: (productId: string, variantId: string, data: Partial<VariantCreateData & Pick<Variant, 'status'>> & { expectedRevision: number; reason: string; confirmSensitive: boolean }) =>
    request<Variant>('/products/' + encodeURIComponent(productId) + '/variants/' + encodeURIComponent(variantId), json('PATCH', data)),
  barcode: (code: string) => request<BarcodeMatch>('/barcodes/' + encodeURIComponent(code) + '/lookup'),
  priceLists: () => request<PriceConfiguration>('/price-lists'),
  createPriceList: (name: string) => request<PriceList>('/price-lists', json('POST', { name })),
  assignPriceList: (storeId: string, listId: string | null) =>
    request<void>('/stores/' + encodeURIComponent(storeId) + '/price-list', json('PUT', { listId })),
  priceItem: (listId: string, variantId: string) => request<PriceItem>(
    '/price-lists/' + encodeURIComponent(listId) + '/items/' + encodeURIComponent(variantId),
  ),
  setPriceItem: (listId: string, variantId: string, amount: string | null) => request<PriceItem>(
    '/price-lists/' + encodeURIComponent(listId) + '/items/' + encodeURIComponent(variantId), json('PUT', { amount }),
  ),
  resolvePrice: (storeId: string, variantId: string) => request<ResolvedPrice>(
    '/stores/' + encodeURIComponent(storeId) + '/variants/' + encodeURIComponent(variantId) + '/price',
  ),
  locations: (storeId: string, signal?: AbortSignal) => request<LocationPage>('/locations?' + new URLSearchParams({ store_id: storeId }), { signal }),
  existences: (storeId: string, search: string, signal?: AbortSignal) => request<ExistencePage>(
    '/inventory/existences?' + new URLSearchParams({ store_id: storeId }) + (search ? '&' + search : ''), { signal }),
  existence: (storeId: string, variantId: string, signal?: AbortSignal) => request<ExistenceDetail>(
    '/inventory/existences/' + encodeURIComponent(variantId) + '?' + new URLSearchParams({ store_id: storeId }), { signal }),
  /** Every variant matching the Existencias filters (not only the page), as CSV. */
  downloadExistencesCSV: async (storeId: string, search: string) => {
    const response = await fetchAPI('/inventory/existences/csv?' + new URLSearchParams({ store_id: storeId }) + (search ? '&' + search : ''));
    return response.blob();
  },
  setInventoryCosts: (storeId: string, items: { variantId: string; unitCost: string }[], reason = '') =>
    request<{ items: CostRevision[] }>('/inventory/costs', json('PUT', { storeId, reason, items })),

  createLocation: (data: LocationInput) => request<StockLocation>('/locations', json('POST', data)),
  stockBalances: (storeId: string, locationId: string, lowOnly = false) => request<{ items: StockBalance[] }>(
    '/inventory?' + new URLSearchParams({ store_id: storeId, location_id: locationId, ...(lowOnly ? { low_stock: 'true' } : {}) }),
  ),
  inventoryValuation: (storeId: string) =>
    request<InventoryValuation>('/reports/inventory?' + new URLSearchParams({ store_id: storeId })),
  downloadInventoryValuationCSV: async (storeId: string) => {
    const response = await fetchAPI('/reports/inventory/csv?' + new URLSearchParams({ store_id: storeId }));
    return response.blob();
  },
  setStockMinimum: (storeId: string, locationId: string, variantId: string, minimum: number | null, expectedVersion?: string) =>
    request<void>('/inventory/minimums', json('PUT', { storeId, locationId, variantId, minimum, expectedVersion })),
  movementHistory: (storeId: string, query: string, signal: AbortSignal) => request<MovementHistory>(
    '/inventory/movements/history?' + new URLSearchParams({ store_id: storeId }) + (query ? '&' + query : ''), { signal }),
  movementDetail: (storeId: string, id: string, signal: AbortSignal) => request<MovementHistoryDetail>(
    '/inventory/movements/history/' + encodeURIComponent(id) + '?' + new URLSearchParams({ store_id: storeId }), { signal }),
  stockMovements: (storeId: string, locationId: string) => request<{ items: StockMovement[] }>(
    '/inventory/movements?' + new URLSearchParams({ store_id: storeId, location_id: locationId }),
  ),
  adjustmentResult: (storeId: string, key: string) => request<StockMovement>('/inventory/adjustments/result?' + new URLSearchParams({ store_id: storeId, key })),
  adjustStock: (data: StockAdjustment, key: string) => request<StockMovement>('/inventory/adjustments', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(data),
  }),
  inventoryCounts: (storeId: string, locationId: string) => request<{ items: CountSummary[] }>(
    '/inventory/counts?' + new URLSearchParams({ store_id: storeId, location_id: locationId }),
  ),
  countHistory: (storeId: string, query: string, signal?: AbortSignal) => request<CountHistory>(
    '/inventory/counts/history?' + new URLSearchParams({ store_id: storeId }) + (query ? '&' + query : ''), { signal }),
  saveCountDraft: (id: string, data: CountDraft) => request<InventoryCount>('/inventory/counts/' + encodeURIComponent(id) + '/draft', json('PUT', data)),
  reviewCount: (id: string, storeId: string) => request<CountReview>('/inventory/counts/' + encodeURIComponent(id) + '/review?' + new URLSearchParams({ store_id: storeId })),
  closeCount: (id: string, body: CountClose, key: string) => request<InventoryCount>('/inventory/counts/' + encodeURIComponent(id) + '/close', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(body),
  }),
  countCloseResult: (id: string, storeId: string, key: string) => request<InventoryCount>(
    '/inventory/counts/' + encodeURIComponent(id) + '/close-result?' + new URLSearchParams({ store_id: storeId, key })),
  inventoryCount: (storeId: string, id: string, signal?: AbortSignal) => request<InventoryCount>(
    '/inventory/counts/' + encodeURIComponent(id) + '?' + new URLSearchParams({ store_id: storeId }), { signal },
  ),
  startCount: (data: CountInput, key: string) => request<InventoryCount>('/inventory/counts', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(data),
  }),
  completeCount: (id: string, storeId: string, items: TransferQuantity[], note: string) =>
    request<InventoryCount>('/inventory/counts/' + encodeURIComponent(id) + '/complete', json('POST', { storeId, items, note })),
  cancelCount: (id: string, storeId: string) =>
    request<InventoryCount>('/inventory/counts/' + encodeURIComponent(id) + '/cancel', json('POST', { storeId })),
  transfers: (storeId: string) => request<TransferPage>('/transfers?' + new URLSearchParams({ store_id: storeId })),
  transfer: (storeId: string, id: string) => request<Transfer>(
    '/transfers/' + encodeURIComponent(id) + '?' + new URLSearchParams({ store_id: storeId }),
  ),
  createTransfer: (data: TransferInput, key: string) => request<Transfer>('/transfers', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(data),
  }),
  transferAction: (id: string, action: 'dispatch' | 'cancel', storeId: string) =>
    request<Transfer>('/transfers/' + encodeURIComponent(id) + '/' + action, json('POST', { storeId })),
  receiveTransfer: (id: string, storeId: string, items: TransferQuantity[], reason: string) =>
    request<Transfer>('/transfers/' + encodeURIComponent(id) + '/receive', json('POST', { storeId, items, reason })),
  cashRegisters: (storeId: string) => request<CashRegisters>('/cash-registers?' + new URLSearchParams({ store_id: storeId })),
  createCashRegister: (storeId: string, code: string, name: string) =>
    request<CashRegister>('/cash-registers', json('POST', { storeId, code, name })),
  cashSessions: (storeId: string, registerId: string) => request<{ items: CashSession[] }>(
    '/cash-sessions?' + new URLSearchParams({ store_id: storeId, register_id: registerId }),
  ),
  cashSession: (storeId: string, id: string) => request<CashSession>(
    '/cash-sessions/' + encodeURIComponent(id) + '?' + new URLSearchParams({ store_id: storeId }),
  ),
  openCash: (storeId: string, registerId: string, openingCash: string, key: string) => request<CashSession>('/cash-sessions', {
    ...json('POST', { storeId, registerId, openingCash }),
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
  }),
  closeCash: (storeId: string, id: string, declaredCash: string) =>
    request<CashSession>('/cash-sessions/' + encodeURIComponent(id) + '/close', json('POST', { storeId, declaredCash })),
  cashMovements: (storeId: string, id: string) => request<{ items: CashMovement[]; income?: string; outflow?: string; count?: number }>(
    '/cash-sessions/' + encodeURIComponent(id) + '/movements?' + new URLSearchParams({ store_id: storeId }),
  ),
  addCashMovement: (id: string, data: CashMovementInput, key: string) =>
    request<CashMovement>('/cash-sessions/' + encodeURIComponent(id) + '/movements', {
      ...json('POST', data), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    }),
  customers: (storeId: string, query: string, offset: number, summary = false) => request<CustomerPage>(
    '/customers?' + new URLSearchParams({ store_id: storeId, q: query, offset: String(offset), ...(summary ? { summary: 'true' } : {}) }),
  ),
  createCustomer: (storeId: string, name: string, phone: string) =>
    request<Customer>('/customers', json('POST', { storeId, name, phone })),
  updateCustomer: (storeId: string, customerId: string, name: string, phone: string) =>
    request<Customer>('/customers/' + encodeURIComponent(customerId), json('PATCH', { storeId, name, phone })),
  reservations: (storeId: string, status: ReservationStatus, offset: number) => request<{ items: Reservation[]; hasMore: boolean }>(
    '/reservations?' + new URLSearchParams({ store_id: storeId, status, offset: String(offset) }),
  ),
  createReservation: (input: ReservationInput, key: string) => request<Reservation>('/reservations', {
    ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
  }),
  addReservationDeposit: (id: string, input: ReservationDepositInput, key: string) => request<ReservationDeposit>(
    '/reservations/' + encodeURIComponent(id) + '/deposit',
    { ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  releaseReservation: (id: string, storeId: string, action: 'cancel' | 'expire') =>
    request<Reservation>('/reservations/' + encodeURIComponent(id) + '/' + action, json('POST', { storeId })),
  receivables: (storeId: string, filters: ReceivableFilters, offset: number) => request<ReceivablePage>(
    '/receivables?' + receivableParams(storeId, filters) + '&offset=' + offset,
  ),
  downloadReceivablesCSV: async (storeId: string, filters: ReceivableFilters) => {
    const response = await fetchAPI('/reports/receivables/csv?' + receivableParams(storeId, filters));
    return response.blob();
  },
  receivablePayments: (storeId: string, id: string, offset: number) => request<ReceivablePaymentPage>(
    '/receivables/' + encodeURIComponent(id) + '/payments?' + new URLSearchParams({ store_id: storeId, offset: String(offset) }),
  ),
  addReceivablePayment: (id: string, data: ReceivablePaymentInput, key: string) => request<ReceivablePayment>(
    '/receivables/' + encodeURIComponent(id) + '/payments',
    { ...json('POST', data), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  saleHistory: (storeId: string, number: string, offset: number, limit = 25, filters?: Omit<SaleHistoryFilters, 'number'>) => request<SaleHistoryPage>(
    '/sales?' + new URLSearchParams({ store_id: storeId, number, offset: String(offset), limit: String(limit),
      ...(filters ? { q: filters.query, from_date: filters.fromDate, to_date: filters.toDate } : {}),
      ...(filters?.method ? { method: filters.method } : {}) }),
  ),
  collectionsReport: (storeId: string, limit: number, offset: number, filters: CollectionsFilters) => request<CollectionsReport>(
    '/reports/collections?' + new URLSearchParams({ store_id: storeId, from_date: filters.fromDate, to_date: filters.toDate,
      ...(filters.method ? { method: filters.method } : {}), limit: String(limit), offset: String(offset) }),
  ),
  purchasesReport: (storeId: string, limit: number, offset: number, filters: { fromDate: string; toDate: string }) => request<PurchasesReport>(
    '/reports/purchases?' + new URLSearchParams({ store_id: storeId, from_date: filters.fromDate, to_date: filters.toDate,
      limit: String(limit), offset: String(offset) }),
  ),
  previewReturnOperation: (saleId: string, input: ReturnOperationInput) => request<ReturnPreview>(
    '/sales/' + encodeURIComponent(saleId) + '/return-operations/preview', json('POST', input)),
  addReturnOperation: (saleId: string, input: ReturnOperationInput, key: string) => request<SaleReturn>(
    '/sales/' + encodeURIComponent(saleId) + '/return-operations',
    { ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  salesMarginReport: (storeId: string, limit: number, offset: number, filters: SalesMarginFilters) => request<SalesMarginPage>(
    '/reports/sales?' + salesMarginParams(storeId, filters) +
    '&' + new URLSearchParams({ limit: String(limit), offset: String(offset) }),
  ),
  downloadSalesMarginCSV: async (storeId: string, filters: SalesMarginFilters) => {
    const response = await fetchAPI('/reports/sales/csv?' + salesMarginParams(storeId, filters));
    return response.blob();
  },
  saleReturns: (storeId: string, saleId: string, offset: number) => request<ReturnHistory>(
    '/sales/' + encodeURIComponent(saleId) + '/returns?' + new URLSearchParams({ store_id: storeId, offset: String(offset) }),
  ),
  addSaleReturn: (saleId: string, input: ReturnInput, key: string) => request<SaleReturn>(
    '/sales/' + encodeURIComponent(saleId) + '/returns',
    { ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  addReturnRefund: (saleId: string, returnId: string, storeId: string, cashSessionId: string, allocation: { method: SalePaymentMethod } | { payments: SalePayment[] }, key: string) => request<ReturnRefund>(
    '/sales/' + encodeURIComponent(saleId) + '/returns/' + encodeURIComponent(returnId) + '/refund',
    { ...json('POST', { storeId, cashSessionId, ...allocation }), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  addReturnDebtCredit: (saleId: string, returnId: string, storeId: string, amount: string, key: string) => request<ReturnDebtCredit>(
    '/sales/' + encodeURIComponent(saleId) + '/returns/' + encodeURIComponent(returnId) + '/debt-credits',
    { ...json('POST', { storeId, amount }), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  addReturnExchange: (saleId: string, returnId: string, input: ReturnExchangeInput, key: string) => request<ReturnExchange>(
    '/sales/' + encodeURIComponent(saleId) + '/returns/' + encodeURIComponent(returnId) + '/exchange',
    { ...json('POST', input), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key } },
  ),
  saleTicket: (storeId: string, id: string) => request<SaleTicket>(
    '/sales/' + encodeURIComponent(id) + '/ticket?' + new URLSearchParams({ store_id: storeId }),
  ),
  confirmSale: (sale: SaleInput, key: string) => request<Sale>('/sales', {
    ...json('POST', sale), headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
  }),
  me: () => request<Identity>('/me'),
  stores: () => request<{ items: Store[] }>('/stores'),
  managedStores: () => request<{ items: ManagedStore[] }>('/stores/admin'),
  createStore: (data: StoreData) => request<ManagedStore>('/stores', json('POST', data)),
  updateStore: (id: string, data: StoreData & { status: ManagedStore['status'] }) =>
    request<ManagedStore>('/stores/' + encodeURIComponent(id), json('PATCH', data)),
  chooseCommerce: (token: string, commerceId: string) =>
    request<void>('/auth/choose-commerce', json('POST', { token, commerceId })),
  login: (credentials: Credentials) => request<LoginChoice | undefined>('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(credentials),
  }),
  logout: () => request<void>('/auth/logout', { method: 'POST' }),
};
