package sales

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrExchangeInvalid = errors.New("verificá la ubicación, las variantes y las cantidades del cambio")
var ErrExchangeUnavailable = errors.New("esta devolución ya tiene una resolución registrada")
var ErrExchangeStock = errors.New("stock disponible insuficiente para entregar las prendas del cambio")
var ErrExchangeDifference = errors.New("el cobro o reintegro debe corresponder a la diferencia del cambio")
var ErrExchangeChargeAmount = errors.New("el importe cobrado debe coincidir con la diferencia del cambio")
var ErrExchangeKey = errors.New("la clave de idempotencia ya se usó para otro cambio")

type ReturnExchangeInput struct {
	DebtCreditAmount *string              `json:"debtCreditAmount,omitempty"`
	StoreID          string               `json:"storeId"`
	LocationID       string               `json:"locationId"`
	Items            []ItemInput          `json:"items"`
	Charge           *ExchangeChargeInput `json:"charge,omitempty"`
	Refund           *ExchangeRefundInput `json:"refund,omitempty"`
}

type ExchangeChargeInput struct {
	SessionID string `json:"cashSessionId"`
	PaymentInput
	Payments []PaymentInput `json:"payments,omitempty"`
}

type ExchangeRefundInput struct {
	SessionID string    `json:"cashSessionId"`
	Payments  []Payment `json:"payments"`
}

type ExchangeChargePayment struct {
	PaymentInput
	Change *string `json:"change,omitempty"`
}

type ExchangeCharge struct {
	SessionID    string                  `json:"cashSessionId"`
	Method       *string                 `json:"method"`
	Amount       string                  `json:"amount"`
	CashReceived *string                 `json:"cashReceived,omitempty"`
	Change       *string                 `json:"change,omitempty"`
	Payments     []ExchangeChargePayment `json:"payments"`
}

type ReturnExchange struct {
	ReturnedQuantities map[string]int32 `json:"returnedQuantities,omitempty"`
	ID                 string           `json:"id"`
	ReturnID           string           `json:"returnId"`
	LocationID         string           `json:"locationId"`
	LocationName       string           `json:"locationName"`
	Total              string           `json:"total"`
	CreatedBy          string           `json:"createdBy"`
	CreatedAt          time.Time        `json:"createdAt"`
	Items              []Item           `json:"items"`
	Charge             *ExchangeCharge  `json:"charge,omitempty"`
}

const exchangeItems = `(SELECT jsonb_agg(jsonb_build_object('variantId',i.variant_id,'product',i.product_name,
	'sku',i.sku,'size',i.size,'color',i.color,'quantity',i.quantity,'unitPrice',i.unit_price::text,
	'subtotal',(i.unit_price*i.quantity)::text) ORDER BY i.variant_id)
	FROM sale_return_exchange_items i WHERE (i.tenant_id,i.exchange_id)=(e.tenant_id,e.id))`

const exchangeReturnedQuantities = `(SELECT jsonb_object_agg(returned.variant_id,returned.quantity) FROM (
	SELECT i.variant_id,sum(i.quantity) AS quantity FROM sale_return_items i
	WHERE i.tenant_id=e.tenant_id AND i.source_exchange_id=e.id GROUP BY i.variant_id
) returned)`

const exchangeCharge = `(SELECT jsonb_strip_nulls(jsonb_build_object('cashSessionId',p.cash_session_id,'method',p.method,
	'amount',p.amount::text,'cashReceived',p.cash_received::text,'change',p.change_amount::text,
	'payments',(SELECT jsonb_agg(jsonb_build_object('method',part.method,'amount',part.amount::text,
	    'cashReceived',part.cash_received::text,'change',part.change_amount::text) ORDER BY part.method)
	FROM sale_return_exchange_charge_payments part WHERE (part.tenant_id,part.exchange_id)=(p.tenant_id,p.exchange_id))))
	FROM sale_return_exchange_charges p WHERE (p.tenant_id,p.exchange_id)=(e.tenant_id,e.id))`

