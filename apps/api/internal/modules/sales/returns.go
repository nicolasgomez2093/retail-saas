package sales

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

var ErrReturnInvalid = errors.New("verificá el motivo, las cantidades y las ubicaciones de la devolución")
var ErrReturnQuantity = errors.New("la cantidad supera las unidades pendientes de devolución")
var ErrReturnLocation = errors.New("elegí una ubicación activa de la sucursal que coincida con el destino")
var ErrReturnKey = errors.New("la clave de idempotencia ya se usó para otra devolución")

type ReturnQuantity struct {
	VariantID   string `json:"variantId"`
	LocationID  string `json:"locationId"`
	Disposition string `json:"disposition"`
	Quantity    int32  `json:"quantity"`
}

type ReturnInput struct {
	SourceExchangeID *string          `json:"sourceExchangeId,omitempty"`
	StoreID          string           `json:"storeId"`
	Reason           string           `json:"reason"`
	Items            []ReturnQuantity `json:"items"`
}

type ReturnItem struct {
	ReturnQuantity
	LocationName string `json:"locationName"`
	Product      string `json:"product"`
	SKU          string `json:"sku"`
	UnitPrice    string `json:"unitPrice"`
}

type SaleReturn struct {
	ExchangeRefundOptions []Payment          `json:"exchangeRefundOptions,omitempty"`
	SourceExchangeID      *string            `json:"sourceExchangeId,omitempty"`
	ID                    string             `json:"id"`
	SaleID                string             `json:"saleId"`
	Reason                string             `json:"reason"`
	ReferenceAmount       string             `json:"referenceAmount"`
	CreatedBy             string             `json:"createdBy"`
	CreatedAt             time.Time          `json:"createdAt"`
	Items                 []ReturnItem       `json:"items"`
	Refund                *ReturnRefund      `json:"refund,omitempty"`
	DebtCredits           []ReturnDebtCredit `json:"debtCredits,omitempty"`
	Exchange              *ReturnExchange    `json:"exchange,omitempty"`
}

type ReturnableItem struct {
	VariantID string `json:"variantId"`
	Product   string `json:"product"`
	SKU       string `json:"sku"`
	Size      string `json:"size"`
	Color     string `json:"color"`
	UnitPrice string `json:"unitPrice"`
	Sold      int32  `json:"sold"`
	Returned  int32  `json:"returned"`
}

type ReturnHistory struct {
	Number          string           `json:"number"`
	Currency        string           `json:"currency"`
	Items           []ReturnableItem `json:"items"`
	Returns         []SaleReturn     `json:"returns"`
	HasMore         bool             `json:"hasMore"`
	CanRefundCash   bool             `json:"canRefundCash"`
	RefundMethod    *string          `json:"refundMethod"`
	RefundOptions   []Payment        `json:"refundOptions"`
	Deposit         *Payment         `json:"deposit,omitempty"`
	OutstandingDebt *string          `json:"outstandingDebt,omitempty"`
}

const returnSelect = `SELECT r.id,r.sale_id,r.reason,r.reference_amount::text,r.created_by,r.created_at,r.source_exchange_id,
	(SELECT jsonb_agg(jsonb_build_object('variantId',i.variant_id,'locationId',i.location_id,'locationName',i.location_name,
		'disposition',i.disposition,'quantity',i.quantity,'product',COALESCE(sold.product_name,replacement.product_name),'sku',COALESCE(sold.sku,replacement.sku),
		'unitPrice',COALESCE(sold.unit_price,replacement.unit_price)::text)
		ORDER BY i.location_id,i.variant_id)
	 FROM sale_return_items i
	 LEFT JOIN sale_items sold ON (sold.tenant_id,sold.sale_id,sold.variant_id)=(i.tenant_id,i.sale_id,i.original_variant_id)
	 LEFT JOIN sale_return_exchange_items replacement ON (replacement.tenant_id,replacement.exchange_id,replacement.variant_id)=
	     (i.tenant_id,i.source_exchange_id,i.variant_id)
	 WHERE i.tenant_id=r.tenant_id AND i.return_id=r.id),
	CASE WHEN f.id IS NOT NULL THEN jsonb_build_object('id',f.id,'returnId',f.return_id,'cashSessionId',f.cash_session_id,
		'method',f.method,'payments',` + refundAllocations + `,'amount',f.amount::text,'createdBy',f.created_by,'createdAt',f.created_at) END,
	(SELECT jsonb_agg(jsonb_build_object('id',c.id,'returnId',c.return_id,'amount',c.amount::text,
		'balanceAfter',c.balance_after::text,'createdBy',c.created_by,'createdAt',c.created_at) ORDER BY c.created_at,c.id)
	 FROM sale_return_debt_credits c WHERE (c.tenant_id,c.store_id,c.sale_id,c.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)),
	CASE WHEN e.id IS NOT NULL THEN jsonb_build_object('id',e.id,'returnId',e.return_id,'locationId',e.location_id,
		'locationName',e.location_name,'total',e.total::text,'createdBy',e.created_by,'createdAt',e.created_at,'items',` + exchangeItems + `,'charge',` + exchangeCharge + `,
		'returnedQuantities',` + exchangeReturnedQuantities + `) END
	FROM sale_returns r LEFT JOIN sale_return_refunds f
	    ON (f.tenant_id,f.store_id,f.sale_id,f.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)
	LEFT JOIN sale_return_exchanges e ON (e.tenant_id,e.store_id,e.sale_id,e.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)
	WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.sale_id=$3`

