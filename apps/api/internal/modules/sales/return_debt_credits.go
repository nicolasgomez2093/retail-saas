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

var ErrDebtCreditInvalid = errors.New("verificá la venta, la devolución y el importe de la compensación")
var ErrDebtCreditAmount = errors.New("la compensación supera el saldo del fiado o el importe pendiente de la devolución")

type ReturnDebtCreditInput struct {
	StoreID string `json:"storeId"`
	Amount  string `json:"amount"`
}

type ReturnDebtCredit struct {
	ID           string    `json:"id"`
	ReturnID     string    `json:"returnId"`
	Amount       string    `json:"amount"`
	BalanceAfter string    `json:"balanceAfter"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

const debtCreditFields = "id,return_id,amount::text,balance_after::text,created_by,created_at"

func (s Service) AddReturnDebtCredit(ctx context.Context, actor auth.Identity, saleID, returnID, key string, input ReturnDebtCreditInput) (ReturnDebtCredit, error) {
	amount, valid := money.Parse(input.Amount)
	if !valid || amount == 0 || !validID(input.StoreID) || !validID(saleID) || !validID(returnID) || !keyPattern.MatchString(key) {
		return ReturnDebtCredit{}, ErrDebtCreditInvalid
	}
	input.Amount = money.Format(amount)
	var result ReturnDebtCredit
	err := s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, supervise bool) error {
		var err error
		result, err = postDebtCreditForReturn(ctx, tx, actor, supervise, saleID, returnID, key, input, amount)
		return err
	})
	return result, err
}

func postReturnDebtCredit(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, saleID, returnID, key string,
	amount, balance int64) (ReturnDebtCredit, error) {
	next := money.Format(balance - amount)
	result, err := scanReturnDebtCredit(tx.QueryRow(ctx, `INSERT INTO sale_return_debt_credits
		(tenant_id,store_id,sale_id,return_id,amount,balance_after,created_by,idempotency_key)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6::text::numeric,$7,$8) RETURNING `+debtCreditFields,
		actor.TenantID, storeID, saleID, returnID, money.Format(amount), next, actor.UserID, key))
	if err != nil {
		return ReturnDebtCredit{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE receivables SET outstanding_amount=$4::text::numeric
		WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3`, actor.TenantID, storeID, saleID, next)
	return result, err
}

// Use the same row lock as receivable payments to serialize every financial resolution of a return.
func lockReceivableForReturn(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID string) (int64, error) {
	var balanceText string
	err := tx.QueryRow(ctx, `SELECT outstanding_amount::text FROM receivables
		WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 FOR UPDATE`, tenantID, storeID, saleID).Scan(&balanceText)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrReceivableNotFound
	}
	if err != nil {
		return 0, err
	}
	balance, valid := money.Parse(balanceText)
	if !valid {
		return 0, errors.New("saldo de fiado inválido en la base")
	}
	return balance, nil
}

func scanReturnDebtCredit(row pgx.Row) (ReturnDebtCredit, error) {
	var result ReturnDebtCredit
	err := row.Scan(&result.ID, &result.ReturnID, &result.Amount, &result.BalanceAfter, &result.CreatedBy, &result.CreatedAt)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, err
}

// postDebtCreditForReturn lowers the fiado balance with the value of a return
// inside the caller's transaction.
func postDebtCreditForReturn(ctx context.Context, tx pgx.Tx, actor auth.Identity, supervise bool, saleID, returnID, key string,
	input ReturnDebtCreditInput, amount int64) (ReturnDebtCredit, error) {
	var result ReturnDebtCredit
	err := func() error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
			"retail:return-debt-credit:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var history ReturnHistory
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, false, &history); err != nil {
			return err
		}
		if history.OutstandingDebt == nil {
			return ErrReceivableNotFound
		}
		var reference string
		err := tx.QueryRow(ctx, `SELECT reference_amount::text FROM sale_returns
			WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND id=$4`,
			actor.TenantID, input.StoreID, saleID, returnID).Scan(&reference)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		result, err = scanReturnDebtCredit(tx.QueryRow(ctx, "SELECT "+debtCreditFields+
			" FROM sale_return_debt_credits WHERE tenant_id=$1 AND idempotency_key=$2", actor.TenantID, key))
		if err == nil {
			if result.ReturnID != returnID || result.CreatedBy != actor.UserID || result.Amount != input.Amount {
				return cash.ErrIdempotency
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		balance, err := lockReceivableForReturn(ctx, tx, actor.TenantID, input.StoreID, saleID)
		if err != nil {
			return err
		}
		if amount > balance {
			return ErrDebtCreditAmount
		}
		var fits, exchanged bool
		err = tx.QueryRow(ctx, `SELECT $5::text::numeric+COALESCE(sum(amount),0)<=$6::text::numeric
			    - COALESCE((SELECT f.amount FROM sale_return_refunds f
			      WHERE f.tenant_id=$1 AND f.store_id=$2 AND f.sale_id=$3 AND f.return_id=$4),0)
			, EXISTS (SELECT 1 FROM sale_return_exchanges e
			    WHERE e.tenant_id=$1 AND e.store_id=$2 AND e.sale_id=$3 AND e.return_id=$4)
			FROM sale_return_debt_credits WHERE tenant_id=$1 AND store_id=$2 AND sale_id=$3 AND return_id=$4`,
			actor.TenantID, input.StoreID, saleID, returnID, input.Amount, reference).Scan(&fits, &exchanged)
		if err != nil {
			return err
		}
		if exchanged {
			return ErrExchangeUnavailable
		}
		if !fits {
			return ErrDebtCreditAmount
		}
		result, err = postReturnDebtCredit(ctx, tx, actor, input.StoreID, saleID, returnID, key, amount, balance)
		return err
	}()
	return result, err
}
