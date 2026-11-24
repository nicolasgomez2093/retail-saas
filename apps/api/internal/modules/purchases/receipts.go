package purchases

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrReceiptInvalid = errors.New("verificá el destino, las cantidades, los costos y la observación de la recepción")
var ErrReceiptLocation = errors.New("elegí una ubicación activa de la sucursal de la orden")
var ErrReceiptQuantity = errors.New("la recepción contiene líneas de otra orden o cantidades que superan lo pendiente")

type ReceiptInput struct {
	StoreID    string             `json:"storeId"`
	LocationID string             `json:"locationId"`
	Note       string             `json:"note"`
	Items      []ReceiptItemInput `json:"items"`
}

// maxRejected bounds the units recorded as not accepted in one line. They do
// not consume the pending quantity, so an over-delivery can be recorded too.
const maxRejected = 100000

type ReceiptItemInput struct {
	OrderItemID string  `json:"orderItemId"`
	Accepted    int32   `json:"accepted"`
	Rejected    int32   `json:"rejected"`
	UnitCost    *string `json:"unitCost,omitempty"`
	// RejectionReason explains units that arrived but do not enter stock:
	// damaged, wrong_item (came instead of this line), excess (over the
	// pending quantity) or other. Required when Rejected > 0.
	RejectionReason string `json:"rejectionReason,omitempty"`
}

type ReceiptItem struct {
	OrderItemID     string  `json:"orderItemId"`
	Accepted        int32   `json:"accepted"`
	Rejected        int32   `json:"rejected"`
	RejectionReason *string `json:"rejectionReason"`
	UnitCost        *string `json:"unitCost"`
	VariantID       string  `json:"variantId"`
	SKU             string  `json:"sku"`
	Product         string  `json:"product"`
	Size            string  `json:"size"`
	Color           string  `json:"color"`
	// StockBefore and StockAfter come from the inventory movement the
	// accepted units generated in the receiving location (null if none).
	StockBefore *int32 `json:"stockBefore"`
	StockAfter  *int32 `json:"stockAfter"`
}

type Receipt struct {
	ID           string        `json:"id"`
	OrderID      string        `json:"orderId"`
	LocationID   string        `json:"locationId"`
	LocationName string        `json:"locationName"`
	Note         string        `json:"note"`
	ReceivedBy   string        `json:"receivedBy"`
	ReceivedAt   time.Time     `json:"receivedAt"`
	Items        []ReceiptItem `json:"items"`
}

type ReceiptPage struct {
	Items   []Receipt      `json:"items"`
	HasMore bool           `json:"hasMore"`
	Summary ReceiptSummary `json:"summary"`
}

type ReceiptSummary struct {
	ReceiptCount        int64   `json:"receiptCount"`
	AcceptedWithCost    int64   `json:"acceptedWithCost"`
	AcceptedWithoutCost int64   `json:"acceptedWithoutCost"`
	RejectedUnits       int64   `json:"rejectedUnits"`
	KnownValue          *string `json:"knownValue"`
	TotalValue          *string `json:"totalValue"`
}

const receiptScope = ` WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.purchase_order_id=$3`

const selectReceipts = `SELECT r.id,r.purchase_order_id,r.location_id,r.location_name,r.note,r.received_by,r.received_at,
	(SELECT jsonb_agg(jsonb_build_object('orderItemId',i.order_item_id,'variantId',p.variant_id,'sku',p.sku,
		'product',p.product_name,'size',p.size,'color',p.color,'accepted',i.accepted_qty,'rejected',i.rejected_qty,
		'rejectionReason',i.rejection_reason,'unitCost',i.unit_cost::text,
		'stockBefore',m.quantity_before,'stockAfter',m.quantity_after) ORDER BY p.sku,p.id)
	 FROM purchase_receipt_items i JOIN purchase_order_items p
	 ON (p.tenant_id,p.purchase_order_id,p.id)=(i.tenant_id,i.purchase_order_id,i.order_item_id)
	 LEFT JOIN stock_movements m ON (m.tenant_id,m.purchase_receipt_id,m.variant_id)=(i.tenant_id,i.receipt_id,p.variant_id)
	 WHERE (i.tenant_id,i.purchase_order_id,i.receipt_id)=(r.tenant_id,r.purchase_order_id,r.id))
	FROM purchase_receipts r` + receiptScope

