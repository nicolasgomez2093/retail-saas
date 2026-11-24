package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

type CountMovement struct {
	ID        string `json:"id"`
	VariantID string `json:"variantId"`
	Delta     int32  `json:"delta"`
}

type DraftLine struct {
	VariantID string `json:"variantId"`
	Counted   *int32 `json:"counted"`
	Note      string `json:"note"`
}

type CountDraft struct {
	StoreID          string      `json:"storeId"`
	ExpectedRevision *int64      `json:"expectedRevision"`
	Items            []DraftLine `json:"items"`
}

type CountReference struct {
	VariantID     string `json:"variantId"`
	StockRevision string `json:"stockRevision"`
}

type CountClose struct {
	StoreID          string           `json:"storeId"`
	ExpectedRevision *int64           `json:"expectedRevision"`
	References       []CountReference `json:"references"`
}

type CountReviewLine struct {
	CountReference
	Current  *int32 `json:"current"`
	Reserved int32  `json:"reserved"`
	Counted  int32  `json:"counted"`
	Delta    int32  `json:"delta"`
	Blocked  bool   `json:"blocked"`
}

type CountReview struct {
	Revision int64             `json:"revision"`
	Items    []CountReviewLine `json:"items"`
}

type CountConflicts struct {
	Code     string
	Variants []string
}

func (e *CountConflicts) Error() string {
	switch e.Code {
	case "count_reserved":
		return "las cantidades contadas no cubren las unidades reservadas"
	case "count_draft_stale":
		return "otra operación actualizó el borrador"
	default:
		return "los saldos de la revisión cambiaron"
	}
}

const countExtendedSQL = "SELECT " + countFields + `,
	coalesce(c.location_snapshot,l.name),l.status='active',coalesce(c.store_snapshot,s.name),
	coalesce(c.creator_snapshot,u.name),coalesce(c.closer_snapshot,closer.name),
	c.creator_snapshot IS NOT NULL AND c.location_snapshot IS NOT NULL AND c.store_snapshot IS NOT NULL,
	c.revision,c.draft_workflow,c.close_key,c.close_fingerprint,
	(SELECT count(*) FROM stock_count_items i WHERE (i.tenant_id,i.count_id)=(c.tenant_id,c.id)),
	(SELECT count(*) FROM stock_count_items i WHERE (i.tenant_id,i.count_id)=(c.tenant_id,c.id) AND i.counted_quantity IS NOT NULL)
	FROM stock_counts c JOIN stock_locations l ON (l.tenant_id,l.id)=(c.tenant_id,c.location_id)
	JOIN stores s ON (s.tenant_id,s.id)=(l.tenant_id,l.store_id)
	JOIN users u ON (u.tenant_id,u.id)=(c.tenant_id,c.created_by)
	LEFT JOIN users closer ON (closer.tenant_id,closer.id)=(c.tenant_id,c.completed_by)`

func scanCountExtended(row pgx.Row) (CountSummary, error) {
	var result CountSummary
	err := row.Scan(&result.ID, &result.LocationID, &result.Status, &result.CreatedBy, &result.CreatedAt,
		&result.CompletedBy, &result.CompletedAt, &result.CancelledAt, &result.Note, &result.Location, &result.LocationActive,
		&result.Store, &result.Creator, &result.Closer, &result.HistoricalLabels, &result.Revision, &result.draftWorkflow,
		&result.closeKey, &result.closeFingerprint, &result.LineCount, &result.CountedCount)
	return result, err
}

