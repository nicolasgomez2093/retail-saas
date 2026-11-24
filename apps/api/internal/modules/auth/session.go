package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnauthorized = errors.New("sesión o credenciales inválidas")
var ErrBusy = errors.New("demasiados intentos")
var ErrForbidden = errors.New("permiso insuficiente")

const SessionTTL = 8 * time.Hour

type Identity struct {
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	Name     string `json:"name"`
}

type Service struct {
	db        *pgxpool.Pool
	dummyHash string
	slots     chan struct{}
}

func New(db *pgxpool.Pool) (*Service, error) {
	dummyHash, err := HashPassword("credencial ficticia de verificación")
	if err != nil {
		return nil, err
	}
	return &Service{db: db, dummyHash: dummyHash, slots: make(chan struct{}, 2)}, nil
}

type CommerceChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type LoginChoice struct {
	Token     string           `json:"token"`
	Commerces []CommerceChoice `json:"commerces"`
}

type candidate struct {
	CommerceChoice
	UserID       string
	PasswordHash string
}

type choiceCredential struct {
	UserID      string `json:"userId"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Service) Login(ctx context.Context, slug, email, password string) (string, *LoginChoice, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return "", nil, ErrBusy
	}
	rows, err := s.db.Query(ctx, `SELECT u.tenant_id,t.name,t.slug,u.id,u.password_hash FROM users u
		JOIN tenants t ON t.id=u.tenant_id
		WHERE u.email=$1 AND ($2='' OR t.slug=$2) AND t.status='active' AND u.status='active'
		ORDER BY t.name,t.slug`, email, slug)
	if err != nil {
		return "", nil, err
	}
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var item candidate
		err := row.Scan(&item.ID, &item.Name, &item.Code, &item.UserID, &item.PasswordHash)
		return item, err
	})
	if err != nil {
		return "", nil, err
	}
	if len(candidates) == 0 {
		verifyPassword(password, s.dummyHash)
		return "", nil, ErrUnauthorized
	}
	matches := make([]candidate, 0, len(candidates))
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		if verifyPassword(password, item.PasswordHash) {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return "", nil, ErrUnauthorized
	case 1:
		token, err := s.issueSession(ctx, matches[0])
		return token, nil, err
	default:
		choice, err := s.issueChoice(ctx, matches)
		return "", choice, err
	}
}

func (s *Service) issueSession(ctx context.Context, item candidate) (string, error) {
	token, digest, err := newToken()
	if err != nil {
		return "", err
	}
	tag, err := s.db.Exec(ctx, `INSERT INTO sessions(token_hash,tenant_id,user_id,expires_at)
		SELECT $1,u.tenant_id,u.id,now()+make_interval(secs => $4)
		FROM users u JOIN tenants t ON t.id=u.tenant_id
		WHERE u.tenant_id=$2 AND u.id=$3 AND u.password_hash=$5 AND u.status='active' AND t.status='active'
		FOR SHARE OF u`,
		digest, item.ID, item.UserID, SessionTTL.Seconds(), item.PasswordHash)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrUnauthorized
	}
	return token, nil
}

func (s *Service) issueChoice(ctx context.Context, matches []candidate) (*LoginChoice, error) {
	token, digest, err := newToken()
	if err != nil {
		return nil, err
	}
	result := &LoginChoice{Token: token, Commerces: make([]CommerceChoice, 0, len(matches))}
	credentials := make(map[string]choiceCredential, len(matches))
	for _, item := range matches {
		result.Commerces = append(result.Commerces, item.CommerceChoice)
		fingerprint := sha256.Sum256([]byte(item.PasswordHash))
		credentials[item.ID] = choiceCredential{UserID: item.UserID, Fingerprint: hex.EncodeToString(fingerprint[:])}
	}
	if _, err := s.db.Exec(ctx, `WITH expired AS (DELETE FROM login_choices WHERE expires_at<=now())
		INSERT INTO login_choices(token_hash,candidates,expires_at) VALUES ($1,$2,now()+interval '5 minutes')`, digest, credentials); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) SelectCommerce(ctx context.Context, token, tenantID string) (string, error) {
	digest, ok := tokenHash(token)
	if !ok {
		return "", ErrUnauthorized
	}
	session, sessionDigest, err := newToken()
	if err != nil {
		return "", err
	}
	tag, err := s.db.Exec(ctx, `WITH chosen AS (
		DELETE FROM login_choices
		WHERE token_hash=$1 AND expires_at>now() AND candidates ? $2
		RETURNING candidates->$2 AS credential
	)
	INSERT INTO sessions(token_hash,tenant_id,user_id,expires_at)
	SELECT $3,u.tenant_id,u.id,now()+make_interval(secs => $4)
	FROM chosen JOIN users u ON u.id=(credential->>'userId')::uuid
	JOIN tenants t ON t.id=u.tenant_id
	WHERE u.tenant_id::text=$2 AND u.status='active' AND t.status='active'
		AND encode(sha256(convert_to(u.password_hash,'UTF8')),'hex')=credential->>'fingerprint'
	FOR SHARE OF u`, digest, tenantID, sessionDigest, SessionTTL.Seconds())
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrUnauthorized
	}
	return session, nil
}

func newToken() (string, []byte, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	digest, _ := tokenHash(token)
	return token, digest, nil
}

func (s *Service) Identity(ctx context.Context, token string) (Identity, error) {
	digest, valid := tokenHash(token)
	if !valid {
		return Identity{}, ErrUnauthorized
	}
	var identity Identity
	err := s.db.QueryRow(ctx, `SELECT u.tenant_id,u.id,u.name FROM sessions s
		JOIN users u ON (u.tenant_id,u.id)=(s.tenant_id,s.user_id)
		JOIN tenants t ON t.id=u.tenant_id
		WHERE s.token_hash=$1 AND s.expires_at>now() AND u.status='active' AND t.status='active'`,
		digest).Scan(&identity.TenantID, &identity.UserID, &identity.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrUnauthorized
	}
	return identity, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if digest, valid := tokenHash(token); valid {
		_, err := s.db.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", digest)
		return err
	}
	return nil
}

func tokenHash(token string) ([]byte, bool) {
	if len(token) != 43 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	digest := sha256.Sum256(raw)
	return digest[:], true
}