func normalizeReturnInput(saleID, key string, input *ReturnInput) error {
	input.Reason = strings.TrimSpace(input.Reason)
	if (input.SourceExchangeID != nil && !validID(*input.SourceExchangeID)) || !validID(saleID) || !validID(input.StoreID) || !keyPattern.MatchString(key) ||
		input.Reason == "" || utf8.RuneCountInString(input.Reason) > 300 || strings.IndexFunc(input.Reason, unicode.IsControl) >= 0 ||
		len(input.Items) == 0 || len(input.Items) > 100 {
		return ErrReturnInvalid
	}
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b ReturnQuantity) int {
		if order := cmp.Compare(a.LocationID, b.LocationID); order != 0 {
			return order
		}
		return cmp.Compare(a.VariantID, b.VariantID)
	})
	for i, line := range input.Items {
		if !validID(line.LocationID) || !validID(line.VariantID) || line.Quantity <= 0 ||
			(line.Disposition != "sellable" && line.Disposition != "quarantine") ||
			(i > 0 && line.LocationID == input.Items[i-1].LocationID && line.VariantID == input.Items[i-1].VariantID) {
			return ErrReturnInvalid
		}
	}
	return nil
}

func (s Service) AddReturn(ctx context.Context, actor auth.Identity, saleID, key string, input ReturnInput) (SaleReturn, error) {
	if err := normalizeReturnInput(saleID, key, &input); err != nil {
		return SaleReturn{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return SaleReturn{}, err
	}
	digest := sha256.Sum256(body)
	var result SaleReturn
	err = s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, supervise bool) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:sale-return:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, true, &ReturnHistory{}); err != nil {
			return err
		}
		id, _, err := postReturn(ctx, tx, actor, saleID, key, input, digest[:])
		if err != nil {
			return err
		}
		result, err = scanSaleReturn(tx.QueryRow(ctx, returnSelect+" AND r.id=$4", actor.TenantID, input.StoreID, saleID, id))
		return err
	})
	result.Refund = nil
	result.DebtCredits = nil
	result.Exchange = nil
	return result, err
}

// postReturn registers the goods that come back and their stock movements.
// The caller holds the sale lock (readReturnSale with lock). A key already
// used returns the stored return when the request is the same one.
func postReturn(ctx context.Context, tx pgx.Tx, actor auth.Identity, saleID, key string, input ReturnInput, digest []byte) (string, bool, error) {
	var id, foundSale, creator string
	var prior []byte
	err := tx.QueryRow(ctx, "SELECT id,sale_id,created_by,request_hash FROM sale_returns WHERE tenant_id=$1 AND idempotency_key=$2",
		actor.TenantID, key).Scan(&id, &foundSale, &creator, &prior)
	if err == nil {
		if foundSale != saleID || creator != actor.UserID || !bytes.Equal(prior, digest) {
			return "", false, ErrReturnKey
		}
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	err = func() error {
		sold, err := returnableItems(ctx, tx, actor.TenantID, input.StoreID, saleID, input.SourceExchangeID)
		if err != nil {
			return err
		}
		if input.SourceExchangeID != nil && len(sold) == 0 {
			return ErrNotFound
		}
		byVariant := make(map[string]ReturnableItem, len(sold))
		for _, line := range sold {
			byVariant[line.VariantID] = line
		}
		var amount int64
		for _, line := range input.Items {
			snapshot, ok := byVariant[line.VariantID]
			if !ok || line.Quantity > snapshot.Sold-snapshot.Returned {
				return ErrReturnQuantity
			}
			snapshot.Returned += line.Quantity
			byVariant[line.VariantID] = snapshot
			unit, valid := money.Parse(snapshot.UnitPrice)
			if !valid || unit > (money.MaxCents-amount)/int64(line.Quantity) {
				return ErrTotal
			}
			amount += unit * int64(line.Quantity)
		}
		err = tx.QueryRow(ctx, `INSERT INTO sale_returns(tenant_id,store_id,sale_id,reason,reference_amount,created_by,idempotency_key,request_hash,source_exchange_id)
			VALUES ($1,$2,$3,$4,$5::text::numeric,$6,$7,$8,$9) RETURNING id`,
			actor.TenantID, input.StoreID, saleID, input.Reason, money.Format(amount), actor.UserID, key, digest, input.SourceExchangeID).Scan(&id)
		if err != nil {
			return err
		}
		for _, line := range input.Items {
			tag, err := tx.Exec(ctx, `INSERT INTO sale_return_items(tenant_id,store_id,sale_id,return_id,location_id,location_name,variant_id,quantity,disposition,source_exchange_id)
				SELECT l.tenant_id,l.store_id,$3,$4,l.id,l.name,$6,$7,$8,$9 FROM stock_locations l
				WHERE l.tenant_id=$1 AND l.store_id=$2 AND l.id=$5 AND l.status='active' AND l.is_sellable=($8='sellable')`,
				actor.TenantID, input.StoreID, saleID, id, line.LocationID, line.VariantID, line.Quantity, line.Disposition, input.SourceExchangeID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrReturnLocation
			}
			if err := inventory.PostReturnMovement(ctx, tx, actor, saleID, id, line.LocationID, line.VariantID, byVariant[line.VariantID].SKU, line.Quantity); err != nil {
				return err
			}
		}
		return nil
	}()
	return id, false, err
}

func (s Service) Returns(ctx context.Context, actor auth.Identity, storeID, saleID string, limit, offset int) (ReturnHistory, error) {
	if !validID(storeID) || !validID(saleID) || limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return ReturnHistory{}, ErrReturnInvalid
	}
	var history ReturnHistory
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		if err := readReturnSale(ctx, tx, actor, storeID, saleID, supervise, false, &history); err != nil {
			return err
		}
		var err error
		history.RefundOptions, err = refundOptions(ctx, tx, actor.TenantID, storeID, saleID)
		if err != nil {
			return err
		}
		if len(history.RefundOptions) == 1 {
			history.RefundMethod = &history.RefundOptions[0].Method
		}
		history.CanRefundCash = history.RefundMethod != nil && *history.RefundMethod == "cash"
		history.Items, err = returnableItems(ctx, tx, actor.TenantID, storeID, saleID, nil)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, returnSelect+" ORDER BY r.created_at DESC,r.id DESC LIMIT $4 OFFSET $5", actor.TenantID, storeID, saleID, limit+1, offset)
		if err != nil {
			return err
		}
		history.Returns, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SaleReturn, error) { return scanSaleReturn(row) })
		if err != nil {
			return err
		}
		if len(history.Returns) > limit {
			history.Returns = history.Returns[:limit]
			history.HasMore = offset+limit <= 100000
		}
		return loadExchangeRefundOptions(ctx, tx, actor.TenantID, storeID, saleID, &history)
	})
	return history, err
}

