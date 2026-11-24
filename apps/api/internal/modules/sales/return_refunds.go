package sales

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrRefundInvalid = errors.New("verificá la venta, la devolución y la caja del reintegro")
var ErrRefundUnavailable = errors.New("la venta todavía no tiene cobros disponibles para reintegrar")
var ErrRefundMethod = errors.New("usá los medios del pago original para registrar el reintegro")
var ErrRefundAlreadyRecorded = errors.New("esta devolución ya tiene un reintegro registrado")
var ErrRefundAllocation = errors.New("distribuí el importe completo del reintegro dentro del saldo disponible de cada medio")
var ErrRefundAmount = errors.New("el importe de la devolución debe ser positivo y estar cubierto por el pago original")

type ReturnRefundInput struct {
	StoreID   string    `json:"storeId"`
	SessionID string    `json:"cashSessionId"`
	Method    string    `json:"method"`
	Payments  []Payment `json:"payments"`
}

type ReturnRefund struct {
	ID        string    `json:"id"`
	ReturnID  string    `json:"returnId"`
	SessionID string    `json:"cashSessionId"`
	Method    *string   `json:"method"`
	Amount    string    `json:"amount"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	Payments  []Payment `json:"payments"`
}

const refundFields = "id,return_id,cash_session_id,method,amount::text,created_by,created_at"

const refundAllocations = `(SELECT jsonb_agg(jsonb_build_object('method',a.method,'amount',a.amount::text) ORDER BY a.method)
	FROM sale_return_refund_allocations a WHERE a.tenant_id=f.tenant_id AND a.refund_id=f.id)`

func (s Service) AddReturnRefund(ctx context.Context, actor auth.Identity, saleID, returnID, key string, input ReturnRefundInput) (ReturnRefund, error) {
	if !validID(input.StoreID) || !validID(input.SessionID) || !validID(saleID) || !validID(returnID) || !keyPattern.MatchString(key) {
		return ReturnRefund{}, ErrRefundInvalid
	}
	if err := normalizeRefundInput(&input); err != nil {
		return ReturnRefund{}, err
	}
	var result ReturnRefund
	err := s.cash.Checkout(ctx, actor, input.StoreID, input.SessionID, func(tx pgx.Tx, session cash.Session, _ string, supervise bool) error {
		var err error
		result, err = postRefundForReturn(ctx, tx, actor, session, supervise, saleID, returnID, key, input)
		return err
	})
	return result, err
}

func postReturnRefund(ctx context.Context, tx pgx.Tx, actor auth.Identity, session cash.Session, storeID, saleID, returnID,
	key, reference string, payments []Payment, cashAmount int64, sources []exchangeRefundSource) (ReturnRefund, error) {
	var method *string
	if len(payments) == 1 {
		method = &payments[0].Method
	}
	result, err := scanReturnRefund(tx.QueryRow(ctx, `INSERT INTO sale_return_refunds
		(tenant_id,store_id,sale_id,return_id,cash_session_id,method,amount,created_by,idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7::text::numeric,$8,$9) RETURNING `+refundFields+`,NULL::jsonb`,
		actor.TenantID, storeID, saleID, returnID, session.ID, method, reference, actor.UserID, key))
	if err != nil {
		return ReturnRefund{}, err
	}
	result.Payments = payments
	for _, payment := range payments {
		if _, err := tx.Exec(ctx, `INSERT INTO sale_return_refund_allocations(tenant_id,refund_id,method,amount)
			VALUES ($1,$2,$3,$4::text::numeric)`, actor.TenantID, result.ID, payment.Method, payment.Amount); err != nil {
			return ReturnRefund{}, err
		}
	}
	if cashAmount > 0 {
		if err := cash.PostReturnRefund(ctx, tx, actor, session, saleID, result.ID, cashAmount); err != nil {
			return ReturnRefund{}, err
		}
	}
	if err := postExchangeRefundSources(ctx, tx, actor.TenantID, result.ID, payments, sources); err != nil {
		return ReturnRefund{}, err
	}
	return result, nil
}

func normalizeRefundInput(input *ReturnRefundInput) error {
	if input.Payments == nil {
		if input.Method == "" {
			input.Method = "cash"
		}
		if !knownRefundMethod(input.Method) {
			return ErrRefundInvalid
		}
		return nil
	}
	if input.Method != "" || len(input.Payments) == 0 || len(input.Payments) > 5 {
		return ErrRefundInvalid
	}
	input.Payments = slices.Clone(input.Payments)
	slices.SortFunc(input.Payments, func(a, b Payment) int { return cmp.Compare(a.Method, b.Method) })
	for i := range input.Payments {
		payment := &input.Payments[i]
		if !knownRefundMethod(payment.Method) || !normalizeAmount(&payment.Amount) || payment.Amount == "0.00" ||
			(i > 0 && input.Payments[i-1].Method == payment.Method) {
			return ErrRefundInvalid
		}
	}
	return nil
}

func knownRefundMethod(method string) bool {
	switch method {
	case "cash", "transfer", "debit", "credit", "qr":
		return true
	default:
		return false
	}
}

func sameRefundAllocation(result ReturnRefund, input ReturnRefundInput) bool {
	if input.Payments == nil {
		return result.Method != nil && *result.Method == input.Method
	}
	return slices.Equal(result.Payments, input.Payments)
}

func allocateRefund(input ReturnRefundInput, options []Payment, reference string, amount int64) ([]Payment, int64, error) {
	if len(options) == 0 {
		return nil, 0, ErrRefundUnavailable
	}
	payments := input.Payments
	if payments == nil {
		if len(options) > 1 {
			return nil, 0, ErrRefundAllocation
		}
		if options[0].Method != input.Method {
			return nil, 0, ErrRefundMethod
		}
		payments = []Payment{{Method: input.Method, Amount: reference}}
	}
	available := make(map[string]int64, len(options))
	for _, option := range options {
		cents, valid := money.Parse(option.Amount)
		if !valid {
			return nil, 0, ErrRefundAmount
		}
		available[option.Method] = cents
	}
	var cashAmount int64
	remaining := amount
	for _, payment := range payments {
		limit, found := available[payment.Method]
		if !found {
			return nil, 0, ErrRefundMethod
		}
		cents, _ := money.Parse(payment.Amount)
		if cents > limit || cents > remaining {
			return nil, 0, ErrRefundAllocation
		}
		remaining -= cents
		if payment.Method == "cash" {
			cashAmount = cents
		}
	}
	if remaining != 0 {
		return nil, 0, ErrRefundAllocation
	}
	return payments, cashAmount, nil
}

func refundOptions(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID string) ([]Payment, error) {
	rows, err := tx.Query(ctx, `WITH refundable_sale AS (
		SELECT s.tenant_id,s.id,s.total,d.method AS deposit_method,d.amount AS deposit_amount,
		    r.id AS receivable_id,COALESCE(r.outstanding_amount,0) AS outstanding
		FROM sales s `+saleDepositJoin+`
		LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
		WHERE s.tenant_id=$1 AND s.store_id=$2 AND s.id=$3
	), paid AS (
		SELECT p.method,p.amount FROM refundable_sale s
		JOIN sale_payments p ON (p.tenant_id,p.sale_id)=(s.tenant_id,s.id)
		WHERE p.amount > 0
		UNION ALL
		SELECT deposit_method,deposit_amount FROM refundable_sale WHERE deposit_amount IS NOT NULL
		UNION ALL
		SELECT p.method,p.amount FROM refundable_sale s
		JOIN receivable_payments p ON (p.tenant_id,p.receivable_id)=(s.tenant_id,s.receivable_id)
		WHERE p.store_id=$2
	), refunded AS (
		SELECT a.method,sum(a.amount-COALESCE((
		    SELECT sum(c.amount) FROM sale_return_refund_charge_allocations c
		    WHERE c.tenant_id=a.tenant_id AND c.refund_id=a.refund_id AND c.method=a.method
		),0)) AS amount FROM sale_return_refunds f
		JOIN sale_return_refund_allocations a ON (a.tenant_id,a.refund_id)=(f.tenant_id,f.id)
		WHERE f.tenant_id=$1 AND f.store_id=$2 AND f.sale_id=$3 GROUP BY a.method
	)
	SELECT p.method,(sum(p.amount)-COALESCE(r.amount,0.00))::text
	FROM paid p LEFT JOIN refunded r ON r.method=p.method
	WHERE (SELECT sum(amount) FROM paid)=(SELECT total-outstanding FROM refundable_sale)
	    - COALESCE((SELECT sum(amount) FROM sale_return_debt_credits WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3),0)
	GROUP BY p.method,r.amount ORDER BY p.method`, tenantID, storeID, saleID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Payment])
}

func scanReturnRefund(row pgx.Row) (ReturnRefund, error) {
	var result ReturnRefund
	err := row.Scan(&result.ID, &result.ReturnID, &result.SessionID, &result.Method, &result.Amount, &result.CreatedBy, &result.CreatedAt, &result.Payments)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, err
}

// postRefundForReturn pays back what the return leaves after debt credits,
// with the methods of the original payments, inside the caller's transaction.
func postRefundForReturn(ctx context.Context, tx pgx.Tx, actor auth.Identity, session cash.Session, supervise bool, saleID, returnID, key string,
	input ReturnRefundInput) (ReturnRefund, error) {
	var result ReturnRefund
	err := func() error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:return-refund:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var history ReturnHistory
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, true, &history); err != nil {
			return err
		}
		var reference string
		var fromExchange bool
		err := tx.QueryRow(ctx, `SELECT reference_amount::text,source_exchange_id IS NOT NULL FROM sale_returns
			WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND id=$4`, actor.TenantID, input.StoreID, saleID, returnID).Scan(&reference, &fromExchange)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		result, err = scanReturnRefund(tx.QueryRow(ctx, "SELECT "+refundFields+","+refundAllocations+" FROM sale_return_refunds f WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key))
		if err == nil {
			if result.ReturnID != returnID || result.SessionID != session.ID || result.CreatedBy != actor.UserID || !sameRefundAllocation(result, input) {
				return cash.ErrIdempotency
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if session.Status != "OPEN" {
			return cash.ErrSessionState
		}
		var exchanged bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sale_return_exchanges
			WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND return_id=$4)`,
			actor.TenantID, input.StoreID, saleID, returnID).Scan(&exchanged); err != nil {
			return err
		}
		if exchanged {
			return ErrExchangeUnavailable
		}
		if history.OutstandingDebt != nil {
			if input.Payments == nil {
				return ErrRefundAllocation
			}
			if _, err := lockReceivableForReturn(ctx, tx, actor.TenantID, input.StoreID, saleID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT ($5::text::numeric-COALESCE(sum(amount),0.00))::text
				FROM sale_return_debt_credits WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND return_id=$4`,
				actor.TenantID, input.StoreID, saleID, returnID, reference).Scan(&reference); err != nil {
				return err
			}
		}
		amount, valid := money.Parse(reference)
		if !valid || amount == 0 {
			return ErrRefundAmount
		}
		var recorded, fits bool
		err = tx.QueryRow(ctx, `SELECT COALESCE(bool_or(f.return_id=$4),false), $5::text::numeric+COALESCE(sum(f.amount-COALESCE((
			    SELECT sum(a.amount) FROM sale_return_refund_charge_allocations a WHERE a.tenant_id=f.tenant_id AND a.refund_id=f.id
			),0)),0)<=s.total
			FROM sales s LEFT JOIN sale_return_refunds f ON (f.tenant_id,f.store_id,f.sale_id)=(s.tenant_id,s.store_id,s.id)
			WHERE s.tenant_id=$1 AND s.store_id=$2 AND s.id=$3 GROUP BY s.total`,
			actor.TenantID, input.StoreID, saleID, returnID, reference).Scan(&recorded, &fits)
		if err != nil {
			return err
		}
		if recorded {
			return ErrRefundAlreadyRecorded
		}
		if !fromExchange && !fits {
			return ErrRefundAmount
		}
		options, sources, err := refundOptionsForReceipt(ctx, tx, actor.TenantID, input.StoreID, saleID, returnID, fromExchange)
		if err != nil {
			return err
		}
		payments, cashAmount, err := allocateRefund(input, options, reference, amount)
		if err != nil {
			return err
		}
		result, err = postReturnRefund(ctx, tx, actor, session, input.StoreID, saleID, returnID, key, reference, payments, cashAmount, sources)
		return err
	}()
	return result, err
}
