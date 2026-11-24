package reservations

import (
	"context"
	"errors"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrDepositInvalid = errors.New("verificá la caja, el medio y el importe de la seña")
var ErrDepositExists = errors.New("la reserva ya tiene una seña registrada")
var ErrDepositChanged = errors.New("cambió la seña. Quitá la reserva del carrito y volvé a cargarla")
var ErrDepositExceedsTotal = errors.New("la seña supera el total vigente de la venta")

type DepositInput struct {
	StoreID   string `json:"storeId"`
	SessionID string `json:"cashSessionId"`
	Method    string `json:"method"`
	Amount    string `json:"amount"`
}

type Deposit struct {
	ID        string    `json:"id"`
	SessionID string    `json:"cashSessionId"`
	Method    string    `json:"method"`
	Amount    string    `json:"amount"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

const depositFields = "id,cash_session_id,method,amount::text,created_by,created_at"

func (s Service) AddDeposit(ctx context.Context, actor auth.Identity, id, key string, input DepositInput) (Deposit, error) {
	amount, valid := money.Parse(input.Amount)
	if !valid || amount <= 0 || !validID(id) || !keyPattern.MatchString(key) {
		return Deposit{}, ErrDepositInvalid
	}
	switch input.Method {
	case "cash", "transfer", "debit", "credit", "qr":
	default:
		return Deposit{}, ErrDepositInvalid
	}
	input.Amount = money.Format(amount)
	var result Deposit
	err := s.cash.Checkout(ctx, actor, input.StoreID, input.SessionID, func(tx pgx.Tx, session cash.Session, _ string, _ bool) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:reservation-deposit:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		state, elapsed, err := lock(ctx, tx, actor.TenantID, input.StoreID, id)
		if err != nil {
			return err
		}
		var previousID string
		err = tx.QueryRow(ctx, "SELECT reservation_id,"+depositFields+" FROM reservation_deposits WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key).Scan(&previousID, &result.ID, &result.SessionID, &result.Method, &result.Amount, &result.CreatedBy, &result.CreatedAt)
		if err == nil {
			if previousID != id || result.SessionID != session.ID || result.Method != input.Method ||
				result.Amount != input.Amount || result.CreatedBy != actor.UserID {
				return cash.ErrIdempotency
			}
			result.CreatedAt = result.CreatedAt.UTC()
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if state != "ACTIVE" || elapsed {
			return ErrState
		}
		if session.Status != "OPEN" {
			return cash.ErrSessionState
		}
		err = tx.QueryRow(ctx, `INSERT INTO reservation_deposits(tenant_id,store_id,reservation_id,cash_session_id,method,amount,created_by,idempotency_key)
			VALUES ($1,$2,$3,$4,$5,$6::text::numeric,$7,$8) ON CONFLICT (tenant_id,store_id,reservation_id) DO NOTHING
			RETURNING `+depositFields, actor.TenantID, input.StoreID, id, session.ID, input.Method, input.Amount, actor.UserID, key).
			Scan(&result.ID, &result.SessionID, &result.Method, &result.Amount, &result.CreatedBy, &result.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDepositExists
		}
		if err != nil {
			return err
		}
		result.CreatedAt = result.CreatedAt.UTC()
		if input.Method == "cash" {
			return cash.PostReservationDeposit(ctx, tx, actor, session, result.ID, amount)
		}
		return nil
	})
	return result, err
}
