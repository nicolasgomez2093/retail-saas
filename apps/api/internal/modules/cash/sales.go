package cash

import (
	"context"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

func (s Service) Checkout(ctx context.Context, actor auth.Identity, storeID, sessionID string, operation func(pgx.Tx, Session, string, bool) error) error {
	if !validID(sessionID) {
		return ErrInvalid
	}
	return s.withStore(ctx, actor, storeID, true, func(tx pgx.Tx, scope access) error {
		session, err := readSession(ctx, tx, actor, storeID, sessionID, scope, true)
		if err != nil {
			return err
		}
		return operation(tx, session, scope.currency, scope.supervise)
	})
}

func (s Service) ReadStoreSales(ctx context.Context, actor auth.Identity, storeID string, read func(pgx.Tx, bool) error) error {
	return s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error { return read(tx, scope.supervise) })
}

func PostSaleCash(ctx context.Context, tx pgx.Tx, actor auth.Identity, session Session, saleID string, amount int64) error {
	if err := validateCashCredit(session, amount); err != nil {
		return err
	}
	if amount == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,sale_id)
		VALUES ($1,$2,$3,'sale_cash',$4::text::numeric,'Cobro de venta en efectivo',$5)`,
		actor.TenantID, session.ID, actor.UserID, money.Format(amount), saleID)
	return err
}

func PostReceivableCash(ctx context.Context, tx pgx.Tx, actor auth.Identity, session Session, saleID, paymentID string, amount int64) error {
	if err := validateCashCredit(session, amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,sale_id,receivable_payment_id)
		VALUES ($1,$2,$3,'receivable_cash',$4::text::numeric,'Abono de fiado en efectivo',$5,$6)`,
		actor.TenantID, session.ID, actor.UserID, money.Format(amount), saleID, paymentID)
	return err
}

func validateCashCredit(session Session, amount int64) error {
	if session.Status != "OPEN" {
		return ErrSessionState
	}
	expected, valid := money.Parse(session.ExpectedCash)
	if !valid || amount < 0 || amount > money.MaxCents-expected {
		return ErrCashBalance
	}
	return nil
}

func (s Service) WriteStoreSales(ctx context.Context, actor auth.Identity, storeID string, write func(pgx.Tx, bool) error) error {
	return s.withStore(ctx, actor, storeID, true, func(tx pgx.Tx, scope access) error { return write(tx, scope.supervise) })
}

func PostReservationDeposit(ctx context.Context, tx pgx.Tx, actor auth.Identity, session Session, depositID string, amount int64) error {
	if err := validateCashCredit(session, amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements
		(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,reservation_deposit_id)
		VALUES ($1,$2,$3,'reservation_deposit_cash',$4::text::numeric,'Seña de reserva en efectivo',$5)`,
		actor.TenantID, session.ID, actor.UserID, money.Format(amount), depositID)
	return err
}

func PostReturnRefund(ctx context.Context, tx pgx.Tx, actor auth.Identity, session Session, saleID, refundID string, amount int64) error {
	if session.Status != "OPEN" {
		return ErrSessionState
	}
	expected, valid := money.Parse(session.ExpectedCash)
	if !valid || amount <= 0 || amount > expected {
		return ErrCashBalance
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements
		(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,sale_id,return_refund_id)
		VALUES ($1,$2,$3,'sale_refund_cash',$4::text::numeric,'Reintegro de devolución en efectivo',$5,$6)`,
		actor.TenantID, session.ID, actor.UserID, money.Format(-amount), saleID, refundID)
	return err
}

func PostExchangeCash(ctx context.Context, tx pgx.Tx, actor auth.Identity, session Session, saleID, exchangeID string, amount int64) error {
	if err := validateCashCredit(session, amount); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO cash_movements
		(tenant_id,cash_session_id,actor_id,movement_type,amount,reason,sale_id,exchange_id)
		VALUES ($1,$2,$3,'sale_exchange_cash',$4::text::numeric,'Diferencia de cambio en efectivo',$5,$6)`,
		actor.TenantID, session.ID, actor.UserID, money.Format(amount), saleID, exchangeID)
	return err
}
