package inventory

import (
	"context"
	"errors"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

var ErrCountState = errors.New("el estado o el resultado confirmado no permiten esa operación")
var ErrCountStale = errors.New("el saldo cambió desde la referencia del conteo")

const countFields = "c.id,c.location_id,c.status,c.created_by,c.created_at,c.completed_by,c.completed_at,c.cancelled_at,c.note"

type CountSummary struct {
	ID                         string     `json:"id"`
	LocationID                 string     `json:"locationId"`
	Status                     string     `json:"status"`
	CreatedBy                  string     `json:"createdBy"`
	CreatedAt                  time.Time  `json:"createdAt"`
	CompletedBy                *string    `json:"completedBy"`
	CompletedAt                *time.Time `json:"completedAt"`
	CancelledAt                *time.Time `json:"cancelledAt"`
	Note                       string     `json:"note"`
	Location                   string     `json:"location"`
	LocationActive             bool       `json:"locationActive"`
	Store                      string     `json:"store"`
	Creator                    string     `json:"creator"`
	Closer                     *string    `json:"closer"`
	HistoricalLabels           bool       `json:"historicalLabels"`
	Revision                   int64      `json:"revision"`
	LineCount                  int        `json:"lineCount"`
	CountedCount               int        `json:"countedCount"`
	draftWorkflow              bool
	closeKey, closeFingerprint *string
}

type CountLine struct {
	VariantID        string `json:"variantId"`
	SKU              string `json:"sku"`
	Expected         int32  `json:"expected"`
	Counted          *int32 `json:"counted"`
	Product          string `json:"product"`
	Size             string `json:"size"`
	Color            string `json:"color"`
	Note             string `json:"note"`
	HistoricalLabels bool   `json:"historicalLabels"`
	ExpectedRecorded bool   `json:"expectedRecorded"`
	revision         *int64
}

type Count struct {
	CountSummary
	Items     []CountLine     `json:"items"`
	Movements []CountMovement `json:"movements"`
}

type NewCount struct {
	StoreID    string   `json:"storeId"`
	LocationID string   `json:"locationId"`
	VariantIDs []string `json:"variantIds"`
	Draft      bool     `json:"draft,omitempty"`
	Note       string   `json:"note,omitempty"`
}

type CountCompletion struct {
	StoreID string            `json:"storeId"`
	Items   []EnteredQuantity `json:"items"`
	Note    string            `json:"note"`
}

func (s Service) Counts(ctx context.Context, actor auth.Identity, storeID, locationID string) ([]CountSummary, error) {
	if !validID(locationID) {
		return nil, ErrInvalid
	}
	var result []CountSummary
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		if err := requireLocation(ctx, tx, actor.TenantID, storeID, locationID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, countExtendedSQL+` WHERE c.tenant_id=$1 AND c.location_id=$2 ORDER BY c.created_at DESC,c.id DESC LIMIT 50`, actor.TenantID, locationID)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (CountSummary, error) { return scanCountExtended(row) })
		return err
	})
	return result, err
}

func (s Service) Count(ctx context.Context, actor auth.Identity, storeID, id string) (Count, error) {
	if !validID(id) {
		return Count{}, ErrInvalid
	}
	var result Count
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		var err error
		result, err = readCount(ctx, tx, actor.TenantID, storeID, id)
		return err
	})
	return result, err
}

