package cash

import (
	"context"
	"errors"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

type MovementInput struct {
	StoreID string `json:"storeId"`
	Type    string `json:"type"`
	Amount  string `json:"amount"`
	Reason  string `json:"reason"`
}

type Movement struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	ActorID   string    `json:"actorId"`
	Type      string    `json:"type"`
	Amount    string    `json:"amount"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
	SaleID    *string   `json:"saleId"`
}

type MovementPage struct {
	Items   []Movement `json:"items"`
	Income  string     `json:"income"`
	Outflow string     `json:"outflow"`
	Count   int64      `json:"count"`
}

const movementFields = "id,cash_session_id,actor_id,movement_type,amount::text,reason,created_at,sale_id"

func (s Service) Movements(ctx context.Context, actor auth.Identity, storeID, id string) (MovementPage, error) {
	if !validID(id) {
		return MovementPage{}, ErrInvalid
	}
	var result MovementPage
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error {
		if _, err := readSession(ctx, tx, actor, storeID, id, scope, false); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(amount) FILTER (WHERE amount>0),0.00)::text,
			coalesce(sum(-amount) FILTER (WHERE amount<0),0.00)::text,count(*)
			FROM cash_movements WHERE tenant_id=$1 AND cash_session_id=$2`, actor.TenantID, id).
			Scan(&result.Income, &result.Outflow, &result.Count); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+movementFields+` FROM cash_movements WHERE tenant_id=$1 AND cash_session_id=$2
			ORDER BY created_at DESC,id DESC LIMIT 100`, actor.TenantID, id)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Movement, error) { return scanMovement(row) })
		return err
	})
	return result, err
}

func (s Service) AddMovement(ctx context.Context, actor auth.Identity, id, key string, input MovementInput) (Movement, error) {
	cents, valid := money.Parse(input.Amount)
	input.Reason = strings.TrimSpace(input.Reason)
	if !valid || cents == 0 || !validID(id) || !keyPattern.MatchString(key) || !validText(input.Reason, 300) ||
		(input.Type != "manual_in" && input.Type != "manual_out") {
		return Movement{}, ErrInvalid
	}
	if input.Type == "manual_out" {
		cents = -cents
	}
	amount := money.Format(cents)
	var result Movement
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, scope access) error {
		session, err := readSession(ctx, tx, actor, input.StoreID, id, scope, true)
		if err != nil {
			return err
		}
		prior, err := scanMovement(tx.QueryRow(ctx, "SELECT "+movementFields+" FROM cash_movements WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key))
		if err == nil {
			if prior.SessionID != id || prior.ActorID != actor.UserID || prior.Type != input.Type ||
				prior.Amount != amount || prior.Reason != input.Reason {
				return ErrIdempotency
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if session.Status != "OPEN" {
			return ErrSessionState
		}
		expected, valid := money.Parse(session.ExpectedCash)
		if !valid || expected+cents < 0 || expected+cents > money.MaxCents {
			return ErrCashBalance
		}
		result, err = scanMovement(tx.QueryRow(ctx, `INSERT INTO cash_movements(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,idempotency_key)
			VALUES ($1,$2,$3,$4,$5::text::numeric,$6,$7) RETURNING `+movementFields,
			actor.TenantID, id, actor.UserID, input.Type, amount, input.Reason, key))
		return err
	})
	return result, err
}

func scanMovement(row pgx.Row) (Movement, error) {
	var movement Movement
	err := row.Scan(&movement.ID, &movement.SessionID, &movement.ActorID, &movement.Type, &movement.Amount, &movement.Reason, &movement.CreatedAt, &movement.SaleID)
	movement.CreatedAt = movement.CreatedAt.UTC()
	return movement, err
}
