package inventory

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const movementPageSize = 25

type MovementFilter struct {
	Query, Kind, From, To, Location, Variant, Actor, Reference, Older, Newer string
}

type MovementLocation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	StoreName  string `json:"storeName"`
	Historical bool   `json:"historical,omitempty"`
	Active     bool   `json:"active"`
}

type HistoryMovement struct {
	Movement
	Product         string            `json:"product"`
	Size            string            `json:"size"`
	Color           string            `json:"color"`
	ProductSnapshot bool              `json:"productSnapshot"`
	ActorName       string            `json:"actorName"`
	Location        MovementLocation  `json:"location"`
	Origin          *MovementLocation `json:"origin"`
	Destination     *MovementLocation `json:"destination"`
	OtherEndHidden  bool              `json:"otherEndHidden"`
	PurchaseOrderID *string           `json:"purchaseOrderId"`
	SaleNumber      *string           `json:"saleNumber"`
}

type MovementHistory struct {
	Items     []HistoryMovement  `json:"items"`
	Locations []MovementLocation `json:"locations"`
	Timezone  string             `json:"timezone"`
	Older     string             `json:"older,omitempty"`
	Newer     string             `json:"newer,omitempty"`
}

type MovementDetail struct {
	Item     HistoryMovement `json:"item"`
	Timezone string          `json:"timezone"`
}

func (s Service) MovementHistory(ctx context.Context, actor auth.Identity, storeID string, f MovementFilter) (MovementHistory, error) {
	f.Query, f.Actor = strings.TrimSpace(f.Query), strings.TrimSpace(f.Actor)
	for _, term := range []string{f.Query, f.Actor} {
		if !utf8.ValidString(term) || strings.ContainsRune(term, 0) || utf8.RuneCountInString(term) > 160 {
			return MovementHistory{}, ErrInvalid
		}
	}
	for _, id := range []string{f.Location, f.Variant, f.Reference, f.Older, f.Newer} {
		if id != "" && !validID(id) {
			return MovementHistory{}, ErrInvalid
		}
	}
	switch f.Kind {
	case "", "adjustment", "count_adjustment", "transfer_dispatch", "transfer_receipt", "purchase_receipt", "sale", "sale_return", "sale_exchange":
	default:
		return MovementHistory{}, ErrInvalid
	}
	if f.Older != "" && f.Newer != "" || f.From != "" && f.To != "" && f.From > f.To {
		return MovementHistory{}, ErrInvalid
	}

	var result MovementHistory
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if f.Location != "" {
			var id string
			err := tx.QueryRow(ctx, `SELECT id FROM stock_locations WHERE tenant_id=$1 AND store_id=$2 AND id=$3`,
				actor.TenantID, storeID, f.Location).Scan(&id)
			if err != nil {
				return mapNotFound(err)
			}
		}
		result.Timezone = allowed.timezone
		zone, err := time.LoadLocation(allowed.timezone)
		if err != nil {
			return err
		}
		from, err := movementDate(f.From, zone)
		if err != nil {
			return err
		}
		to, err := movementDate(f.To, zone)
		if err != nil {
			return err
		}
		if to != nil {
			end := to.AddDate(0, 0, 1)
			to = &end
		}

		args := historyArgs(actor, storeID)
		args["q"], args["kind"], args["place"], args["variant"] = f.Query, f.Kind, f.Location, f.Variant
		args["actor"], args["ref"], args["from_day"], args["to_day"] = f.Actor, f.Reference, from, to
		cursor := f.Older
		direction, comparator := "DESC", "<"
		if f.Newer != "" {
			cursor, direction, comparator = f.Newer, "ASC", ">"
		}
		boundary := ""
		if cursor != "" {
			var date time.Time
			err := tx.QueryRow(ctx, `SELECT m.created_at FROM stock_movements m
				JOIN stock_locations l ON (l.tenant_id,l.id)=(m.tenant_id,m.location_id)
				WHERE m.tenant_id=$1 AND l.store_id=$2 AND m.id=$3`, actor.TenantID, storeID, cursor).Scan(&date)
			if err != nil {
				return mapNotFound(err)
			}
			args["cursor_at"], args["cursor_id"] = date, cursor
			boundary = " AND (m.created_at,m.id) " + comparator + " (@cursor_at::timestamptz,@cursor_id::uuid)"
		}
		query := movementHistorySelect + movementFilterSQL + boundary +
			" ORDER BY m.created_at " + direction + ",m.id " + direction + " LIMIT 26"
		result.Items, err = readMovementRows(ctx, tx, query, args)
		if err != nil {
			return err
		}
		more := len(result.Items) > movementPageSize
		if more {
			result.Items = result.Items[:movementPageSize]
		}
		if f.Newer != "" {
			for i, j := 0, len(result.Items)-1; i < j; i, j = i+1, j-1 {
				result.Items[i], result.Items[j] = result.Items[j], result.Items[i]
			}
		}
		if len(result.Items) > 0 {
			if f.Older != "" || f.Newer != "" && more {
				result.Newer = result.Items[0].ID
			}
			if f.Newer != "" || more {
				result.Older = result.Items[len(result.Items)-1].ID
			}
		}

		rows, err := tx.Query(ctx, `SELECT id,name,code,'' AS store_name,false AS historical,status='active' AS active
			FROM stock_locations WHERE tenant_id=$1 AND store_id=$2 ORDER BY (status='active') DESC,code,id`, actor.TenantID, storeID)
		if err != nil {
			return err
		}
		result.Locations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[MovementLocation])
		return err
	})
	return result, err
}