func (s Service) StartCount(ctx context.Context, actor auth.Identity, key string, input NewCount) (Count, error) {
	input.Note = strings.TrimSpace(input.Note)
	if !validID(input.LocationID) || !keyPattern.MatchString(key) ||
		(!input.Draft && (len(input.VariantIDs) == 0 || input.Note != "")) ||
		(input.Draft && len(input.VariantIDs) != 0) || len(input.VariantIDs) > maxInventoryLines ||
		(input.Note != "" && !validText(input.Note, 300)) {
		return Count{}, ErrInvalid
	}
	ids := make(map[string]bool, len(input.VariantIDs))
	for _, id := range input.VariantIDs {
		if !validID(id) || ids[id] {
			return Count{}, ErrInvalid
		}
		ids[id] = true
	}
	var result Count
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		if err := requireLocation(ctx, tx, actor.TenantID, input.StoreID, input.LocationID); err != nil {
			return err
		}
		var id, creator, location string
		err := tx.QueryRow(ctx, "SELECT id,created_by,location_id FROM stock_counts WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key).Scan(&id, &creator, &location)
		if err == nil {
			if creator != actor.UserID || location != input.LocationID {
				return ErrKeyConflict
			}
			prior, err := readCount(ctx, tx, actor.TenantID, input.StoreID, id)
			if err != nil {
				return err
			}
			if prior.draftWorkflow != input.Draft || input.Draft && prior.Note != input.Note {
				return ErrKeyConflict
			}
			if input.Draft {
				result = prior
				return nil
			}
			if len(prior.Items) != len(ids) {
				return ErrKeyConflict
			}
			for _, line := range prior.Items {
				if !ids[line.VariantID] {
					return ErrKeyConflict
				}
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO stock_counts(tenant_id,location_id,idempotency_key,created_by,note,draft_workflow,
			creator_snapshot,location_snapshot,store_snapshot)
			SELECT l.tenant_id,l.id,$3,u.id,$5,$6,u.name,l.name,s.name
			FROM stock_locations l JOIN stores s ON (s.tenant_id,s.id)=(l.tenant_id,l.store_id)
			JOIN users u ON u.tenant_id=l.tenant_id AND u.id=$4
			WHERE l.tenant_id=$1 AND l.id=$2 RETURNING id`, actor.TenantID, input.LocationID, key, actor.UserID, input.Note, input.Draft).Scan(&id)
		if err != nil {
			return err
		}
		if len(input.VariantIDs) > 0 {
			tag, err := tx.Exec(ctx, `INSERT INTO stock_count_items(tenant_id,count_id,variant_id,sku_snapshot,expected_quantity,expected_revision,
				product_snapshot,size_snapshot,color_snapshot)
				SELECT v.tenant_id,$2,v.id,v.sku,coalesce(b.quantity_on_hand,0),b.revision,p.name,v.size,v.color FROM product_variants v
				JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				LEFT JOIN stock_balances b ON (b.tenant_id,b.variant_id)=(v.tenant_id,v.id) AND b.location_id=$3
				WHERE v.tenant_id=$1 AND v.id=ANY($4::text[]::uuid[])`, actor.TenantID, id, input.LocationID, input.VariantIDs)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != int64(len(ids)) {
				return ErrNotFound
			}
		}
		result, err = readCount(ctx, tx, actor.TenantID, input.StoreID, id)
		return err
	})
	return result, err
}

func (s Service) CompleteCount(ctx context.Context, actor auth.Identity, id string, input CountCompletion) (Count, error) {
	items, valid := enteredQuantities(input.Items)
	input.Note = strings.TrimSpace(input.Note)
	if !valid || (input.Note != "" && !validText(input.Note, 300)) {
		return Count{}, ErrInvalid
	}
	return s.changeCount(ctx, actor, input.StoreID, id, func(tx pgx.Tx, current Count) error {
		if current.draftWorkflow || current.Revision > 0 || current.Status != "DRAFT" && current.Status != "COMPLETED" {
			return ErrCountState
		}
		if len(current.Items) != len(items) {
			return ErrInvalid
		}
		for _, line := range current.Items {
			qty, exists := items[line.VariantID]
			if !exists {
				return ErrInvalid
			}
			if current.Status == "COMPLETED" && (line.Counted == nil || *line.Counted != qty || current.Note != input.Note) {
				return ErrCountState
			}
		}
		if current.Status == "COMPLETED" {
			return nil
		}
		for _, line := range current.Items {
			var revision *int64
			err := tx.QueryRow(ctx, `SELECT revision FROM stock_balances WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3
				FOR UPDATE`, actor.TenantID, current.LocationID, line.VariantID).Scan(&revision)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if (line.revision == nil) != (revision == nil) ||
				(line.revision != nil && revision != nil && *line.revision != *revision) {
				return ErrCountStale
			}
		}
		reason := "Conteo físico"
		if input.Note != "" {
			reason = input.Note
		}
		for _, line := range current.Items {
			qty := items[line.VariantID]
			delta := qty - line.Expected
			if delta != 0 {
				if _, err := postMovement(ctx, tx, actor, Movement{LocationID: current.LocationID, VariantID: line.VariantID, SKU: line.SKU,
					Type: "count_adjustment", Delta: delta, Reason: reason, CountID: &id}, nil); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE stock_count_items SET counted_quantity=$4
				WHERE tenant_id=$1 AND count_id=$2 AND variant_id=$3`, actor.TenantID, id, line.VariantID, qty); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE stock_counts SET status='COMPLETED',completed_by=$3,completed_at=clock_timestamp(),note=$4,
			closer_snapshot=(SELECT name FROM users WHERE tenant_id=$1 AND id=$3)
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID, input.Note)
		return err
	})
}

