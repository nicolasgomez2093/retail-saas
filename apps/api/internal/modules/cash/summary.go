package cash

import (
	"context"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

// SessionSummary answers the two questions of a register at a glance (how much was sold, how much cash should be
// in the drawer) and keeps the detail one level below: money by payment method, credit given and the cash breakdown.
type SessionSummary struct {
	Currency    string          `json:"currency"`
	SalesCount  int64           `json:"salesCount"`
	SalesTotal  string          `json:"salesTotal"`
	OnCredit    string          `json:"onCredit"`
	OnCreditCnt int64           `json:"onCreditCount"`
	Methods     []MethodTotal   `json:"methods"`
	Cash        []CashLineTotal `json:"cash"`
}

// MethodTotal is the net money of the session in one payment method: sales, credit payments, deposits and
// exchange charges, minus refunds.
type MethodTotal struct {
	Method   string `json:"method"`
	Amount   string `json:"amount"`
	Refunded string `json:"refunded"`
}

// CashLineTotal groups the drawer movements of the session by kind (sale_cash, manual_out, ...).
type CashLineTotal struct {
	Type   string `json:"type"`
	Amount string `json:"amount"`
	Count  int64  `json:"count"`
}

// sessionMoney: $1 tenant, $2 session. Sale payments follow the sale's session; every other movement its own.
const sessionMoney = `WITH money AS (
		SELECT sp.method,sp.amount FROM sales s JOIN sale_payments sp ON (sp.tenant_id,sp.sale_id)=(s.tenant_id,s.id)
		WHERE s.tenant_id=$1 AND s.cash_session_id=$2 AND sp.amount>0
		UNION ALL
		SELECT d.method,d.amount FROM reservation_deposits d WHERE d.tenant_id=$1 AND d.cash_session_id=$2
		UNION ALL
		SELECT p.method,p.amount FROM receivable_payments p WHERE p.tenant_id=$1 AND p.cash_session_id=$2
		UNION ALL
		SELECT cp.method,cp.amount FROM sale_return_exchange_charge_payments cp
		JOIN sale_return_exchange_charges ch ON (ch.tenant_id,ch.exchange_id)=(cp.tenant_id,cp.exchange_id)
		WHERE ch.tenant_id=$1 AND ch.cash_session_id=$2
		UNION ALL
		SELECT a.method,-a.amount FROM sale_return_refund_allocations a JOIN sale_return_refunds f ON (f.tenant_id,f.id)=(a.tenant_id,a.refund_id)
		WHERE f.tenant_id=$1 AND f.cash_session_id=$2
	)`

func (s Service) Summary(ctx context.Context, actor auth.Identity, storeID, id string) (SessionSummary, error) {
	if !validID(id) {
		return SessionSummary{}, ErrInvalid
	}
	var result SessionSummary
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error {
		if _, err := readSession(ctx, tx, actor, storeID, id, scope, false); err != nil {
			return err
		}
		result.Currency = scope.currency
		if err := tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(s.total),0.00)::text,
			coalesce(sum(r.original_amount),0.00)::text,count(r.id)
			FROM sales s LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
			WHERE s.tenant_id=$1 AND s.cash_session_id=$2`, actor.TenantID, id).
			Scan(&result.SalesCount, &result.SalesTotal, &result.OnCredit, &result.OnCreditCnt); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sessionMoney+`
			SELECT method,sum(amount)::text,coalesce(-sum(amount) FILTER (WHERE amount<0),0.00)::text
			FROM money GROUP BY method ORDER BY sum(amount) DESC,method`, actor.TenantID, id)
		if err != nil {
			return err
		}
		if result.Methods, err = pgx.CollectRows(rows, pgx.RowToStructByPos[MethodTotal]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT movement_type,sum(amount)::text,count(*) FROM cash_movements
			WHERE tenant_id=$1 AND cash_session_id=$2 GROUP BY movement_type ORDER BY sum(amount) DESC,movement_type`, actor.TenantID, id)
		if err != nil {
			return err
		}
		result.Cash, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CashLineTotal])
		return err
	})
	return result, err
}