func movementDate(day string, zone *time.Location) (*time.Time, error) {
	if day == "" {
		return nil, nil
	}
	value, err := time.ParseInLocation(time.DateOnly, day, zone)
	if err != nil || value.Year() < 1 || value.Format(time.DateOnly) != day {
		return nil, ErrInvalid
	}
	return &value, nil
}

func (s Service) MovementDetail(ctx context.Context, actor auth.Identity, storeID, id string) (MovementDetail, error) {
	if !validID(id) {
		return MovementDetail{}, ErrInvalid
	}
	var result MovementDetail
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		args := historyArgs(actor, storeID)
		args["id"] = id
		rows, err := readMovementRows(ctx, tx, movementHistorySelect+" AND m.id=@id", args)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return ErrNotFound
		}
		result.Item, result.Timezone = rows[0], allowed.timezone
		return nil
	})
	return result, err
}

func historyArgs(actor auth.Identity, storeID string) pgx.NamedArgs {
	return pgx.NamedArgs{"tenant": actor.TenantID, "user_id": actor.UserID, "store": storeID}
}

func readMovementRows(ctx context.Context, tx pgx.Tx, query string, args pgx.NamedArgs) ([]HistoryMovement, error) {
	rows, err := tx.Query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistoryMovement, error) {
		var item HistoryMovement
		err := row.Scan(&item)
		return item, err
	})
}

const movementFilterSQL = `
	AND (@q='' OR strpos(lower(m.sku_snapshot),lower(@q))>0 OR
		strpos(lower(coalesce(ei.product_name,si.product_name,oi.product_name,ci.product_snapshot,p.name)),lower(@q))>0)
	AND (@kind='' OR m.movement_type=@kind)
	AND (nullif(@place,'') IS NULL OR m.location_id=nullif(@place,'')::uuid)
	AND (nullif(@variant,'') IS NULL OR m.variant_id=nullif(@variant,'')::uuid)
	AND (@actor='' OR strpos(lower(u.name),lower(@actor))>0 OR m.actor_id::text=@actor)
	AND (nullif(@ref,'') IS NULL OR nullif(@ref,'')::uuid IN
		(m.id,t.id,sc.id,sale.id,ri.return_id,ex.id,r.id,r.purchase_order_id))
	AND (@from_day::timestamptz IS NULL OR m.created_at>=@from_day)
	AND (@to_day::timestamptz IS NULL OR m.created_at<@to_day)`