func (s Service) AddReturnExchange(ctx context.Context, actor auth.Identity, saleID, returnID, key string, input ReturnExchangeInput) (ReturnExchange, error) {
	if !validID(input.StoreID) || !validID(input.LocationID) || !validID(saleID) || !validID(returnID) ||
		!keyPattern.MatchString(key) || len(input.Items) == 0 || len(input.Items) > 100 {
		return ReturnExchange{}, ErrExchangeInvalid
	}
	creditAmount, chargePayments, err := normalizeExchangeInput(&input)
	if err != nil {
		return ReturnExchange{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return ReturnExchange{}, err
	}
	digest := sha256.Sum256(body)
	var result ReturnExchange
	operation := func(tx pgx.Tx, session *cash.Session, supervise bool) error {
		var err error
		result, err = postExchange(ctx, tx, actor, session, supervise, saleID, returnID, key, input, digest[:], creditAmount, chargePayments)
		return err
	}
	var sessionID string
	if input.Charge != nil {
		sessionID = input.Charge.SessionID
	}
	if input.Refund != nil {
		sessionID = input.Refund.SessionID
	}
	if sessionID == "" {
		err = s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, supervise bool) error {
			return operation(tx, nil, supervise)
		})
	} else {
		err = s.cash.Checkout(ctx, actor, input.StoreID, sessionID, func(tx pgx.Tx, session cash.Session, _ string, supervise bool) error {
			return operation(tx, &session, supervise)
		})
	}
	return result, err
}