func (s Service) Receive(ctx context.Context, actor auth.Identity, orderID, key string, input ReceiptInput) (Receipt, error) {
	input.Note = strings.TrimSpace(input.Note)
	if !validID(orderID) || !validID(input.StoreID) || !validID(input.LocationID) || !keyPattern.MatchString(key) ||
		utf8.RuneCountInString(input.Note) > 300 || strings.IndexFunc(input.Note, unicode.IsControl) >= 0 ||
		len(input.Items) == 0 || len(input.Items) > 100 {
		return Receipt{}, ErrReceiptInvalid
	}
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b ReceiptItemInput) int { return cmp.Compare(a.OrderItemID, b.OrderItemID) })
	for i := range input.Items {
		item := &input.Items[i]
		if !validID(item.OrderItemID) || item.Accepted < 0 || item.Rejected < 0 || item.Rejected > maxRejected || (item.Accepted == 0 && item.Rejected == 0) ||
			(i > 0 && input.Items[i-1].OrderItemID == item.OrderItemID) {
			return Receipt{}, ErrReceiptInvalid
		}
		switch item.RejectionReason {
		case "":
			if item.Rejected > 0 {
				return Receipt{}, ErrReceiptInvalid
			}
		case "damaged", "wrong_item", "excess", "other":
			if item.Rejected == 0 {
				return Receipt{}, ErrReceiptInvalid
			}
		default:
			return Receipt{}, ErrReceiptInvalid
		}
		if item.UnitCost != nil {
			cost, valid := money.Parse(*item.UnitCost)
			if !valid || item.Accepted == 0 {
				return Receipt{}, ErrReceiptInvalid
			}
			normalized := money.Format(cost)
			item.UnitCost = &normalized
		}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Receipt{}, err
	}
	digest := sha256.Sum256(body)
	var result Receipt
	err = s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, _ orderAccess) error {
		order, err := scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var receiptID, previousOrder, creator string
		var priorHash []byte
		err = tx.QueryRow(ctx, `SELECT id,purchase_order_id,received_by,request_hash FROM purchase_receipts
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&receiptID, &previousOrder, &creator, &priorHash)
		if err == nil {
			if previousOrder != orderID || creator != actor.UserID || !bytes.Equal(priorHash, digest[:]) {
				return ErrKey
			}
			result, err = scanReceipt(tx.QueryRow(ctx, selectReceipts+" AND r.id=$4", actor.TenantID, input.StoreID, orderID, receiptID))
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if order.Status == "CANCELLED" || order.Status == "RECEIVED" {
			return ErrClosed
		}
		orderItems := make(map[string]Item, len(order.Items))
		pending := 0
		for _, item := range order.Items {
			orderItems[item.ID] = item
			if item.Received < item.Quantity {
				pending++
			}
		}
		status := order.Status
		for _, line := range input.Items {
			item, exists := orderItems[line.OrderItemID]
			left := item.Quantity - item.Received
			// Only accepted units consume what is pending. Units not accepted
			// (damaged, wrong item or excess) are recorded but never enter stock.
			if !exists || line.Accepted > left {
				return ErrReceiptQuantity
			}
			if line.Accepted > 0 {
				status = "PARTIALLY_RECEIVED"
				if line.Accepted == left {
					pending--
				}
			}
		}
		if pending == 0 {
			status = "RECEIVED"
		}

		err = tx.QueryRow(ctx, `INSERT INTO purchase_receipts(tenant_id,store_id,purchase_order_id,location_id,location_name,note,received_by,idempotency_key,request_hash)
			SELECT tenant_id,store_id,$3,id,name,$5,$6,$7,$8 FROM stock_locations
			WHERE tenant_id=$1 AND store_id=$2 AND id=$4 AND status='active' RETURNING id`,
			actor.TenantID, input.StoreID, orderID, input.LocationID, input.Note, actor.UserID, key, digest[:]).Scan(&receiptID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReceiptLocation
		}
		if err != nil {
			return err
		}
		for _, line := range input.Items {
			if _, err := tx.Exec(ctx, `INSERT INTO purchase_receipt_items(tenant_id,purchase_order_id,receipt_id,order_item_id,accepted_qty,rejected_qty,unit_cost,rejection_reason)
				VALUES ($1,$2,$3,$4,$5,$6,$7::text::numeric,NULLIF($8,''))`, actor.TenantID, orderID, receiptID, line.OrderItemID, line.Accepted, line.Rejected, line.UnitCost, line.RejectionReason); err != nil {
				return err
			}
			if line.Accepted > 0 {
				item := orderItems[line.OrderItemID]
				if err := inventory.PostPurchaseReceiptMovement(ctx, tx, actor, receiptID, input.StoreID, input.LocationID, item.VariantID, item.SKU, line.Accepted, line.UnitCost); err != nil {
					return err
				}
			}
		}
		if status != order.Status {
			if _, err := tx.Exec(ctx, "UPDATE purchase_orders SET status=$4 WHERE tenant_id=$1 AND store_id=$2 AND id=$3",
				actor.TenantID, input.StoreID, orderID, status); err != nil {
				return err
			}
		}
		result, err = scanReceipt(tx.QueryRow(ctx, selectReceipts+" AND r.id=$4", actor.TenantID, input.StoreID, orderID, receiptID))
		return err
	})
	return result, err
}

func (s Service) Receipts(ctx context.Context, actor auth.Identity, storeID, orderID string, limit, offset int) (ReceiptPage, error) {
	if limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return ReceiptPage{}, ErrReceiptInvalid
	}
	var page ReceiptPage
	rowLimit := limit + 1
	err := s.readReceipts(ctx, actor, storeID, orderID, &rowLimit, offset, &page.Summary, func(_, _ string, rows pgx.Rows) error {
		var err error
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Receipt, error) { return scanReceipt(row) })
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		return err
	})
	return page, err
}

func (s Service) readReceipts(ctx context.Context, actor auth.Identity, storeID, orderID string, limit *int, offset int, summary *ReceiptSummary, read func(string, string, pgx.Rows) error) error {
	if !validID(storeID) || !validID(orderID) {
		return ErrReceiptInvalid
	}
	return s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, access orderAccess) error {
		var currency string
		err := tx.QueryRow(ctx, "SELECT currency FROM purchase_orders WHERE tenant_id=$1 AND store_id=$2 AND id=$3",
			actor.TenantID, storeID, orderID).Scan(&currency)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if summary != nil {
			*summary, err = summarizeReceipts(ctx, tx, actor.TenantID, storeID, orderID)
			if err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, selectReceipts+" ORDER BY r.received_at DESC,r.id DESC LIMIT $4 OFFSET $5",
			actor.TenantID, storeID, orderID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		return read(currency, access.timezone, rows)
	})
}

func summarizeReceipts(ctx context.Context, tx pgx.Tx, tenantID, storeID, orderID string) (ReceiptSummary, error) {
	var result ReceiptSummary
	err := tx.QueryRow(ctx, `WITH totals AS (
		SELECT count(DISTINCT r.id) AS receipts,
			coalesce(sum(i.accepted_qty) FILTER (WHERE i.unit_cost IS NOT NULL),0) AS known_units,
			coalesce(sum(i.accepted_qty) FILTER (WHERE i.unit_cost IS NULL),0) AS unknown_units,
			coalesce(sum(i.rejected_qty),0) AS rejected_units,sum(i.accepted_qty*i.unit_cost) AS known_value
		FROM purchase_receipts r
		JOIN purchase_receipt_items i ON (i.tenant_id,i.purchase_order_id,i.receipt_id)=(r.tenant_id,r.purchase_order_id,r.id)`+receiptScope+`
	)
	SELECT receipts,known_units,unknown_units,rejected_units,known_value::text,
		CASE WHEN unknown_units=0 THEN coalesce(known_value,0.00)::text END FROM totals`, tenantID, storeID, orderID).
		Scan(&result.ReceiptCount, &result.AcceptedWithCost, &result.AcceptedWithoutCost, &result.RejectedUnits, &result.KnownValue, &result.TotalValue)
	return result, err
}

func scanReceipt(row pgx.Row) (Receipt, error) {
	var result Receipt
	err := row.Scan(&result.ID, &result.OrderID, &result.LocationID, &result.LocationName, &result.Note,
		&result.ReceivedBy, &result.ReceivedAt, &result.Items)
	result.ReceivedAt = result.ReceivedAt.UTC()
	return result, err
}