func (s Service) SaveCountDraft(ctx context.Context, actor auth.Identity, id string, input CountDraft) (Count, error) {
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || len(input.Items) > maxInventoryLines || input.Items == nil {
		return Count{}, ErrInvalid
	}
	ids := make([]string, 0, len(input.Items))
	seen := make(map[string]bool, len(input.Items))
	for i := range input.Items {
		line := &input.Items[i]
		line.Note = strings.TrimSpace(line.Note)
		if !validID(line.VariantID) || seen[line.VariantID] || line.Counted != nil && *line.Counted < 0 ||
			line.Note != "" && !validText(line.Note, 300) {
			return Count{}, ErrInvalid
		}
		seen[line.VariantID] = true
		ids = append(ids, line.VariantID)
	}
	return s.changeCount(ctx, actor, input.StoreID, id, func(tx pgx.Tx, current Count) error {
		if current.Status != "DRAFT" {
			return ErrCountState
		}
		if current.Revision != *input.ExpectedRevision {
			return &CountConflicts{Code: "count_draft_stale"}
		}
		for _, line := range input.Items {
			tag, err := tx.Exec(ctx, `INSERT INTO stock_count_items(tenant_id,count_id,variant_id,sku_snapshot,expected_quantity,
				expected_revision,counted_quantity,note,product_snapshot,size_snapshot,color_snapshot)
				SELECT v.tenant_id,$2,v.id,v.sku,coalesce(b.quantity_on_hand,0),b.revision,$5,$6,p.name,v.size,v.color
				FROM product_variants v JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				LEFT JOIN stock_balances b ON (b.tenant_id,b.variant_id)=(v.tenant_id,v.id) AND b.location_id=$3
				WHERE v.tenant_id=$1 AND v.id=$4
				ON CONFLICT(tenant_id,count_id,variant_id) DO UPDATE SET counted_quantity=excluded.counted_quantity,note=excluded.note`,
				actor.TenantID, id, current.LocationID, line.VariantID, line.Counted, line.Note)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrNotFound
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM stock_count_items WHERE tenant_id=$1 AND count_id=$2
			AND NOT (variant_id=ANY($3::text[]::uuid[]))`, actor.TenantID, id, ids); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE stock_counts SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id)
		return err
	})
}

func (s Service) ReviewCount(ctx context.Context, actor auth.Identity, storeID, id string) (CountReview, error) {
	if !validID(id) {
		return CountReview{}, ErrInvalid
	}
	var result CountReview
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		current, err := readCount(ctx, tx, actor.TenantID, storeID, id)
		if err != nil {
			return err
		}
		if current.Status != "DRAFT" || !current.LocationActive {
			return ErrCountState
		}
		result, err = reviewCount(ctx, tx, actor.TenantID, current)
		return err
	})
	return result, err
}

func reviewCount(ctx context.Context, tx pgx.Tx, tenantID string, current Count) (CountReview, error) {
	rows, err := tx.Query(ctx, `SELECT i.variant_id,coalesce(b.revision::text,'unrecorded'),b.quantity_on_hand,
		coalesce(b.quantity_reserved,0),i.counted_quantity,i.counted_quantity-coalesce(b.quantity_on_hand,0),
		i.counted_quantity<coalesce(b.quantity_reserved,0)
		FROM stock_count_items i
		LEFT JOIN stock_balances b ON (b.tenant_id,b.location_id,b.variant_id)=(i.tenant_id,$3,i.variant_id)
		WHERE i.tenant_id=$1 AND i.count_id=$2 AND i.counted_quantity IS NOT NULL ORDER BY i.variant_id`,
		tenantID, current.ID, current.LocationID)
	if err != nil {
		return CountReview{}, err
	}
	lines, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CountReviewLine, error) {
		var line CountReviewLine
		err := row.Scan(&line.VariantID, &line.StockRevision, &line.Current, &line.Reserved, &line.Counted, &line.Delta, &line.Blocked)
		return line, err
	})
	return CountReview{Revision: current.Revision, Items: lines}, err
}

func (s Service) CloseCount(ctx context.Context, actor auth.Identity, id, key string, input CountClose) (Count, error) {
	if !keyPattern.MatchString(key) || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 ||
		len(input.References) == 0 || len(input.References) > maxInventoryLines {
		return Count{}, ErrInvalid
	}
	byID := make(map[string]string, len(input.References))
	for _, ref := range input.References {
		if !validID(ref.VariantID) || !validPrecondition(ref.StockRevision) || byID[ref.VariantID] != "" {
			return Count{}, ErrInvalid
		}
		byID[ref.VariantID] = ref.StockRevision
	}
	sort.Slice(input.References, func(i, j int) bool { return input.References[i].VariantID < input.References[j].VariantID })
	payload, err := json.Marshal(input)
	if err != nil {
		return Count{}, err
	}
	digest := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(digest[:])

	return s.changeCount(ctx, actor, input.StoreID, id, func(tx pgx.Tx, current Count) error {
		if current.Status == "COMPLETED" {
			if current.closeKey == nil || *current.closeKey != key {
				return ErrCountState
			}
			if current.CompletedBy == nil || *current.CompletedBy != actor.UserID || *current.closeFingerprint != fingerprint {
				return ErrKeyConflict
			}
			return nil
		}
		if current.Status != "DRAFT" {
			return ErrCountState
		}
		if current.Revision != *input.ExpectedRevision {
			return &CountConflicts{Code: "count_draft_stale"}
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stock_counts WHERE tenant_id=$1 AND close_key=$2)`,
			actor.TenantID, key).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrKeyConflict
		}
		if current.CountedCount != len(byID) {
			return ErrInvalid
		}

		ids := make([]string, 0, current.CountedCount)
		lines := make(map[string]CountLine, current.CountedCount)
		for _, line := range current.Items {
			if line.Counted == nil {
				continue
			}
			if byID[line.VariantID] == "" {
				return ErrInvalid
			}
			if err := LockCostVariant(ctx, tx, actor.TenantID, input.StoreID, line.VariantID); err != nil {
				return err
			}
			ids = append(ids, line.VariantID)
			lines[line.VariantID] = line
		}
		// The tenant write guard covers first-time balances; row locks also protect existing reservations.
		rows, err := tx.Query(ctx, `SELECT variant_id FROM stock_balances WHERE tenant_id=$1 AND location_id=$2
			AND variant_id=ANY($3::text[]::uuid[]) ORDER BY variant_id FOR UPDATE`, actor.TenantID, current.LocationID, ids)
		if err != nil {
			return err
		}
		if _, err := pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		review, err := reviewCount(ctx, tx, actor.TenantID, current)
		if err != nil {
			return err
		}
		var stale, blocked []string
		for _, line := range review.Items {
			if byID[line.VariantID] != line.StockRevision {
				stale = append(stale, line.VariantID)
			}
			if line.Blocked {
				blocked = append(blocked, line.VariantID)
			}
		}
		if len(stale) > 0 {
			return &CountConflicts{Code: "count_stale", Variants: stale}
		}
		if len(blocked) > 0 {
			return &CountConflicts{Code: "count_reserved", Variants: blocked}
		}
		for _, line := range review.Items {
			draft := lines[line.VariantID]
			var version *int64
			var before int32
			if line.Current != nil {
				before = *line.Current
				n, err := strconv.ParseInt(line.StockRevision, 10, 64)
				if err != nil {
					return err
				}
				version = &n
			}
			if line.Delta != 0 {
				reason := draft.Note
				if reason == "" {
					reason = current.Note
				}
				if reason == "" {
					reason = "Conteo físico"
				}
				// Use the current operational SKU for the new movement and retain the draft SKU in its own snapshot.
				var sku string
				if err := tx.QueryRow(ctx, `SELECT sku FROM product_variants WHERE tenant_id=$1 AND id=$2`,
					actor.TenantID, line.VariantID).Scan(&sku); err != nil {
					return err
				}
				if _, err := postMovement(ctx, tx, actor, Movement{LocationID: current.LocationID, VariantID: line.VariantID, SKU: sku,
					Type: "count_adjustment", Delta: line.Delta, Reason: reason, CountID: &id}, nil); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE stock_count_items SET expected_quantity=$4,expected_revision=$5
				WHERE tenant_id=$1 AND count_id=$2 AND variant_id=$3`, actor.TenantID, id, line.VariantID, before, version)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE stock_counts SET status='COMPLETED',completed_by=$3,completed_at=clock_timestamp(),
			closer_snapshot=(SELECT name FROM users WHERE tenant_id=$1 AND id=$3),
			close_key=$4,close_fingerprint=$5,revision=revision+1
			WHERE tenant_id=$1 AND id=$2`, actor.TenantID, id, actor.UserID, key, fingerprint)
		return err
	})
}

