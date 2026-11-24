package sales

import (
	"context"

	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

type exchangeRefundSource struct {
	ReturnID   string
	ExchangeID string
	Method     string
	Cents      int64
}

func refundOptionsForReceipt(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID, returnID string, fromExchange bool) ([]Payment, []exchangeRefundSource, error) {
	options, err := refundOptions(ctx, tx, tenantID, storeID, saleID)
	if err != nil || !fromExchange {
		return options, nil, err
	}
	sources, err := exchangeRefundSources(ctx, tx, tenantID, storeID, saleID, []string{returnID})
	if err != nil {
		return nil, nil, err
	}
	return combineRefundOptions(options, sources), sources, nil
}

func exchangeRefundSources(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID string, returnIDs []string) ([]exchangeRefundSource, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE chain(return_id, exchange_id, depth) AS (
		SELECT r.id,r.source_exchange_id,1 FROM sale_returns r
		WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.sale_id=$3 AND r.id=ANY($4::text[]::uuid[]) AND r.source_exchange_id IS NOT NULL
		UNION ALL
		SELECT c.return_id,r.source_exchange_id,c.depth+1 FROM chain c
		JOIN sale_return_exchanges e ON e.tenant_id=$1 AND e.store_id=$2 AND e.sale_id=$3 AND e.id=c.exchange_id
		JOIN sale_returns r ON r.tenant_id=$1 AND r.store_id=$2 AND r.sale_id=$3 AND r.id=e.return_id
		WHERE r.source_exchange_id IS NOT NULL
	)
	SELECT c.return_id,c.exchange_id,p.method,((p.amount-COALESCE(a.amount,0))*100)::bigint
	FROM chain c
	JOIN sale_return_exchange_charge_payments p ON p.tenant_id=$1 AND p.exchange_id=c.exchange_id
	CROSS JOIN LATERAL (SELECT sum(amount) AS amount FROM sale_return_refund_charge_allocations
		WHERE tenant_id=p.tenant_id AND exchange_id=p.exchange_id AND method=p.method) a
	WHERE p.amount>COALESCE(a.amount,0)
	ORDER BY c.return_id,c.depth,p.method`, tenantID, storeID, saleID, returnIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[exchangeRefundSource])
}

func combineRefundOptions(original []Payment, sources []exchangeRefundSource) []Payment {
	if len(sources) == 0 {
		return original
	}
	balances := make(map[string]int64, len(original))
	for _, payment := range original {
		balances[payment.Method], _ = money.Parse(payment.Amount)
	}
	for _, source := range sources {
		balances[source.Method] = min(money.MaxCents, balances[source.Method]+source.Cents)
	}
	options := make([]Payment, 0, len(balances))
	for _, method := range []string{"cash", "credit", "debit", "qr", "transfer"} {
		if balance := balances[method]; balance > 0 {
			options = append(options, Payment{Method: method, Amount: money.Format(balance)})
		}
	}
	return options
}

func postExchangeRefundSources(ctx context.Context, tx pgx.Tx, tenantID, refundID string, payments []Payment, sources []exchangeRefundSource) error {
	for _, payment := range payments {
		remaining, _ := money.Parse(payment.Amount)
		// The nearest exchange charge is consumed first. Any remainder is funded by the original sale.
		for _, source := range sources {
			if remaining == 0 {
				break
			}
			if source.Method != payment.Method {
				continue
			}
			amount := min(remaining, source.Cents)
			if _, err := tx.Exec(ctx, `INSERT INTO sale_return_refund_charge_allocations(tenant_id,refund_id,method,exchange_id,amount)
				VALUES ($1,$2,$3,$4,$5::text::numeric)`, tenantID, refundID, payment.Method, source.ExchangeID, money.Format(amount)); err != nil {
				return err
			}
			remaining -= amount
		}
	}
	return nil
}

func loadExchangeRefundOptions(ctx context.Context, tx pgx.Tx, tenantID, storeID, saleID string, history *ReturnHistory) error {
	var returnIDs []string
	for _, receipt := range history.Returns {
		if receipt.SourceExchangeID != nil && receipt.Exchange == nil && receipt.Refund == nil {
			returnIDs = append(returnIDs, receipt.ID)
		}
	}
	if len(returnIDs) == 0 {
		return nil
	}
	sources, err := exchangeRefundSources(ctx, tx, tenantID, storeID, saleID, returnIDs)
	if err != nil {
		return err
	}
	byReturn := make(map[string][]exchangeRefundSource)
	for _, source := range sources {
		byReturn[source.ReturnID] = append(byReturn[source.ReturnID], source)
	}
	for index := range history.Returns {
		receipt := &history.Returns[index]
		if receipt.SourceExchangeID != nil && receipt.Exchange == nil && receipt.Refund == nil {
			receipt.ExchangeRefundOptions = combineRefundOptions(history.RefundOptions, byReturn[receipt.ID])
		}
	}
	return nil
}
