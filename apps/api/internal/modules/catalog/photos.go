package catalog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const MaxPhotoBytes = 5 << 20
const maxProductPhotos = 6

var ErrPhotoInvalid = errors.New("la foto debe ser JPEG o PNG válida y tener hasta 4096 píxeles por lado y 12 megapíxeles")
var ErrPhotoBytes = errors.New("la foto supera el límite de 5 MiB")
var ErrPhotoCount = errors.New("el producto ya tiene el máximo de 6 fotos")

type Photo struct {
	ID      string `json:"id"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Primary bool   `json:"primary"`
}

func (s Service) UploadPhoto(ctx context.Context, actor auth.Identity, productID, format string, input io.Reader) ([]Photo, error) {
	if _, valid := parseID(productID); !valid {
		return nil, ErrInvalid
	}
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		var id string
		return writeError(tx.QueryRow(ctx, "SELECT id FROM products WHERE tenant_id=$1 AND id=$2",
			actor.TenantID, productID).Scan(&id))
	})
	if err != nil {
		return nil, err
	}
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	default:
		return nil, auth.ErrBusy
	}
	source, err := io.ReadAll(io.LimitReader(input, MaxPhotoBytes+1))
	if err != nil {
		return nil, err
	}
	if len(source) > MaxPhotoBytes {
		return nil, ErrPhotoBytes
	}
	picture, metadata, err := normalizePhoto(source, format)
	if err != nil {
		return nil, err
	}
	var photos []Photo
	err = s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM product_images WHERE tenant_id=$1 AND product_id=$2",
			actor.TenantID, productID).Scan(&count); err != nil {
			return err
		}
		if count >= maxProductPhotos {
			return ErrPhotoCount
		}
		_, err := tx.Exec(ctx, `INSERT INTO product_images(tenant_id,product_id,width,height,is_primary,jpeg_data)
			SELECT tenant_id,id,$3,$4,$5,$6 FROM products WHERE tenant_id=$1 AND id=$2`,
			actor.TenantID, productID, metadata.Width, metadata.Height, count == 0, picture)
		if err != nil {
			return err
		}
		photos, err = listPhotos(ctx, tx, actor.TenantID, productID)
		return err
	})
	return photos, err
}

func (s Service) GetPhoto(ctx context.Context, actor auth.Identity, productID, imageID string) ([]byte, error) {
	if !validPhotoPath(productID, imageID) {
		return nil, ErrInvalid
	}
	var jpegData []byte
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		return writeError(tx.QueryRow(ctx, `SELECT i.jpeg_data FROM product_images i
			JOIN products p ON (p.tenant_id,p.id)=(i.tenant_id,i.product_id)
			WHERE i.tenant_id=$1 AND i.product_id=$2 AND i.id=$3 AND ($4 OR p.status='active')`,
			actor.TenantID, productID, imageID, manage).Scan(&jpegData))
	})
	return jpegData, err
}

func (s Service) SetPrimaryPhoto(ctx context.Context, actor auth.Identity, productID, imageID string) ([]Photo, error) {
	if !validPhotoPath(productID, imageID) {
		return nil, ErrInvalid
	}
	var photos []Photo
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var primary bool
		err := tx.QueryRow(ctx, "SELECT is_primary FROM product_images WHERE tenant_id=$1 AND product_id=$2 AND id=$3",
			actor.TenantID, productID, imageID).Scan(&primary)
		if err != nil {
			return writeError(err)
		}
		if !primary {
			if _, err := tx.Exec(ctx, "UPDATE product_images SET is_primary=false WHERE tenant_id=$1 AND product_id=$2 AND is_primary",
				actor.TenantID, productID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE product_images SET is_primary=true WHERE tenant_id=$1 AND product_id=$2 AND id=$3",
				actor.TenantID, productID, imageID); err != nil {
				return err
			}
		}
		photos, err = listPhotos(ctx, tx, actor.TenantID, productID)
		return err
	})
	return photos, err
}

func (s Service) DeletePhoto(ctx context.Context, actor auth.Identity, productID, imageID string) ([]Photo, error) {
	if !validPhotoPath(productID, imageID) {
		return nil, ErrInvalid
	}
	var photos []Photo
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var primary bool
		err := tx.QueryRow(ctx, "DELETE FROM product_images WHERE tenant_id=$1 AND product_id=$2 AND id=$3 RETURNING is_primary",
			actor.TenantID, productID, imageID).Scan(&primary)
		if err != nil {
			return writeError(err)
		}
		if primary {
			_, err := tx.Exec(ctx, `UPDATE product_images SET is_primary=true WHERE tenant_id=$1 AND product_id=$2 AND id=(
				SELECT id FROM product_images WHERE tenant_id=$1 AND product_id=$2 ORDER BY created_at,id LIMIT 1)`,
				actor.TenantID, productID)
			if err != nil {
				return err
			}
		}
		photos, err = listPhotos(ctx, tx, actor.TenantID, productID)
		return err
	})
	return photos, err
}

func listPhotos(ctx context.Context, tx pgx.Tx, tenantID, productID string) ([]Photo, error) {
	rows, err := tx.Query(ctx, `SELECT id,width,height,is_primary FROM product_images
		WHERE tenant_id=$1 AND product_id=$2 ORDER BY is_primary DESC,created_at,id`, tenantID, productID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Photo])
}

func validPhotoPath(productID, imageID string) bool {
	_, productOK := parseID(productID)
	_, imageOK := parseID(imageID)
	return productOK && imageOK
}

func normalizePhoto(source []byte, format string) ([]byte, Photo, error) {
	config, detected, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || detected != format || config.Width < 1 || config.Height < 1 ||
		config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 12000000 {
		return nil, Photo{}, ErrPhotoInvalid
	}
	picture, decoded, err := image.Decode(bytes.NewReader(source))
	if err != nil || decoded != detected || picture.Bounds().Dx() != config.Width || picture.Bounds().Dy() != config.Height {
		return nil, Photo{}, ErrPhotoInvalid
	}
	if detected == "png" {
		canvas := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(canvas, canvas.Bounds(), picture, picture.Bounds().Min, draw.Over)
		picture = canvas
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, picture, &jpeg.Options{Quality: 88}); err != nil {
		return nil, Photo{}, err
	}
	if output.Len() > MaxPhotoBytes {
		return nil, Photo{}, ErrPhotoBytes
	}
	return output.Bytes(), Photo{Width: config.Width, Height: config.Height}, nil
}
