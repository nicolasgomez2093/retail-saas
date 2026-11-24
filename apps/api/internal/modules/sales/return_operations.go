package sales

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/modules/pricing"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

// ErrOperationUnsettled rejects a return whose value is not fully resolved:
// every peso goes to a replacement, the fiado balance or a refund.
var ErrOperationUnsettled = errors.New("resolvé todo el importe de la devolución: cambio, descuento del fiado o reintegro")

// ReturnOperationExchange lists the garments the customer takes instead.
type ReturnOperationExchange struct {
	LocationID string      `json:"locationId"`
	Items      []ItemInput `json:"items"`
}

// ReturnOperationInput is one complete exchange or return: the goods that come
// back, what the customer takes instead and how the difference is settled.
type ReturnOperationInput struct {
	StoreID          string                   `json:"storeId"`
	Reason           string                   `json:"reason"`
	Items            []ReturnQuantity         `json:"items"`
	Exchange         *ReturnOperationExchange `json:"exchange,omitempty"`
	Charge           *ExchangeChargeInput     `json:"charge,omitempty"`
	Refund           *ExchangeRefundInput     `json:"refund,omitempty"`
	DebtCreditAmount *string                  `json:"debtCreditAmount,omitempty"`
}

// ReturnPreview is the server's calculation shown before confirming.
type ReturnPreview struct {
	Currency         string    `json:"currency"`
	ReturnAmount     string    `json:"returnAmount"`
	ReplacementTotal string    `json:"replacementTotal"`
	Replacements     []Item    `json:"replacements"`
	Difference       string    `json:"difference"`
	Charge           string    `json:"charge"`
	DebtCredit       string    `json:"debtCredit"`
	Refund           string    `json:"refund"`
	OutstandingDebt  *string   `json:"outstandingDebt"`
	RefundOptions    []Payment `json:"refundOptions"`
}

type normalizedOperation struct {
	ret            ReturnInput
	exchange       *ReturnExchangeInput
	refund         *ReturnRefundInput
	credit         int64
	chargePayments []PaymentInput
	digest         []byte
}

func normalizeOperation(saleID, key string, input ReturnOperationInput) (normalizedOperation, error) {
	op := normalizedOperation{ret: ReturnInput{StoreID: input.StoreID, Reason: input.Reason, Items: input.Items}}
	if err := normalizeReturnInput(saleID, key, &op.ret); err != nil {
		return op, err
	}
	input.Reason, input.Items = op.ret.Reason, op.ret.Items
	if input.Exchange != nil {
		exchange := ReturnExchangeInput{DebtCreditAmount: input.DebtCreditAmount, StoreID: input.StoreID, LocationID: input.Exchange.LocationID,
			Items: input.Exchange.Items, Charge: input.Charge, Refund: input.Refund}
		if !validID(exchange.LocationID) || len(exchange.Items) == 0 || len(exchange.Items) > 100 {
			return op, ErrExchangeInvalid
		}
		credit, payments, err := normalizeExchangeInput(&exchange)
		if err != nil {
			return op, err
		}
		op.exchange, op.credit, op.chargePayments = &exchange, credit, payments
		input.Exchange.Items, input.Charge, input.Refund, input.DebtCreditAmount = exchange.Items, exchange.Charge, exchange.Refund, exchange.DebtCreditAmount
	} else {
		if input.Charge != nil {
			return op, ErrExchangeDifference
		}
		if input.DebtCreditAmount != nil {
			credit, valid := money.Parse(*input.DebtCreditAmount)
			if !valid || credit == 0 {
				return op, ErrDebtCreditInvalid
			}
			canonical := money.Format(credit)
			input.DebtCreditAmount, op.credit = &canonical, credit
		}
		if input.Refund != nil {
			refund := ReturnRefundInput{StoreID: input.StoreID, SessionID: input.Refund.SessionID, Payments: input.Refund.Payments}
			if !validID(refund.SessionID) || len(refund.Payments) == 0 || normalizeRefundInput(&refund) != nil {
				return op, ErrRefundInvalid
			}
			input.Refund = &ExchangeRefundInput{SessionID: refund.SessionID, Payments: refund.Payments}
			op.refund = &refund
		}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return op, err
	}
	digest := sha256.Sum256(body)
	op.digest = digest[:]
	return op, nil
}

// AddReturnOperation records the return, the replacement, the stock movements,
// the charge or refund and the fiado credit in a single transaction: either
// everything is confirmed or nothing is. A retry with the same key returns the
// stored operation without moving stock or money again.
func (s Service) AddReturnOperation(ctx context.Context, actor auth.Identity, saleID, key string, input ReturnOperationInput) (SaleReturn, error) {
	op, err := normalizeOperation(saleID, key, input)
	if err != nil {
		return SaleReturn{}, err
	}
	var result SaleReturn
	operation := func(tx pgx.Tx, session *cash.Session, supervise bool) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:return-operation:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var history ReturnHistory
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, true, &history); err != nil {
			return err
		}
		returnID, replay, err := postReturn(ctx, tx, actor, saleID, key, op.ret, op.digest)
		if err != nil {
			return err
		}
		if !replay {
			if err := settleOperation(ctx, tx, actor, session, supervise, saleID, returnID, key, input.StoreID, op); err != nil {
				return err
			}
		}
		result, err = scanSaleReturn(tx.QueryRow(ctx, returnSelect+" AND r.id=$4", actor.TenantID, input.StoreID, saleID, returnID))
		return err
	}
	sessionID := ""
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

