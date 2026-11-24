package inventory

import (
	"context"
	"errors"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

var ErrTransferState = errors.New("el estado o las cantidades confirmadas no permiten esa operación")
var ErrTransferKey = errors.New("la clave ya se usó para otra transferencia")
var ErrReceiptReason = errors.New("indicá el motivo de la diferencia de recepción")

const maxInventoryLines = 100

type TransferLocation struct {
	ID        string `json:"id"`
	StoreID   string `json:"storeId"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	StoreName string `json:"storeName"`
}

type TransferSummary struct {
	ID            string           `json:"id"`
	Origin        TransferLocation `json:"origin"`
	Destination   TransferLocation `json:"destination"`
	Status        string           `json:"status"`
	CreatedAt     time.Time        `json:"createdAt"`
	DispatchedAt  *time.Time       `json:"dispatchedAt"`
	ReceivedAt    *time.Time       `json:"receivedAt"`
	CancelledAt   *time.Time       `json:"cancelledAt"`
	ReceiptReason string           `json:"receiptReason"`
}

type TransferItem struct {
	VariantID string `json:"variantId"`
	SKU       string `json:"sku"`
	Quantity  int32  `json:"quantity"`
	Received  *int32 `json:"received"`
}

type Transfer struct {
	TransferSummary
	Items []TransferItem `json:"items"`
}

type TransferPage struct {
	Items        []TransferSummary  `json:"items"`
	Destinations []TransferLocation `json:"destinations"`
	CanTransfer  bool               `json:"canTransfer"`
}

type TransferQuantity struct {
	VariantID string `json:"variantId"`
	Quantity  int32  `json:"quantity"`
}

type NewTransfer struct {
	StoreID       string             `json:"storeId"`
	OriginID      string             `json:"originLocationId"`
	DestinationID string             `json:"destinationLocationId"`
	Items         []TransferQuantity `json:"items"`
}

type EnteredQuantity struct {
	VariantID string `json:"variantId"`
	Quantity  *int32 `json:"quantity"`
}

type TransferReceipt struct {
	StoreID string            `json:"storeId"`
	Items   []EnteredQuantity `json:"items"`
	Reason  string            `json:"reason"`
}

const selectTransfer = `SELECT t.id,t.status,t.created_at,t.dispatched_at,t.received_at,t.cancelled_at,t.receipt_reason,
	o.id,o.store_id,o.name,o.code,os.name,d.id,d.store_id,d.name,d.code,ds.name
	FROM stock_transfers t
	JOIN stock_locations o ON (o.tenant_id,o.id)=(t.tenant_id,t.origin_location_id)
	JOIN stock_locations d ON (d.tenant_id,d.id)=(t.tenant_id,t.destination_location_id)
	JOIN stores os ON (os.tenant_id,os.id)=(o.tenant_id,o.store_id)
	JOIN stores ds ON (ds.tenant_id,ds.id)=(d.tenant_id,d.store_id)`

func (s Service) Transfers(ctx context.Context, actor auth.Identity, storeID string) (TransferPage, error) {
	result := TransferPage{Destinations: []TransferLocation{}}
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		rows, err := tx.Query(ctx, selectTransfer+` WHERE t.tenant_id=$1 AND (o.store_id=$2 OR d.store_id=$2)
			ORDER BY t.created_at DESC,t.id DESC LIMIT 50`, actor.TenantID, storeID)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (TransferSummary, error) { return scanTransfer(row) })
		result.CanTransfer = allowed.transfer
		if err != nil || !allowed.transfer {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT l.id,l.store_id,l.name,l.code,s.name FROM stock_locations l
			JOIN stores s ON (s.tenant_id,s.id)=(l.tenant_id,l.store_id)
			WHERE l.tenant_id=$1 AND l.status='active' AND s.status='active' ORDER BY s.code,l.code`, actor.TenantID)
		if err != nil {
			return err
		}
		result.Destinations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TransferLocation])
		return err
	})
	return result, err
}

func (s Service) Transfer(ctx context.Context, actor auth.Identity, storeID, id string) (Transfer, error) {
	if !validID(id) {
		return Transfer{}, ErrInvalid
	}
	var result Transfer
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		var err error
		result, err = readTransfer(ctx, tx, actor.TenantID, storeID, id)
		return err
	})
	return result, err
}