func postExchangeCharge(ctx context.Context, tx pgx.Tx, actor auth.Identity, session cash.Session, saleID, exchangeID string,
	difference int64, payments []appliedPayment) error {
	var method, received, change *string
	if len(payments) == 1 {
		method, received, change = &payments[0].Method, payments[0].CashReceived, payments[0].change
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sale_return_exchange_charges
		(tenant_id,exchange_id,cash_session_id,method,amount,cash_received,change_amount)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6::text::numeric,$7::text::numeric)`,
		actor.TenantID, exchangeID, session.ID, method, money.Format(difference), received, change); err != nil {
		return err
	}
	for _, payment := range payments {
		if _, err := tx.Exec(ctx, `INSERT INTO sale_return_exchange_charge_payments
			(tenant_id,exchange_id,method,amount,cash_received,change_amount)
			VALUES ($1,$2,$3,$4::text::numeric,$5::text::numeric,$6::text::numeric)`,
			actor.TenantID, exchangeID, payment.Method, payment.Amount, payment.CashReceived, payment.change); err != nil {
			return err
		}
		if payment.Method == "cash" {
			amount, _ := money.Parse(payment.Amount)
			if err := cash.PostExchangeCash(ctx, tx, actor, session, saleID, exchangeID, amount); err != nil {
				return err
			}
		}
	}
	return nil
}

func readExchange(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID, id string) (ReturnExchange, error) {
	var result ReturnExchange
	err := tx.QueryRow(ctx, `SELECT e.id,e.return_id,e.location_id,e.location_name,e.total::text,e.created_by,e.created_at,`+exchangeItems+","+exchangeCharge+`
		FROM sale_return_exchanges e WHERE e.tenant_id=$1 AND e.store_id=$2 AND e.sale_id=$3 AND e.id=$4`,
		tenantID, storeID, saleID, id).Scan(&result.ID, &result.ReturnID, &result.LocationID, &result.LocationName, &result.Total, &result.CreatedBy,
		&result.CreatedAt, &result.Items, &result.Charge)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, err
}

// postExchange delivers the replacement garments of a recorded return and settles
// the difference (charge, debt credit and refund) inside the caller's transaction.
func postExchange(ctx context.Context, tx pgx.Tx, actor auth.Identity, session *cash.Session, supervise bool, saleID, returnID, key string,
	input ReturnExchangeInput, digest []byte, creditAmount int64, chargePayments []PaymentInput) (ReturnExchange, error) {
	var result ReturnExchange
	err := func() error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
			"retail:return-exchange:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var history ReturnHistory
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, true, &history); err != nil {
			return err
		}
		var reference string
		var resolved, fromExchange bool
		err := tx.QueryRow(ctx, `SELECT r.reference_amount::text,f.id IS NOT NULL OR e.id IS NOT NULL,r.source_exchange_id IS NOT NULL
			FROM sale_returns r LEFT JOIN sale_return_refunds f
			    ON (f.tenant_id,f.store_id,f.sale_id,f.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)
			LEFT JOIN sale_return_exchanges e
			    ON (e.tenant_id,e.store_id,e.sale_id,e.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)
			WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.sale_id=$3 AND r.id=$4`,
			actor.TenantID, input.StoreID, saleID, returnID).Scan(&reference, &resolved, &fromExchange)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var previousID, previousReturn, creator string
		var previousHash []byte
		err = tx.QueryRow(ctx, `SELECT id,return_id,created_by,request_hash FROM sale_return_exchanges
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&previousID, &previousReturn, &creator, &previousHash)
		if err == nil {
			if previousReturn != returnID || creator != actor.UserID || !bytes.Equal(previousHash, digest[:]) {
				return ErrExchangeKey
			}
			result, err = readExchange(ctx, tx, actor.TenantID, input.StoreID, saleID, previousID)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if resolved {
			return ErrExchangeUnavailable
		}
		var balance int64
		if history.OutstandingDebt != nil {
			balance, err = lockReceivableForReturn(ctx, tx, actor.TenantID, input.StoreID, saleID)
			if err != nil {
				return err
			}
			var credited bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sale_return_debt_credits
				WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND return_id=$4)`,
				actor.TenantID, input.StoreID, saleID, returnID).Scan(&credited); err != nil {
				return err
			}
			if credited {
				return ErrExchangeUnavailable
			}
		}
		lines, total, err := quote(ctx, tx, actor, Input{StoreID: input.StoreID, Items: input.Items})
		if err != nil {
			return err
		}
		historical, valid := money.Parse(reference)
		difference := total - historical
		if !valid || (difference > 0) != (input.Charge != nil) {
			return ErrExchangeDifference
		}
		if creditAmount > 0 && (creditAmount > balance || creditAmount > -difference) {
			return ErrDebtCreditAmount
		}
		refundAmount := -difference - creditAmount
		if (refundAmount > 0) != (input.Refund != nil) {
			return ErrExchangeDifference
		}
		if session != nil && session.Status != "OPEN" {
			return cash.ErrSessionState
		}
		var applied []appliedPayment
		var refundParts []Payment
		var refundSources []exchangeRefundSource
		var cashRefund int64
		if input.Charge != nil {
			applied, _, err = applyPayments(Input{Payments: chargePayments}, difference)
			if errors.Is(err, ErrPaymentTotal) {
				return ErrExchangeChargeAmount
			}
			if err != nil {
				return err
			}
		}
		if input.Refund != nil {
			var options []Payment
			options, refundSources, err = refundOptionsForReceipt(ctx, tx, actor.TenantID, input.StoreID, saleID, returnID, fromExchange)
			if err != nil {
				return err
			}
			refundParts, cashRefund, err = allocateRefund(ReturnRefundInput{Payments: input.Refund.Payments}, options, money.Format(refundAmount), refundAmount)
			if err != nil {
				return err
			}
		}
		var exchangeID string
		err = tx.QueryRow(ctx, `INSERT INTO sale_return_exchanges(tenant_id,store_id,sale_id,return_id,location_id,location_name,
			total,created_by,idempotency_key,request_hash)
			SELECT l.tenant_id,l.store_id,$3,$4,l.id,l.name,$6::text::numeric,$7,$8,$9 FROM stock_locations l
			WHERE l.tenant_id=$1 AND l.store_id=$2 AND l.id=$5 AND l.status='active' AND l.is_sellable RETURNING id`,
			actor.TenantID, input.StoreID, saleID, returnID, input.LocationID, money.Format(total), actor.UserID, key, digest[:]).Scan(&exchangeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLocation
		}
		if err != nil {
			return err
		}
		for _, line := range lines {
			_, err = tx.Exec(ctx, `INSERT INTO sale_return_exchange_items(tenant_id,exchange_id,variant_id,product_name,sku,size,color,quantity,unit_price)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::text::numeric)`,
				actor.TenantID, exchangeID, line.VariantID, line.Product, line.SKU, line.Size, line.Color, line.Quantity, line.UnitPrice)
			if err != nil {
				return err
			}
			if err := inventory.PostExchangeMovement(ctx, tx, actor, saleID, exchangeID, input.LocationID, line.VariantID, line.SKU, line.Quantity); err != nil {
				if errors.Is(err, inventory.ErrBalance) {
					return ErrExchangeStock
				}
				return err
			}
		}
		if input.Charge != nil {
			if err := postExchangeCharge(ctx, tx, actor, *session, saleID, exchangeID, difference, applied); err != nil {
				return err
			}
		}
		// The colon keeps generated keys outside the public idempotency key namespaces.
		if creditAmount > 0 {
			if _, err := postReturnDebtCredit(ctx, tx, actor, input.StoreID, saleID, returnID,
				"exchange-debt-credit:"+exchangeID, creditAmount, balance); err != nil {
				return err
			}
		}
		if input.Refund != nil {
			if _, err := postReturnRefund(ctx, tx, actor, *session, input.StoreID, saleID, returnID,
				"exchange-refund:"+exchangeID, money.Format(refundAmount), refundParts, cashRefund, refundSources); err != nil {
				return err
			}
		}
		result, err = readExchange(ctx, tx, actor.TenantID, input.StoreID, saleID, exchangeID)
		return err
	}()
	return result, err
}

// normalizeExchangeInput sorts and validates replacements, charge, refund and
// debt credit so equal requests produce the same idempotency hash.
func normalizeExchangeInput(input *ReturnExchangeInput) (int64, []PaymentInput, error) {
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b ItemInput) int { return cmp.Compare(a.VariantID, b.VariantID) })
	for index := range input.Items {
		item := &input.Items[index]
		if !validID(item.VariantID) || item.DiscountPercent != 0 || item.Quantity <= 0 || !normalizeAmount(&item.ExpectedPrice) ||
			(index > 0 && item.VariantID == input.Items[index-1].VariantID) {
			return 0, nil, ErrExchangeInvalid
		}
	}
	var chargePayments []PaymentInput
	if input.Charge != nil {
		charge := *input.Charge
		split := charge.Payments != nil
		if !validID(charge.SessionID) || (split && (charge.Method != "" || charge.Amount != "" || charge.CashReceived != nil)) {
			return 0, nil, ErrExchangeInvalid
		}
		paymentInput := Input{Payments: charge.Payments}
		if !split {
			paymentInput.Payments = []PaymentInput{charge.PaymentInput}
		}
		if normalizePaymentInput(&paymentInput) != nil {
			return 0, nil, ErrExchangeInvalid
		}
		chargePayments = paymentInput.Payments
		if split {
			charge.Payments = chargePayments
		} else {
			charge.PaymentInput = chargePayments[0]
		}
		input.Charge = &charge
	}
	if input.Refund != nil {
		refund := *input.Refund
		refundInput := ReturnRefundInput{Payments: refund.Payments}
		if input.Charge != nil || !validID(refund.SessionID) || len(refund.Payments) == 0 || normalizeRefundInput(&refundInput) != nil {
			return 0, nil, ErrExchangeInvalid
		}
		refund.Payments = refundInput.Payments
		input.Refund = &refund
	}
	var creditAmount int64
	if input.DebtCreditAmount != nil {
		var valid bool
		creditAmount, valid = money.Parse(*input.DebtCreditAmount)
		if !valid || creditAmount == 0 || input.Charge != nil {
			return 0, nil, ErrExchangeInvalid
		}
		canonical := money.Format(creditAmount)
		input.DebtCreditAmount = &canonical
	}
	return creditAmount, chargePayments, nil
}