// The ledger supplies the historical SKU. Where there is no document snapshot,
// the product and attribute names are identified as current catalog labels.
// A transfer counterpart is returned only when the actor can read its branch.
const movementHistorySelect = `SELECT jsonb_build_object(
	'id',m.id,'locationId',m.location_id,'variantId',m.variant_id,'actorId',m.actor_id,'sku',m.sku_snapshot,
	'type',m.movement_type,'delta',m.quantity_delta,'before',m.quantity_before,'after',m.quantity_after,
	'reason',m.reason,'createdAt',m.created_at,'transferId',t.id,'countId',sc.id,
	'saleId',sale.id,'returnId',ri.return_id,'exchangeId',ex.id,'purchaseReceiptId',r.id,
	'purchaseOrderId',r.purchase_order_id,'saleNumber',sale.sale_number::text,
	'product',coalesce(ei.product_name,si.product_name,oi.product_name,ci.product_snapshot,p.name),
	'size',coalesce(ei.size,si.size,oi.size,ci.size_snapshot,v.size),'color',coalesce(ei.color,si.color,oi.color,ci.color_snapshot,v.color),
	'productSnapshot',coalesce(ei.product_name,si.product_name,oi.product_name,ci.product_snapshot) IS NOT NULL,
	'actorName',u.name,'location',own.location,
	'origin', CASE
		WHEN m.movement_type IN ('sale','sale_exchange','transfer_dispatch') THEN own.location
		WHEN m.movement_type='transfer_receipt' THEN counterpart.location END,
	'destination', CASE
		WHEN m.movement_type IN ('sale_return','purchase_receipt','transfer_receipt') THEN own.location
		WHEN m.movement_type='transfer_dispatch' THEN counterpart.location END,
	'otherEndHidden',t.id IS NOT NULL AND counterpart.location IS NULL
)
FROM stock_movements m
JOIN stock_locations l ON (l.tenant_id,l.id)=(m.tenant_id,m.location_id)
JOIN stores branch ON (branch.tenant_id,branch.id)=(l.tenant_id,l.store_id)
JOIN product_variants v ON (v.tenant_id,v.id)=(m.tenant_id,m.variant_id)
JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
JOIN users u ON (u.tenant_id,u.id)=(m.tenant_id,m.actor_id)
LEFT JOIN sales sale ON (sale.tenant_id,sale.store_id,sale.id)=(m.tenant_id,l.store_id,m.sale_id)
LEFT JOIN sale_items si ON (si.tenant_id,si.sale_id,si.variant_id)=(sale.tenant_id,sale.id,m.variant_id)
	AND m.movement_type IN ('sale','sale_return')
LEFT JOIN sale_return_items ri ON (ri.tenant_id,ri.return_id,ri.location_id,ri.variant_id)=(m.tenant_id,m.return_id,m.location_id,m.variant_id)
LEFT JOIN sale_return_exchanges ex ON (ex.tenant_id,ex.store_id,ex.location_id,ex.id)=(m.tenant_id,l.store_id,m.location_id,m.exchange_id)
LEFT JOIN sale_return_exchange_items ei ON (ei.tenant_id,ei.exchange_id,ei.variant_id)=(ex.tenant_id,ex.id,m.variant_id)
LEFT JOIN purchase_receipts r ON (r.tenant_id,r.store_id,r.id)=(m.tenant_id,l.store_id,m.purchase_receipt_id)
LEFT JOIN purchase_order_items oi ON (oi.tenant_id,oi.purchase_order_id,oi.variant_id)=(r.tenant_id,r.purchase_order_id,m.variant_id)
LEFT JOIN stock_counts sc ON (sc.tenant_id,sc.location_id,sc.id)=(m.tenant_id,m.location_id,m.count_id)
LEFT JOIN stock_count_items ci ON (ci.tenant_id,ci.count_id,ci.variant_id)=(sc.tenant_id,sc.id,m.variant_id)
LEFT JOIN stock_transfers t ON (t.tenant_id,t.id)=(m.tenant_id,m.transfer_id) AND
	CASE m.movement_type WHEN 'transfer_dispatch' THEN t.origin_location_id=m.location_id
		WHEN 'transfer_receipt' THEN t.destination_location_id=m.location_id ELSE false END
CROSS JOIN LATERAL (SELECT jsonb_build_object('id',l.id,'name',coalesce(ri.location_name,ex.location_name,r.location_name,l.name),
	'code',l.code,'storeName',branch.name,'active',l.status='active','historical',coalesce(ri.location_name,ex.location_name,r.location_name) IS NOT NULL) AS location) own
LEFT JOIN LATERAL (
	SELECT jsonb_build_object('id',loc.id,'name',loc.name,'code',loc.code,'storeName',s.name,'active',loc.status='active') AS location
	FROM stock_locations loc
	JOIN stores s ON (s.tenant_id,s.id)=(loc.tenant_id,loc.store_id)
	JOIN user_stores us ON (us.tenant_id,us.store_id)=(s.tenant_id,s.id) AND us.user_id=@user_id
	WHERE loc.tenant_id=m.tenant_id AND loc.id=CASE m.movement_type
		WHEN 'transfer_dispatch' THEN t.destination_location_id WHEN 'transfer_receipt' THEN t.origin_location_id END
		AND s.status='active'
) counterpart ON true
WHERE m.tenant_id=@tenant AND l.store_id=@store`