func (s Service) CreateTransfer(ctx context.Context, actor auth.Identity, key string, input NewTransfer) (Transfer, error) {
	items, valid := quantities(input.Items, false)
	if !valid || !keyPattern.MatchString(key) || !validID(input.OriginID) || !validID(input.DestinationID) ||
		input.OriginID == input.DestinationID {
		return Transfer{}, ErrInvalid
	}
	var result Transfer
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.transfer {
			return auth.ErrForbidden
		}
		if err := requireLocation(ctx, tx, actor.TenantID, input.StoreID, input.OriginID); err != nil {
			return err
		}
		var id, creator string
		err := tx.QueryRow(ctx, "SELECT id,created_by FROM stock_transfers WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key).Scan(&id, &creator)
		if err == nil {
			if creator != actor.UserID {
				return ErrTransferKey
			}
			prior, err := readTransfer(ctx, tx, actor.TenantID, input.StoreID, id)
			if errors.Is(err, ErrNotFound) {
				return ErrTransferKey
			}
			if err != nil {
				return err
			}
			if prior.Origin.ID != input.OriginID || prior.Destination.ID != input.DestinationID || len(prior.Items) != len(items) {
				return ErrTransferKey
			}
			for _, line := range prior.Items {
				if items[line.VariantID] != line.Quantity {
					return ErrTransferKey
				}
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := requireActiveDestination(ctx, tx, actor.TenantID, input.DestinationID); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO stock_transfers(tenant_id,origin_location_id,destination_location_id,idempotency_key,created_by)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`, actor.TenantID, input.OriginID, input.DestinationID, key, actor.UserID).Scan(&id)
		if err != nil {
			return err
		}
		for _, line := range input.Items {
			tag, err := tx.Exec(ctx, `INSERT INTO stock_transfer_items(tenant_id,transfer_id,variant_id,sku_snapshot,requested_quantity)
				SELECT tenant_id,$2,id,sku,$4 FROM product_variants WHERE tenant_id=$1 AND id=$3`,
				actor.TenantID, id, line.VariantID, line.Quantity)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		result, err = readTransfer(ctx, tx, actor.TenantID, input.StoreID, id)
		return err
	})
	return result, err
}

func (s Service) DispatchTransfer(ctx context.Context, actor auth.Identity, storeID, id string) (Transfer, error) {
	return s.changeTransfer(ctx, actor, storeID, id, true, func(tx pgx.Tx, current Transfer) error {
		if current.Status == "DISPATCHED" || current.Status == "RECEIVED" {
			return nil
		}
		if current.Status != "DRAFT" {
			return ErrTransferState
		}
		if err := requireLocation(ctx, tx, actor.TenantID, storeID, current.Origin.ID); err != nil {
			return err
		}
		if err := requireActiveDestination(ctx, tx, actor.TenantID, current.Destination.ID); err != nil {
			return err
		}
		for _, line := range current.Items {
			if _, err := postMovement(ctx, tx, actor, Movement{LocationID: current.Origin.ID, VariantID: line.VariantID, SKU: line.SKU,
				Type: "transfer_dispatch", Delta: -line.Quantity, Reason: "Despacho de transferencia", TransferID: &id}, nil); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE stock_transfers SET status='DISPATCHED',dispatched_by=$3,dispatched_at=clock_timestamp()
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID)
		return err
	})
}

func (s Service) ReceiveTransfer(ctx context.Context, actor auth.Identity, id string, input TransferReceipt) (Transfer, error) {
	items, valid := enteredQuantities(input.Items)
	input.Reason = strings.TrimSpace(input.Reason)
	if !valid || (input.Reason != "" && !validText(input.Reason, 300)) {
		return Transfer{}, ErrInvalid
	}
	return s.changeTransfer(ctx, actor, input.StoreID, id, false, func(tx pgx.Tx, current Transfer) error {
		if current.Status != "DISPATCHED" && current.Status != "RECEIVED" {
			return ErrTransferState
		}
		if len(current.Items) != len(items) {
			return ErrInvalid
		}
		discrepancy := false
		for _, line := range current.Items {
			qty, exists := items[line.VariantID]
			if !exists || qty > line.Quantity {
				return ErrInvalid
			}
			if current.Status == "RECEIVED" && (line.Received == nil || *line.Received != qty || current.ReceiptReason != input.Reason) {
				return ErrTransferState
			}
			discrepancy = discrepancy || qty != line.Quantity
		}
		if current.Status == "RECEIVED" {
			return nil
		}
		if discrepancy && input.Reason == "" {
			return ErrReceiptReason
		}
		if err := requireLocation(ctx, tx, actor.TenantID, input.StoreID, current.Destination.ID); err != nil {
			return err
		}
		for _, line := range current.Items {
			qty := items[line.VariantID]
			if qty > 0 {
				if _, err := postMovement(ctx, tx, actor, Movement{LocationID: current.Destination.ID, VariantID: line.VariantID, SKU: line.SKU,
					Type: "transfer_receipt", Delta: qty, Reason: "Recepción de transferencia", TransferID: &id}, nil); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE stock_transfer_items SET received_quantity=$4 WHERE tenant_id=$1 AND transfer_id=$2 AND variant_id=$3`,
				actor.TenantID, id, line.VariantID, qty); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE stock_transfers SET status='RECEIVED',received_by=$3,received_at=clock_timestamp(),receipt_reason=$4
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID, input.Reason)
		return err
	})
}

