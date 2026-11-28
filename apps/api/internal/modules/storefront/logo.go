package storefront

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const (
	// MaxLogoUpload is the largest file accepted; the stored logo is smaller after resizing.
	MaxLogoUpload = 4 << 20
	logoSide      = 512
	maxLogoPixels = 4096
)

// ErrLogo means the file is not a PNG or JPEG image the page can show.
var ErrLogo = errors.New("el logo tiene que ser una imagen PNG o JPG de hasta 4 MB")

// logoPNG reads a PNG or JPEG logo and returns it as PNG, at most 512 px on its longest side, keeping transparency.
func logoPNG(source []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxLogoPixels || config.Height > maxLogoPixels {
		return nil, ErrLogo
	}
	picture, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, ErrLogo
	}
	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	targetWidth, targetHeight := width, height
	if width > logoSide || height > logoSide {
		targetWidth, targetHeight = logoSide, max(height*logoSide/width, 1)
		if height > width {
			targetWidth, targetHeight = max(width*logoSide/height, 1), logoSide
		}
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), picture, bounds.Min, draw.Src)
	output := rgba
	if targetWidth != width || targetHeight != height {
		output = image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		for y := range targetHeight {
			top, bottom := y*height/targetHeight, max((y+1)*height/targetHeight, y*height/targetHeight+1)
			for x := range targetWidth {
				left, right := x*width/targetWidth, max((x+1)*width/targetWidth, x*width/targetWidth+1)
				// Average weighted by opacity, so transparent edges do not darken the logo.
				var red, green, blue, alpha, count int
				for sy := top; sy < bottom; sy++ {
					row := rgba.Pix[sy*rgba.Stride+left*4 : sy*rgba.Stride+right*4]
					for i := 0; i < len(row); i += 4 {
						a := int(row[i+3])
						red, green, blue, alpha = red+int(row[i])*a, green+int(row[i+1])*a, blue+int(row[i+2])*a, alpha+a
						count++
					}
				}
				offset := y*output.Stride + x*4
				if alpha > 0 {
					output.Pix[offset], output.Pix[offset+1], output.Pix[offset+2] = uint8(red/alpha), uint8(green/alpha), uint8(blue/alpha)
				}
				output.Pix[offset+3] = uint8(alpha / count)
			}
		}
	}
	var result bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&result, output); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

// SetLogo stores a new logo, or removes it when source is nil. Returns the admin view with the new logo version.
func (s Service) SetLogo(ctx context.Context, actor auth.Identity, source []byte) (Admin, error) {
	var logo []byte
	if source != nil {
		if len(source) > MaxLogoUpload {
			return Admin{}, ErrLogo
		}
		var err error
		if logo, err = logoPNG(source); err != nil {
			return Admin{}, err
		}
	}
	var result Admin
	err := s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO storefronts(tenant_id,logo_png,logo_version,updated_at,updated_by)
			VALUES ($1,$2,1,now(),$3)
			ON CONFLICT (tenant_id) DO UPDATE SET logo_png=excluded.logo_png,logo_version=storefronts.logo_version+1,
				updated_at=now(),updated_by=excluded.updated_by`, actor.TenantID, logo, actor.UserID); err != nil {
			return err
		}
		return readAdmin(ctx, tx, actor.TenantID, &result)
	})
	return result, err
}

// AdminLogo lets the owner preview the logo even while the page is not published.
func (s Service) AdminLogo(ctx context.Context, actor auth.Identity) ([]byte, error) {
	var logo []byte
	err := s.manage(ctx, actor, false, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT logo_png FROM storefronts WHERE tenant_id=$1 AND logo_png IS NOT NULL", actor.TenantID).Scan(&logo)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return logo, err
}

// PublicLogo serves the logo of a published storefront only.
func (s Service) PublicLogo(ctx context.Context, slug string) ([]byte, error) {
	if !slugPattern.MatchString(slug) || len(slug) > 80 {
		return nil, ErrNotFound
	}
	var logo []byte
	err := s.db.QueryRow(ctx, `SELECT f.logo_png FROM storefronts f JOIN tenants t ON t.id=f.tenant_id
		JOIN stores st ON (st.tenant_id,st.id)=(f.tenant_id,f.store_id)
		WHERE t.slug=$1 AND t.status='active' AND f.enabled AND st.status='active' AND f.logo_png IS NOT NULL`, slug).Scan(&logo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return logo, err
}
