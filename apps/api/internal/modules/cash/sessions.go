package cash

import (
	"context"
	"errors"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Session struct {
	ID           string     `json:"id"`
	RegisterID   string     `json:"registerId"`
	OpenedBy     string     `json:"openedBy"`
	OpenedAt     time.Time  `json:"openedAt"`
	OpeningCash  string     `json:"openingCash"`
	Status       string     `json:"status"`
	ExpectedCash string     `json:"expectedCash"`
	DeclaredCash *string    `json:"declaredCash"`
	Difference   *string    `json:"difference"`
	ClosedBy     *string    `json:"closedBy"`
	ClosedAt     *time.Time `json:"closedAt"`
}

type OpenInput struct {
	StoreID     string `json:"storeId"`
	RegisterID  string `json:"registerId"`
	OpeningCash string `json:"openingCash"`
}

type CloseInput struct {
	StoreID      string `json:"storeId"`
	DeclaredCash string `json:"declaredCash"`
}

const sessionSelect = `SELECT cs.id,cs.cash_register_id,cs.opened_by,cs.opened_at,cs.opening_cash::text,cs.status,
	coalesce(cs.expected_cash,cs.opening_cash+coalesce((SELECT sum(m.amount) FROM cash_movements m
		WHERE (m.tenant_id,m.cash_session_id)=(cs.tenant_id,cs.id)),0))::text,
	cs.declared_cash::text,cs.difference::text,cs.closed_by,cs.closed_at
	FROM cash_sessions cs JOIN cash_registers r ON (r.tenant_id,r.id)=(cs.tenant_id,cs.cash_register_id)
	WHERE cs.tenant_id=$1 AND r.store_id=$2 AND r.status='active' AND ($4 OR cs.opened_by=$3)`

func (s Service) Sessions(ctx context.Context, actor auth.Identity, storeID, registerID string) ([]Session, error) {
	if !validID(registerID) {
		return nil, ErrInvalid
	}
	var result []Session
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error {
		if err := requireRegister(ctx, tx, actor.TenantID, storeID, registerID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sessionSelect+` AND r.id=$5 ORDER BY cs.opened_at DESC,cs.id DESC LIMIT 50`,
			actor.TenantID, storeID, actor.UserID, scope.supervise, registerID)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Session, error) { return scanSession(row) })
		return err
	})
	return result, err
}

func (s Service) Session(ctx context.Context, actor auth.Identity, storeID, id string) (Session, error) {
	if !validID(id) {
		return Session{}, ErrInvalid
	}
	var result Session
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error {
		var err error
		result, err = readSession(ctx, tx, actor, storeID, id, scope, false)
		return err
	})
	return result, err
}

func (s Service) Open(ctx context.Context, actor auth.Identity, key string, input OpenInput) (Session, error) {
	cents, valid := money.Parse(input.OpeningCash)
	if !valid || !validID(input.RegisterID) || !keyPattern.MatchString(key) {
		return Session{}, ErrInvalid
	}
	opening := money.Format(cents)
	var result Session
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, scope access) error {
		if err := requireRegister(ctx, tx, actor.TenantID, input.StoreID, input.RegisterID); err != nil {
			return err
		}
		var id, register, opener, priorAmount string
		err := tx.QueryRow(ctx, `SELECT id,cash_register_id,opened_by,opening_cash::text FROM cash_sessions
			WHERE tenant_id=$1 AND open_key=$2`, actor.TenantID, key).Scan(&id, &register, &opener, &priorAmount)
		if err == nil {
			if register != input.RegisterID || opener != actor.UserID || priorAmount != opening {
				return ErrIdempotency
			}
			result, err = readSession(ctx, tx, actor, input.StoreID, id, scope, false)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO cash_sessions(tenant_id,cash_register_id,opened_by,opening_cash,open_key)
			VALUES ($1,$2,$3,$4::text::numeric,$5) RETURNING id`, actor.TenantID, input.RegisterID, actor.UserID, opening, key).Scan(&id)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == "cash_sessions_one_open" {
			return ErrOccupied
		}
		if err != nil {
			return err
		}
		result, err = readSession(ctx, tx, actor, input.StoreID, id, scope, false)
		return err
	})
	return result, err
}

func (s Service) Close(ctx context.Context, actor auth.Identity, id string, input CloseInput) (Session, error) {
	cents, valid := money.Parse(input.DeclaredCash)
	if !valid || !validID(id) {
		return Session{}, ErrInvalid
	}
	declared := money.Format(cents)
	var result Session
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, scope access) error {
		current, err := readSession(ctx, tx, actor, input.StoreID, id, scope, true)
		if err != nil {
			return err
		}
		if current.Status == "CLOSED" {
			if current.DeclaredCash == nil || *current.DeclaredCash != declared {
				return ErrSessionState
			}
			result = current
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE cash_sessions SET status='CLOSED',closed_by=$3,closed_at=clock_timestamp(),
			expected_cash=$4::text::numeric,declared_cash=$5::text::numeric,
			difference=$5::text::numeric-$4::text::numeric WHERE tenant_id=$1 AND id=$2`,
			actor.TenantID, id, actor.UserID, current.ExpectedCash, declared); err != nil {
			return err
		}
		result, err = readSession(ctx, tx, actor, input.StoreID, id, scope, false)
		return err
	})
	return result, err
}

func readSession(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, id string, scope access, lock bool) (Session, error) {
	query := sessionSelect + " AND cs.id=$5"
	if lock {
		query += " FOR UPDATE OF cs"
	}
	result, err := scanSession(tx.QueryRow(ctx, query, actor.TenantID, storeID, actor.UserID, scope.supervise, id))
	if err == nil && lock {
		// The first statement may have waited for a posting transaction. Refresh the movement total after locking.
		return readSession(ctx, tx, actor, storeID, id, scope, false)
	}
	return result, notFound(err)
}

func scanSession(row pgx.Row) (Session, error) {
	var session Session
	err := row.Scan(&session.ID, &session.RegisterID, &session.OpenedBy, &session.OpenedAt, &session.OpeningCash, &session.Status,
		&session.ExpectedCash, &session.DeclaredCash, &session.Difference, &session.ClosedBy, &session.ClosedAt)
	session.OpenedAt = session.OpenedAt.UTC()
	if session.ClosedAt != nil {
		*session.ClosedAt = session.ClosedAt.UTC()
	}
	return session, err
}