func (s Service) CancelCount(ctx context.Context, actor auth.Identity, storeID, id string) (Count, error) {
	return s.changeCount(ctx, actor, storeID, id, func(tx pgx.Tx, current Count) error {
		if current.Status == "CANCELLED" {
			return nil
		}
		if current.Status != "DRAFT" {
			return ErrCountState
		}
		_, err := tx.Exec(ctx, `UPDATE stock_counts SET status='CANCELLED',cancelled_by=$3,cancelled_at=clock_timestamp()
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID)
		return err
	})
}

func (s Service) changeCount(ctx context.Context, actor auth.Identity, storeID, id string, change func(pgx.Tx, Count) error) (Count, error) {
	if !validID(id) {
		return Count{}, ErrInvalid
	}
	var result Count
	err := s.withStore(ctx, actor, storeID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		current, err := readCount(ctx, tx, actor.TenantID, storeID, id)
		if err != nil {
			return err
		}
		if current.Status == "DRAFT" && !current.LocationActive {
			return ErrNotFound
		}
		if err := change(tx, current); err != nil {
			return err
		}
		result, err = readCount(ctx, tx, actor.TenantID, storeID, id)
		return err
	})
	return result, err
}

func readCount(ctx context.Context, tx pgx.Tx, tenantID, storeID, id string) (Count, error) {
	header, err := scanCountExtended(tx.QueryRow(ctx, countExtendedSQL+
		` WHERE c.tenant_id=$1 AND l.store_id=$2 AND c.id=$3`, tenantID, storeID, id))
	if err != nil {
		return Count{}, mapNotFound(err)
	}
	rows, err := tx.Query(ctx, `SELECT i.variant_id,i.sku_snapshot,i.expected_quantity,i.counted_quantity,i.expected_revision,
		coalesce(i.product_snapshot,p.name),coalesce(i.size_snapshot,v.size),coalesce(i.color_snapshot,v.color),
		i.note,i.product_snapshot IS NOT NULL
		FROM stock_count_items i JOIN product_variants v ON (v.tenant_id,v.id)=(i.tenant_id,i.variant_id)
		JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
		WHERE i.tenant_id=$1 AND i.count_id=$2 ORDER BY i.variant_id`, tenantID, id)
	if err != nil {
		return Count{}, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CountLine, error) {
		var line CountLine
		err := row.Scan(&line.VariantID, &line.SKU, &line.Expected, &line.Counted, &line.revision,
			&line.Product, &line.Size, &line.Color, &line.Note, &line.HistoricalLabels)
		line.ExpectedRecorded = line.revision != nil
		return line, err
	})
	if err != nil {
		return Count{}, err
	}
	result := Count{CountSummary: header, Items: items, Movements: []CountMovement{}}
	if header.Status == "COMPLETED" {
		rows, err := tx.Query(ctx, `SELECT id,variant_id,quantity_delta FROM stock_movements
			WHERE tenant_id=$1 AND count_id=$2 AND location_id=$3 ORDER BY variant_id,id`, tenantID, id, header.LocationID)
		if err != nil {
			return Count{}, err
		}
		result.Movements, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CountMovement])
		if err != nil {
			return Count{}, err
		}
	}
	return result, nil
}
