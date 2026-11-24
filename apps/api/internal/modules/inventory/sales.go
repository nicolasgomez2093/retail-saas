package inventory

import (
	"context"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

func RequireSellableLocation(ctx context.Context, tx pgx.Tx, tenantID, storeID, locationID string) error {
	var id string
	return mapNotFound(tx.QueryRow(ctx, `SELECT id FROM stock_locations
		WHERE tenant_id=$1 AND store_id=$2 AND id=$3 AND status='active' AND is_sellable`, tenantID, storeID, locationID).Scan(&id))
}

func PostSaleMovement(ctx context.Context, tx pgx.Tx, actor auth.Identity, saleID, locationID, variantID, sku string, quantity int32) error {
	if quantity <= 0 {
		return ErrInvalid
	}
	_, err := postMovement(ctx, tx, actor, Movement{LocationID: locationID, VariantID: variantID, SKU: sku, Type: "sale",
		Delta: -quantity, Reason: "Salida por venta", SaleID: &saleID}, nil)
	return err
}

func PostReturnMovement(ctx context.Context, tx pgx.Tx, actor auth.Identity, saleID, returnID, locationID, variantID, sku string, quantity int32) error {
	if quantity <= 0 {
		return ErrInvalid
	}
	_, err := postMovement(ctx, tx, actor, Movement{LocationID: locationID, VariantID: variantID, SKU: sku, Type: "sale_return",
		Delta: quantity, Reason: "Recepción de devolución", SaleID: &saleID, ReturnID: &returnID}, nil)
	return err
}

func PostExchangeMovement(ctx context.Context, tx pgx.Tx, actor auth.Identity, saleID, exchangeID, locationID, variantID, sku string, quantity int32) error {
	if quantity <= 0 {
		return ErrInvalid
	}
	_, err := postMovement(ctx, tx, actor, Movement{LocationID: locationID, VariantID: variantID, SKU: sku, Type: "sale_exchange",
		Delta: -quantity, Reason: "Salida por cambio", SaleID: &saleID, ExchangeID: &exchangeID}, nil)
	return err
}