// The colon keeps these derived keys outside the public key namespace.
func settleOperation(ctx context.Context, tx pgx.Tx, actor auth.Identity, session *cash.Session, supervise bool,
	saleID, returnID, key, storeID string, op normalizedOperation) error {
	if op.exchange != nil {
		_, err := postExchange(ctx, tx, actor, session, supervise, saleID, returnID, "return-operation-exchange:"+key,
			*op.exchange, op.digest, op.credit, op.chargePayments)
		return err
	}
	var referenceText string
	if err := tx.QueryRow(ctx, "SELECT reference_amount::text FROM sale_returns WHERE tenant_id=$1 AND id=$2",
		actor.TenantID, returnID).Scan(&referenceText); err != nil {
		return err
	}
	reference, _ := money.Parse(referenceText)
	if op.credit > reference {
		return ErrDebtCreditAmount
	}
	if op.credit > 0 {
		if _, err := postDebtCreditForReturn(ctx, tx, actor, supervise, saleID, returnID, "return-operation-credit:"+key,
			ReturnDebtCreditInput{StoreID: storeID, Amount: money.Format(op.credit)}, op.credit); err != nil {
			return err
		}
	}
	remaining := reference - op.credit
	switch {
	case remaining > 0 && op.refund == nil:
		return ErrOperationUnsettled
	case remaining == 0 && op.refund != nil:
		return ErrExchangeDifference
	case remaining > 0:
		if session == nil {
			return ErrRefundInvalid
		}
		_, err := postRefundForReturn(ctx, tx, actor, *session, supervise, saleID, returnID, "return-operation-refund:"+key, *op.refund)
		return err
	}
	return nil
}

// PreviewReturnOperation computes the value of the returned units at their
// historical price, the replacement at the current price and how the
// difference would be settled. It writes nothing.
func (s Service) PreviewReturnOperation(ctx context.Context, actor auth.Identity, saleID string, input ReturnOperationInput) (ReturnPreview, error) {
	ret := ReturnInput{StoreID: input.StoreID, Reason: "vista previa", Items: input.Items}
	if err := normalizeReturnInput(saleID, "preview", &ret); err != nil {
		return ReturnPreview{}, err
	}
	var exchangeItems []ItemInput
	if input.Exchange != nil {
		if len(input.Exchange.Items) == 0 || len(input.Exchange.Items) > 100 {
			return ReturnPreview{}, ErrExchangeInvalid
		}
		for _, item := range input.Exchange.Items {
			if !validID(item.VariantID) || item.Quantity <= 0 || item.DiscountPercent != 0 {
				return ReturnPreview{}, ErrExchangeInvalid
			}
		}
		exchangeItems = input.Exchange.Items
	}
	var preview ReturnPreview
	err := s.cash.ReadStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, supervise bool) error {
		var history ReturnHistory
		if err := readReturnSale(ctx, tx, actor, input.StoreID, saleID, supervise, false, &history); err != nil {
			return err
		}
		preview.Currency, preview.OutstandingDebt = history.Currency, history.OutstandingDebt
		sold, err := returnableItems(ctx, tx, actor.TenantID, input.StoreID, saleID, nil)
		if err != nil {
			return err
		}
		left := make(map[string]ReturnableItem, len(sold))
		for _, line := range sold {
			left[line.VariantID] = line
		}
		var reference int64
		for _, line := range ret.Items {
			item, ok := left[line.VariantID]
			if !ok || line.Quantity > item.Sold-item.Returned {
				return ErrReturnQuantity
			}
			item.Returned += line.Quantity
			left[line.VariantID] = item
			unit, _ := money.Parse(item.UnitPrice)
			reference += unit * int64(line.Quantity)
		}
		var total int64
		if len(exchangeItems) > 0 {
			priced := make([]ItemInput, len(exchangeItems))
			for index, item := range exchangeItems {
				price, err := pricing.ResolvePrice(ctx, tx, actor, input.StoreID, item.VariantID)
				if errors.Is(err, pricing.ErrNotFound) {
					return ErrNotFound
				}
				if err != nil {
					return err
				}
				if price.Amount == nil {
					return ErrNoPrice
				}
				item.ExpectedPrice = *price.Amount
				priced[index] = item
			}
			preview.Replacements, total, err = quote(ctx, tx, actor, Input{StoreID: input.StoreID, Items: priced})
			if err != nil {
				return err
			}
		}
		difference := total - reference
		var balance, charge, credit, refund int64
		if history.OutstandingDebt != nil {
			balance, _ = money.Parse(*history.OutstandingDebt)
		}
		if difference > 0 {
			charge = difference
		} else {
			credit = min(balance, -difference)
			refund = -difference - credit
		}
		preview.ReturnAmount, preview.ReplacementTotal, preview.Difference = money.Format(reference), money.Format(total), signed(difference)
		preview.Charge, preview.DebtCredit, preview.Refund = money.Format(charge), money.Format(credit), money.Format(refund)
		preview.RefundOptions, err = refundOptions(ctx, tx, actor.TenantID, input.StoreID, saleID)
		return err
	})
	return preview, err
}

func signed(cents int64) string {
	if cents < 0 {
		return "-" + money.Format(-cents)
	}
	return money.Format(cents)
}
