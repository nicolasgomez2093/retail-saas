package sales

import (
	"cmp"
	"errors"
	"slices"

	"retail-platform/api/internal/platform/money"
)

var ErrPaymentTotal = errors.New("la suma de los pagos debe coincidir con el total de la venta")

type Payment struct {
	Method string `json:"method"`
	Amount string `json:"amount"`
}

type PaymentInput struct {
	Payment
	CashReceived *string `json:"cashReceived,omitempty"`
}

type appliedPayment struct {
	PaymentInput
	change *string
}

func normalizePaymentInput(input *Input) error {
	if input.CashReceived != nil {
		if input.OnCredit || input.Payments != nil || !normalizeAmount(input.CashReceived) {
			return ErrInvalid
		}
		return nil
	}
	if (input.OnCredit || input.ReservationID != "") && len(input.Payments) == 0 {
		input.Payments = nil
		return nil
	}
	if len(input.Payments) < 1 || len(input.Payments) > 5 {
		return ErrInvalid
	}
	input.Payments = slices.Clone(input.Payments)
	slices.SortFunc(input.Payments, func(a, b PaymentInput) int { return cmp.Compare(a.Method, b.Method) })
	for i := range input.Payments {
		payment := &input.Payments[i]
		if i > 0 && input.Payments[i-1].Method == payment.Method {
			return ErrInvalid
		}
		if !normalizeAmount(&payment.Amount) || payment.Amount == "0.00" {
			return ErrInvalid
		}
		switch payment.Method {
		case "cash":
			if payment.CashReceived == nil {
				return ErrInvalid
			}
			tender := *payment.CashReceived
			if !normalizeAmount(&tender) {
				return ErrInvalid
			}
			payment.CashReceived = &tender
		case "transfer", "debit", "credit", "qr":
			if payment.CashReceived != nil {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func normalizeAmount(value *string) bool {
	cents, ok := money.Parse(*value)
	if ok {
		*value = money.Format(cents)
	}
	return ok
}

func applyPayments(input Input, total int64) ([]appliedPayment, int64, error) {
	payments := input.Payments
	if input.CashReceived != nil {
		payments = []PaymentInput{{Payment: Payment{Method: "cash", Amount: money.Format(total)}, CashReceived: input.CashReceived}}
	}
	applied := make([]appliedPayment, 0, len(payments))
	remaining := total
	for _, payment := range payments {
		amount, _ := money.Parse(payment.Amount)
		if amount > remaining {
			return nil, 0, ErrPaymentTotal
		}
		remaining -= amount
		line := appliedPayment{PaymentInput: payment}
		if payment.CashReceived != nil {
			tender, _ := money.Parse(*payment.CashReceived)
			if tender < amount {
				return nil, 0, ErrTender
			}
			change := money.Format(tender - amount)
			line.change = &change
		}
		applied = append(applied, line)
	}
	if input.OnCredit {
		if remaining <= 0 {
			return nil, 0, ErrCreditBalance
		}
		return applied, remaining, nil
	}
	if remaining != 0 {
		return nil, 0, ErrPaymentTotal
	}
	return applied, 0, nil
}