func (s Service) CancelTransfer(ctx context.Context, actor auth.Identity, storeID, id string) (Transfer, error) {
	return s.changeTransfer(ctx, actor, storeID, id, true, func(tx pgx.Tx, current Transfer) error {
		if current.Status == "CANCELLED" {
			return nil
		}
		if current.Status != "DRAFT" {
			return ErrTransferState
		}
		_, err := tx.Exec(ctx, `UPDATE stock_transfers SET status='CANCELLED',cancelled_by=$3,cancelled_at=clock_timestamp()
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID)
		return err
	})
}

func (s Service) changeTransfer(ctx context.Context, actor auth.Identity, storeID, id string, fromOrigin bool,
	change func(pgx.Tx, Transfer) error) (Transfer, error) {
	if !validID(id) {
		return Transfer{}, ErrInvalid
	}
	var result Transfer
	err := s.withStore(ctx, actor, storeID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.transfer {
			return auth.ErrForbidden
		}
		current, err := readTransfer(ctx, tx, actor.TenantID, storeID, id)
		if err != nil {
			return err
		}
		required := current.Destination.StoreID
		if fromOrigin {
			required = current.Origin.StoreID
		}
		if required != storeID {
			return auth.ErrForbidden
		}
		if err := change(tx, current); err != nil {
			return err
		}
		result, err = readTransfer(ctx, tx, actor.TenantID, storeID, id)
		return err
	})
	return result, err
}

func readTransfer(ctx context.Context, tx pgx.Tx, tenantID, storeID, id string) (Transfer, error) {
	header, err := scanTransfer(tx.QueryRow(ctx, selectTransfer+` WHERE t.tenant_id=$1 AND t.id=$2 AND (o.store_id=$3 OR d.store_id=$3)`,
		tenantID, id, storeID))
	if err != nil {
		return Transfer{}, mapNotFound(err)
	}
	rows, err := tx.Query(ctx, `SELECT variant_id,sku_snapshot,requested_quantity,received_quantity FROM stock_transfer_items
		WHERE tenant_id=$1 AND transfer_id=$2 ORDER BY variant_id`, tenantID, id)
	if err != nil {
		return Transfer{}, err
	}
	lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[TransferItem])
	return Transfer{TransferSummary: header, Items: lines}, err
}

func scanTransfer(row pgx.Row) (TransferSummary, error) {
	var t TransferSummary
	err := row.Scan(&t.ID, &t.Status, &t.CreatedAt, &t.DispatchedAt, &t.ReceivedAt, &t.CancelledAt, &t.ReceiptReason,
		&t.Origin.ID, &t.Origin.StoreID, &t.Origin.Name, &t.Origin.Code, &t.Origin.StoreName,
		&t.Destination.ID, &t.Destination.StoreID, &t.Destination.Name, &t.Destination.Code, &t.Destination.StoreName)
	t.CreatedAt = t.CreatedAt.UTC()
	for _, timestamp := range []*time.Time{t.DispatchedAt, t.ReceivedAt, t.CancelledAt} {
		if timestamp != nil {
			*timestamp = timestamp.UTC()
		}
	}
	return t, err
}

func enteredQuantities(input []EnteredQuantity) (map[string]int32, bool) {
	if len(input) > maxInventoryLines {
		return nil, false
	}
	lines := make([]TransferQuantity, 0, len(input))
	for _, line := range input {
		if line.Quantity == nil {
			return nil, false
		}
		lines = append(lines, TransferQuantity{VariantID: line.VariantID, Quantity: *line.Quantity})
	}
	return quantities(lines, true)
}

func quantities(lines []TransferQuantity, allowZero bool) (map[string]int32, bool) {
	if len(lines) == 0 || len(lines) > maxInventoryLines {
		return nil, false
	}
	result := make(map[string]int32, len(lines))
	for _, line := range lines {
		if !validID(line.VariantID) || line.Quantity < 0 || (!allowZero && line.Quantity == 0) {
			return nil, false
		}
		if _, exists := result[line.VariantID]; exists {
			return nil, false
		}
		result[line.VariantID] = line.Quantity
	}
	return result, true
}

func requireActiveDestination(ctx context.Context, tx pgx.Tx, tenantID, id string) error {
	var rowID string
	return mapNotFound(tx.QueryRow(ctx, `SELECT l.id FROM stock_locations l JOIN stores s ON (s.tenant_id,s.id)=(l.tenant_id,l.store_id)
		WHERE l.tenant_id=$1 AND l.id=$2 AND l.status='active' AND s.status='active'`, tenantID, id).Scan(&rowID))
}
