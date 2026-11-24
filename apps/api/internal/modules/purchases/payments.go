package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrPaymentExceeds = errors.New("el pago supera el saldo del pedido")

// Payment is money handed to the supplier for one order. Payments are never
// deleted: a mistaken one is voided with a reason and stops counting.
type Payment struct {
	ID         string     `json:"id"`
	Method     string     `json:"method"`
	Amount     string     `json:"amount"`
	PaidOn     string     `json:"paidOn"`
	CheckDueOn *string    `json:"checkDueOn"`
	Reference  string     `json:"reference"`
	Note       string     `json:"note"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	VoidedAt   *time.Time `json:"voidedAt"`
	VoidReason string     `json:"voidReason"`
}

type PaymentInput struct {
	StoreID string `json:"storeId"`
	Method  string `json:"method"`
	Amount  string `json:"amount"`
	// PaidOn is the day the money or the check was handed over, in the
	// commerce timezone. It cannot be in the future.
	PaidOn string `json:"paidOn"`
	// CheckDueOn is when a check can be cashed; only for checks.
	CheckDueOn string `json:"checkDueOn,omitempty"`
	Reference  string `json:"reference,omitempty"`
	Note       string `json:"note,omitempty"`
}

type VoidPaymentInput struct {
	StoreID string `json:"storeId"`
	Reason  string `json:"reason"`
}

// paymentState derives the balance and a status the screens can show without
// doing decimal arithmetic. A negative balance is a credit with the supplier
// (for example, an order cancelled after being paid in full).
func paymentState(payable *string, paid string) (*string, string) {
	paidCents, _ := money.Parse(paid)
	if payable == nil {
		return nil, "unknown"
	}
	payableCents, _ := money.Parse(*payable)
	balance := money.Format(payableCents - paidCents)
	switch {
	case payableCents == 0 && paidCents == 0:
		return &balance, "none"
	case paidCents == 0:
		return &balance, "unpaid"
	case paidCents < payableCents:
		return &balance, "partial"
	case paidCents == payableCents:
		return &balance, "paid"
	default:
		return &balance, "credit"
	}
}

func validDate(value string) bool {
	day, err := time.Parse(time.DateOnly, value)
	return err == nil && day.Year() >= 2000 && day.Year() <= 9999 && day.Format(time.DateOnly) == value
}

func (s Service) RegisterPayment(ctx context.Context, actor auth.Identity, orderID, key string, input PaymentInput) (Order, error) {
	input.Reference = strings.TrimSpace(input.Reference)
	input.Note = strings.TrimSpace(input.Note)
	cents, valid := money.Parse(input.Amount)
	switch input.Method {
	case "cash", "transfer", "check", "card", "other":
	default:
		valid = false
	}
	if !valid || cents <= 0 || !validID(orderID) || !validID(input.StoreID) || !keyPattern.MatchString(key) || !validDate(input.PaidOn) ||
		(input.CheckDueOn != "" && (input.Method != "check" || !validDate(input.CheckDueOn))) ||
		!validText(input.Reference, 80) || !validText(input.Note, 300) {
		return Order{}, ErrInvalid
	}
	input.Amount = money.Format(cents)
	body, err := json.Marshal(struct {
		OrderID string
		PaymentInput
	}{orderID, input})
	if err != nil {
		return Order{}, err
	}
	digest := sha256.Sum256(body)
	var result Order
	err = s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, access orderAccess) error {
		var priorOrder, creator string
		var priorHash []byte
		err := tx.QueryRow(ctx, `SELECT purchase_order_id,created_by,request_hash FROM purchase_order_payments
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&priorOrder, &creator, &priorHash)
		if err == nil {
			if priorOrder != orderID || creator != actor.UserID || !bytes.Equal(priorHash, digest[:]) {
				return ErrKey
			}
			result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		order, err := scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var future bool
		if err := tx.QueryRow(ctx, `SELECT $1::date > (clock_timestamp() AT TIME ZONE $2)::date`, input.PaidOn, access.timezone).Scan(&future); err != nil {
			return err
		}
		if future {
			return ErrInvalid
		}
		// The tenant lock taken by withStore serializes payments, so the
		// balance read here cannot change before the insert.
		if order.Balance != nil {
			balance, _ := money.Parse(strings.TrimPrefix(*order.Balance, "-"))
			if strings.HasPrefix(*order.Balance, "-") || cents > balance {
				return ErrPaymentExceeds
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_order_payments(tenant_id,store_id,purchase_order_id,method,amount,paid_on,check_due_on,
			reference,note,created_by,idempotency_key,request_hash)
			VALUES ($1,$2,$3,$4,$5::text::numeric,$6::date,NULLIF($7,'')::date,$8,$9,$10,$11,$12)`,
			actor.TenantID, input.StoreID, orderID, input.Method, input.Amount, input.PaidOn, input.CheckDueOn,
			input.Reference, input.Note, actor.UserID, key, digest[:]); err != nil {
			return err
		}
		result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		return err
	})
	return result, err
}

// VoidPayment cancels a payment recorded by mistake. Voiding twice returns
// the order unchanged.
func (s Service) VoidPayment(ctx context.Context, actor auth.Identity, orderID, paymentID string, input VoidPaymentInput) (Order, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validID(orderID) || !validID(paymentID) || !validID(input.StoreID) || input.Reason == "" || !validText(input.Reason, 300) {
		return Order{}, ErrInvalid
	}
	var result Order
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, _ orderAccess) error {
		var voided bool
		err := tx.QueryRow(ctx, `SELECT voided_at IS NOT NULL FROM purchase_order_payments
			WHERE tenant_id=$1 AND store_id=$2 AND purchase_order_id=$3 AND id=$4 FOR UPDATE`,
			actor.TenantID, input.StoreID, orderID, paymentID).Scan(&voided)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !voided {
			if _, err := tx.Exec(ctx, `UPDATE purchase_order_payments SET voided_at=clock_timestamp(),voided_by=$3,void_reason=$4
				WHERE tenant_id=$1 AND id=$2`, actor.TenantID, paymentID, actor.UserID, input.Reason); err != nil {
				return err
			}
		}
		result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		return err
	})
	return result, err
}