func (s Service) CountCloseResult(ctx context.Context, actor auth.Identity, storeID, id, key string) (Count, error) {
	if !validID(id) || !keyPattern.MatchString(key) {
		return Count{}, ErrInvalid
	}
	var result Count
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		current, err := readCount(ctx, tx, actor.TenantID, storeID, id)
		if err != nil {
			return err
		}
		if current.Status != "COMPLETED" || current.closeKey == nil || *current.closeKey != key ||
			current.CompletedBy == nil || *current.CompletedBy != actor.UserID {
			return ErrNotFound
		}
		result = current
		return nil
	})
	return result, err
}

type CountHistoryLocation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type CountHistory struct {
	Items     []CountSummary         `json:"items"`
	Locations []CountHistoryLocation `json:"locations"`
	Timezone  string                 `json:"timezone"`
	CanCount  bool                   `json:"canCount"`
	HasMore   bool                   `json:"hasMore"`
}

func (s Service) CountHistory(ctx context.Context, actor auth.Identity, storeID, location, status, query string, page int) (CountHistory, error) {
	query = strings.TrimSpace(query)
	if location != "" && !validID(location) || query != "" && !validText(query, 160) || page < 1 || page > ExistenceLastPage {
		return CountHistory{}, ErrInvalid
	}
	switch status {
	case "", "DRAFT", "COMPLETED", "CANCELLED":
	default:
		return CountHistory{}, ErrInvalid
	}
	var result CountHistory
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		result.CanCount, result.Timezone = allowed.adjust, allowed.timezone
		rows, err := tx.Query(ctx, `SELECT id,name,status='active' FROM stock_locations WHERE tenant_id=$1 AND store_id=$2
			ORDER BY (status='active') DESC,name,id`, actor.TenantID, storeID)
		if err != nil {
			return err
		}
		result.Locations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CountHistoryLocation])
		if err != nil {
			return err
		}
		if location != "" {
			found := false
			for _, item := range result.Locations {
				if item.ID == location {
					found = true
					break
				}
			}
			if !found {
				return ErrNotFound
			}
		}
		rows, err = tx.Query(ctx, countExtendedSQL+` WHERE c.tenant_id=$1 AND l.store_id=$2
			AND (nullif($3,'') IS NULL OR c.location_id=nullif($3,'')::uuid)
			AND ($4='' OR c.status=$4)
			AND ($5='' OR strpos(lower(c.id::text),lower($5))>0 OR strpos(lower(c.note),lower($5))>0 OR
				strpos(lower(coalesce(c.creator_snapshot,u.name)),lower($5))>0 OR EXISTS (
					SELECT 1 FROM stock_count_items i WHERE (i.tenant_id,i.count_id)=(c.tenant_id,c.id)
					AND (strpos(lower(i.sku_snapshot),lower($5))>0 OR strpos(lower(i.product_snapshot),lower($5))>0)))
			ORDER BY c.created_at DESC,c.id DESC LIMIT 26 OFFSET $6`, actor.TenantID, storeID, location, status, query, (page-1)*25)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (CountSummary, error) { return scanCountExtended(row) })
		if len(result.Items) > 25 {
			result.Items = result.Items[:25]
			result.HasMore = page < ExistenceLastPage
		}
		return err
	})
	return result, err
}
