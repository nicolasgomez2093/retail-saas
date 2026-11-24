package sales

import (
	"context"
	"errors"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrReceivableNotFound = errors.New("no encontramos el fiado")
var ErrReceivableAmount = errors.New("el abono supera el saldo pendiente")

type ReceivablePaymentInput struct {
	StoreID   string `json:"storeId"`
	SessionID string `json:"cashSessionId"`
	Payment
}

type ReceivablePayment struct {
	ID           string    `json:"id"`
	ReceivableID string    `json:"receivableId"`
	SessionID    string    `json:"cashSessionId"`
	Method       string    `json:"method"`
	Amount       string    `json:"amount"`
	BalanceAfter string    `json:"balanceAfter"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ReceivablePaymentPage struct {
	Items             []ReceivablePayment `json:"items"`
	DebtCredits       []ReturnDebtCredit  `json:"debtCredits"`
	HasMore           bool                `json:"hasMore"`
	OutstandingAmount string              `json:"outstandingAmount"`
}

const paymentFields = "id,receivable_id,cash_session_id,method,amount::text,balance_after::text,created_by,created_at"

func (s Service) AddReceivablePayment(ctx context.Context, actor auth.Identity, id, key string, input ReceivablePaymentInput) (ReceivablePayment, error) {
	amount, valid := money.Parse(input.Amount)
	if !valid || amount == 0 || !validID(id) || !validID(input.StoreID) || !keyPattern.MatchString(key) {
		return ReceivablePayment{}, ErrInvalid
	}
	switch input.Method {
	case "cash", "transfer", "debit", "credit", "qr":
	default:
		return ReceivablePayment{}, ErrInvalid
	}
	input.Amount = money.Format(amount)
	var result ReceivablePayment
	err := s.cash.Checkout(ctx, actor, input.StoreID, input.SessionID, func(tx pgx.Tx, session cash.Session, _ string, supervise bool) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
			"retail:receivable-payment:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		saleID, outstanding, err := readReceivableBalance(ctx, tx, actor, input.StoreID, id, supervise, true)
		if err != nil {
			return err
		}
		prior, err := scanReceivablePayment(tx.QueryRow(ctx, "SELECT "+paymentFields+
			" FROM receivable_payments WHERE tenant_id=$1 AND idempotency_key=$2", actor.TenantID, key))
		if err == nil {
			if prior.ReceivableID != id || prior.SessionID != session.ID || prior.CreatedBy != actor.UserID ||
				prior.Method != input.Method || prior.Amount != input.Amount {
				return cash.ErrIdempotency
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if session.Status != "OPEN" {
			return cash.ErrSessionState
		}
		if amount > outstanding {
			return ErrReceivableAmount
		}
		balance := money.Format(outstanding - amount)
		result, err = scanReceivablePayment(tx.QueryRow(ctx, `INSERT INTO receivable_payments
			(tenant_id,store_id,receivable_id,cash_session_id,method,amount,balance_after,created_by,idempotency_key)
			VALUES ($1,$2,$3,$4,$5,$6::text::numeric,$7::text::numeric,$8,$9) RETURNING `+paymentFields,
			actor.TenantID, input.StoreID, id, session.ID, input.Method, input.Amount, balance, actor.UserID, key))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE receivables SET outstanding_amount=$4::text::numeric
			WHERE tenant_id=$1 AND store_id=$2 AND id=$3`, actor.TenantID, input.StoreID, id, balance); err != nil {
			return err
		}
		if input.Method == "cash" {
			return cash.PostReceivableCash(ctx, tx, actor, session, saleID, result.ID, amount)
		}
		return nil
	})
	return result, err
}

func (s Service) ReceivablePayments(ctx context.Context, actor auth.Identity, storeID, id string, limit, offset int) (ReceivablePaymentPage, error) {
	if !validID(storeID) || !validID(id) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return ReceivablePaymentPage{}, ErrInvalid
	}
	var page ReceivablePaymentPage
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		saleID, balance, err := readReceivableBalance(ctx, tx, actor, storeID, id, supervise, false)
		if err != nil {
			return err
		}
		page.OutstandingAmount = money.Format(balance)
		rows, err := tx.Query(ctx, "SELECT "+paymentFields+` FROM receivable_payments
			WHERE tenant_id=$1 AND store_id=$2 AND receivable_id=$3 ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`,
			actor.TenantID, storeID, id, limit+1, offset)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReceivablePayment, error) {
			return scanReceivablePayment(row)
		})
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		if err != nil {
			return err
		}
		credits, err := tx.Query(ctx, "SELECT "+debtCreditFields+` FROM sale_return_debt_credits
			WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`,
			actor.TenantID, storeID, saleID, limit+1, offset)
		if err != nil {
			return err
		}
		page.DebtCredits, err = pgx.CollectRows(credits, func(row pgx.CollectableRow) (ReturnDebtCredit, error) {
			return scanReturnDebtCredit(row)
		})
		if len(page.DebtCredits) > limit {
			page.DebtCredits = page.DebtCredits[:limit]
			page.HasMore = offset+limit <= 100000
		}
		return err
	})
	return page, err
}

func readReceivableBalance(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, id string, supervise, lock bool) (string, int64, error) {
	query := `SELECT r.sale_id,r.outstanding_amount::text FROM receivables r
		JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(r.tenant_id,r.store_id,r.sale_id)
		JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.id=$3 AND ($4 OR cs.opened_by=$5)`
	if lock {
		query += " FOR UPDATE OF r"
	}
	var saleID, balance string
	err := tx.QueryRow(ctx, query, actor.TenantID, storeID, id, supervise, actor.UserID).Scan(&saleID, &balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrReceivableNotFound
	}
	if err != nil {
		return "", 0, err
	}
	outstanding, valid := money.Parse(balance)
	if !valid {
		return "", 0, errors.New("saldo de fiado inválido en la base")
	}
	return saleID, outstanding, nil
}

func scanReceivablePayment(row pgx.Row) (ReceivablePayment, error) {
	var payment ReceivablePayment
	err := row.Scan(&payment.ID, &payment.ReceivableID, &payment.SessionID, &payment.Method, &payment.Amount,
		&payment.BalanceAfter, &payment.CreatedBy, &payment.CreatedAt)
	payment.CreatedAt = payment.CreatedAt.UTC()
	return payment, err
}