func readReturnSale(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, saleID string, supervise, lock bool, history *ReturnHistory) error {
	query := `SELECT s.sale_number::text,s.currency,
		CASE WHEN d.id IS NOT NULL THEN jsonb_build_object('method',d.method,'amount',d.amount::text) END,r.outstanding_amount::text
		FROM sales s JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		` + saleDepositJoin + `
		LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
		WHERE s.tenant_id=$1 AND s.store_id=$2 AND s.id=$3 AND ($4 OR cs.opened_by=$5)`
	if lock {
		query += " FOR NO KEY UPDATE OF s"
	}
	err := tx.QueryRow(ctx, query, actor.TenantID, storeID, saleID, supervise, actor.UserID).Scan(&history.Number, &history.Currency, &history.Deposit, &history.OutstandingDebt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func returnableItems(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID string, sourceExchangeID *string) ([]ReturnableItem, error) {
	query := `SELECT s.variant_id,s.product_name,s.sku,s.size,s.color,s.unit_price::text,s.quantity,coalesce(r.quantity,0)
		FROM sale_items s LEFT JOIN (
		    SELECT variant_id,sum(quantity) AS quantity FROM sale_return_items
		    WHERE tenant_id=$1 AND sale_id=$2 AND source_exchange_id IS NULL GROUP BY variant_id
		) r ON r.variant_id=s.variant_id WHERE s.tenant_id=$1 AND s.sale_id=$2 ORDER BY s.variant_id`
	args := []any{tenantID, saleID}
	if sourceExchangeID != nil {
		query = `SELECT i.variant_id,i.product_name,i.sku,i.size,i.color,i.unit_price::text,i.quantity,coalesce(r.quantity,0)
			FROM sale_return_exchanges e JOIN sale_return_exchange_items i ON (i.tenant_id,i.exchange_id)=(e.tenant_id,e.id)
			LEFT JOIN (
			    SELECT variant_id,sum(quantity) AS quantity FROM sale_return_items
			    WHERE tenant_id=$1 AND source_exchange_id=$4 GROUP BY variant_id
			) r ON r.variant_id=i.variant_id
			WHERE e.tenant_id=$1 AND e.sale_id=$2 AND e.store_id=$3 AND e.id=$4 ORDER BY i.variant_id`
		args = append(args, storeID, *sourceExchangeID)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReturnableItem])
}

func scanSaleReturn(row pgx.Row) (SaleReturn, error) {
	var result SaleReturn
	err := row.Scan(&result.ID, &result.SaleID, &result.Reason, &result.ReferenceAmount, &result.CreatedBy, &result.CreatedAt, &result.SourceExchangeID, &result.Items, &result.Refund, &result.DebtCredits, &result.Exchange)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, err
}
