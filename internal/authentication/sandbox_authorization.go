package authentication

import (
	"context"
	"errors"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/security"
)

// Sandbox authorization timing and permission constants are fixed V1C policy.
const (
	// SandboxReauthorizationLifetime is the exact lifetime of one high-risk grant.
	SandboxReauthorizationLifetime = 2 * time.Minute
	// TOTPStep is the RFC 6238 counter interval.
	TOTPStep = 30 * time.Second
	// TOTPWindow accepts the immediately adjacent counter on either side.
	TOTPWindow = int64(1)

	// PermissionSandboxRead grants redacted sandbox visibility.
	PermissionSandboxRead = "sandbox.read"
	// PermissionSandboxArm grants bounded manual arming.
	PermissionSandboxArm = "sandbox.arm"
	// PermissionSandboxCancel grants cancellation in every safety state.
	PermissionSandboxCancel = "sandbox.cancel"
	// PermissionSandboxAdmin grants high-risk sandbox administration.
	PermissionSandboxAdmin = "sandbox.admin"
)

// AuthorizationPurpose is one closed high-risk authorization use.
type AuthorizationPurpose string

// High-risk authorization purposes are closed and purpose-bound.
const (
	// PurposeSandboxArm authorizes one bounded sandbox arm.
	PurposeSandboxArm AuthorizationPurpose = "sandbox_arm"
	// PurposeRiskUnlock authorizes one risk unlock.
	PurposeRiskUnlock AuthorizationPurpose = "risk_unlock"
	// PurposeCredentialRotate authorizes one credential rotation.
	PurposeCredentialRotate AuthorizationPurpose = "credential_rotation"
	// PurposeRevokeAllSessions authorizes global session revocation.
	PurposeRevokeAllSessions AuthorizationPurpose = "revoke_all_sessions"
	// PurposeStrategyConfiguration authorizes one versioned strategy configuration change.
	PurposeStrategyConfiguration AuthorizationPurpose = "strategy_configuration"
	// PurposeRiskControl authorizes one policy-loosening risk control change.
	PurposeRiskControl AuthorizationPurpose = "risk_control"
	// PurposeQualificationStart authorizes one approved formal qualification start.
	PurposeQualificationStart AuthorizationPurpose = "qualification_start"
	// PurposeConfigurationActivation authorizes one configuration activation.
	PurposeConfigurationActivation AuthorizationPurpose = "configuration_activation"
	// PurposeRoleChange authorizes one exact user-role revision.
	PurposeRoleChange AuthorizationPurpose = "role_change"
	// PurposeArtifactHold authorizes one evidence hold.
	PurposeArtifactHold AuthorizationPurpose = "artifact_hold"
)

// High-risk authorization errors are deliberately generic to callers.
var (
	// ErrReauthorizationFailed deliberately hides the rejected credential factor.
	ErrReauthorizationFailed = errors.New("sandbox_reauthorization_failed")
	// ErrAuthorizationInvalid reports an invalid, expired, replayed, or mismatched grant.
	ErrAuthorizationInvalid = errors.New("sandbox_authorization_invalid")
)

// NewSandboxAuthorization is persisted atomically with the monotonic TOTP
// counter and the high-risk attempt audit event.
type NewSandboxAuthorization struct {
	ID, TokenHash, UserID, SessionID string
	Purpose                          AuthorizationPurpose
	TOTPCounter                      int64
	SessionRevision                  int64
	CreatedAt, ExpiresAt             time.Time
	SourceHash, ReasonHash           string
	TargetRevision                   *int64
	Audit                            HighRiskAudit
}

// HighRiskAudit is redacted, hash-linked evidence for one privileged attempt
// or result. Before/after are state hashes, never payloads.
type HighRiskAudit struct {
	ID, ActorUserID, SessionID string
	Purpose                    AuthorizationPurpose
	Outcome, SourceHash        string
	ReasonHash                 string
	Revision                   int64
	TargetRevision             *int64
	BeforeHash, AfterHash      string
	PreviousHash, EventHash    string
	OccurredAt                 time.Time
}

// AuthorizationGrant is the only plaintext presentation of a one-use token.
type AuthorizationGrant struct {
	Token     string
	Purpose   AuthorizationPurpose
	ExpiresAt time.Time
}

// ConsumedAuthorization identifies one atomically consumed persisted grant.
type ConsumedAuthorization struct {
	ID, UserID, SessionID string
	Purpose               AuthorizationPurpose
	SourceHash            string
	ReasonHash            string
	ConsumedAt            time.Time
	TargetRevision        *int64
}

// AuthorizationBindingHash returns the canonical redacted binding used for
// high-risk source and reason comparisons. It never exposes the input value.
func AuthorizationBindingHash(value string) string {
	return stableHash(strings.TrimSpace(value))
}

// SandboxAuthorizationStore owns the compare-and-set TOTP counter, one-use
// grants, session revocation, and append-only audit hash chain.
type SandboxAuthorizationStore interface {
	CreateSandboxAuthorization(context.Context, NewSandboxAuthorization) error
	ConsumeSandboxAuthorization(context.Context, string, string, AuthorizationPurpose, time.Time) (ConsumedAuthorization, error)
	RevokeAllUserSessions(context.Context, string, string, string, string, string, time.Time) (int64, error)
	AppendHighRiskAudit(context.Context, HighRiskAudit) error
}

// SandboxAuthorizationService verifies high-risk factors and issues one-use grants.
type SandboxAuthorizationService struct {
	users    Store
	store    SandboxAuthorizationStore
	clock    domain.Clock
	hasher   PasswordHasher
	totpSeed []byte
}

// NewSandboxAuthorizationService loads the API-only seed from its fixed secret file.
func NewSandboxAuthorizationService(
	users Store,
	store SandboxAuthorizationStore,
	clock domain.Clock,
	totpSeedFile string,
) (*SandboxAuthorizationService, error) {
	if users == nil || store == nil || clock == nil || totpSeedFile == "" {
		return nil, ErrConfiguration
	}
	encoded, err := security.ReadSecretFile(totpSeedFile)
	if err != nil {
		return nil, err
	}
	seed, err := decodeTOTPSeed(encoded)
	if err != nil {
		return nil, ErrConfiguration
	}
	return &SandboxAuthorizationService{
		users: users, store: store, clock: clock, hasher: PasswordHasher{},
		totpSeed: seed,
	}, nil
}

// Reauthenticate verifies the current user password and RFC 6238 code, then
// persists a session/purpose-bound one-use grant. Failures are generic.
func (service *SandboxAuthorizationService) Reauthenticate(
	ctx context.Context,
	principal Principal,
	password, code string,
	purpose AuthorizationPurpose,
	source, reason string,
) (AuthorizationGrant, error) {
	if isRevisionBoundPurpose(purpose) {
		return AuthorizationGrant{}, ErrReauthorizationFailed
	}
	now := service.clock.Now().UTC
	issued := false
	defer func() {
		if !issued {
			service.recordRejectedAttempt(ctx, principal, purpose, source, reason, now)
		}
	}()
	counter, err := service.verifyReauthentication(ctx, principal, password, code, purpose, reason, now)
	if err != nil {
		return AuthorizationGrant{}, err
	}
	write, token, err := newSandboxAuthorizationWrite(principal, purpose, source, reason, counter, nil, now)
	if err != nil {
		return AuthorizationGrant{}, ErrReauthorizationFailed
	}
	if err = service.store.CreateSandboxAuthorization(ctx, write); err != nil {
		return AuthorizationGrant{}, ErrReauthorizationFailed
	}
	issued = true
	return AuthorizationGrant{Token: token, Purpose: purpose, ExpiresAt: write.ExpiresAt}, nil
}

// ReauthenticateForRevision issues a grant bound to one positive target
// revision. D1 high-risk commands must consume and match that exact revision.
func (service *SandboxAuthorizationService) ReauthenticateForRevision(
	ctx context.Context,
	principal Principal,
	password, code string,
	purpose AuthorizationPurpose,
	targetRevision int64,
	source, reason string,
) (AuthorizationGrant, error) {
	if targetRevision <= 0 || !isRevisionBoundPurpose(purpose) {
		return AuthorizationGrant{}, ErrReauthorizationFailed
	}
	now := service.clock.Now().UTC
	issued := false
	defer func() {
		if !issued {
			service.recordRejectedAttempt(ctx, principal, purpose, source, reason, now)
		}
	}()
	counter, err := service.verifyReauthentication(ctx, principal, password, code, purpose, reason, now)
	if err != nil {
		return AuthorizationGrant{}, err
	}
	write, token, err := newSandboxAuthorizationWrite(
		principal, purpose, source, reason, counter, &targetRevision, now,
	)
	if err != nil || service.store.CreateSandboxAuthorization(ctx, write) != nil {
		return AuthorizationGrant{}, ErrReauthorizationFailed
	}
	issued = true
	return AuthorizationGrant{Token: token, Purpose: purpose, ExpiresAt: write.ExpiresAt}, nil
}

func (service *SandboxAuthorizationService) verifyReauthentication(
	ctx context.Context,
	principal Principal,
	password, code string,
	purpose AuthorizationPurpose,
	reason string,
	now time.Time,
) (int64, error) {
	if principal.UserID == "" || principal.SessionID == "" || strings.TrimSpace(reason) == "" ||
		!validPurpose(purpose) || RequirePermission(principal, permissionForPurpose(purpose)) != nil {
		return 0, ErrReauthorizationFailed
	}
	user, err := service.users.UserForLogin(ctx, normalizeEmail(principal.Email))
	if err != nil || user.ID != principal.UserID || user.Status != "active" {
		return 0, ErrReauthorizationFailed
	}
	valid, _, err := service.hasher.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return 0, ErrReauthorizationFailed
	}
	counter, ok := validateTOTP(service.totpSeed, code, now, TOTPWindow)
	if !ok {
		return 0, ErrReauthorizationFailed
	}
	return counter, nil
}

func newSandboxAuthorizationWrite(
	principal Principal,
	purpose AuthorizationPurpose,
	source, reason string,
	counter int64,
	targetRevision *int64,
	now time.Time,
) (NewSandboxAuthorization, string, error) {
	id, err := newIdentifier("sandbox-auth")
	if err != nil {
		return NewSandboxAuthorization{}, "", err
	}
	token, err := newOpaqueToken()
	if err != nil {
		return NewSandboxAuthorization{}, "", err
	}
	expires := now.Add(SandboxReauthorizationLifetime)
	sourceHash := stableHash(strings.TrimSpace(source))
	reasonHash := stableHash(strings.TrimSpace(reason))
	auditID, err := newIdentifier("audit")
	if err != nil {
		return NewSandboxAuthorization{}, "", err
	}
	audit := HighRiskAudit{
		ID: auditID, ActorUserID: principal.UserID, SessionID: principal.SessionID,
		Purpose: purpose, Outcome: "authorization_issued", SourceHash: sourceHash,
		ReasonHash: reasonHash, Revision: principal.SessionRevision,
		TargetRevision: targetRevision, OccurredAt: now,
		EventHash: stableHash(auditID, principal.UserID, principal.SessionID, string(purpose),
			"authorization_issued", sourceHash, reasonHash, now.Format(time.RFC3339Nano)),
	}
	return NewSandboxAuthorization{
		ID: id, TokenHash: tokenHash(token), UserID: principal.UserID, SessionID: principal.SessionID,
		Purpose: purpose, TOTPCounter: counter, SessionRevision: principal.SessionRevision,
		CreatedAt: now, ExpiresAt: expires,
		SourceHash: sourceHash, ReasonHash: reasonHash,
		TargetRevision: targetRevision, Audit: audit,
	}, token, nil
}

// Consume atomically consumes a grant for the exact session and purpose. The
// durable store also appends the consumed-result audit in the same transaction.
func (service *SandboxAuthorizationService) Consume(
	ctx context.Context,
	principal Principal,
	token string,
	purpose AuthorizationPurpose,
) (ConsumedAuthorization, error) {
	if len(token) < 32 || principal.SessionID == "" || !validPurpose(purpose) {
		return ConsumedAuthorization{}, ErrAuthorizationInvalid
	}
	consumed, err := service.store.ConsumeSandboxAuthorization(
		ctx, tokenHash(token), principal.SessionID, purpose, service.clock.Now().UTC,
	)
	if err != nil || consumed.UserID != principal.UserID || consumed.SessionID != principal.SessionID ||
		consumed.Purpose != purpose {
		return ConsumedAuthorization{}, ErrAuthorizationInvalid
	}
	return consumed, nil
}

// RevokeAll consumes its dedicated grant before revoking every user session.
func (service *SandboxAuthorizationService) RevokeAll(
	ctx context.Context,
	principal Principal,
	authorizationToken string,
) (int64, error) {
	consumed, err := service.Consume(
		ctx,
		principal,
		authorizationToken,
		PurposeRevokeAllSessions,
	)
	if err != nil {
		return 0, err
	}
	count, err := service.store.RevokeAllUserSessions(
		ctx,
		consumed.ID,
		principal.UserID,
		principal.SessionID,
		consumed.SourceHash,
		consumed.ReasonHash,
		service.clock.Now().UTC,
	)
	if err != nil {
		return 0, ErrAuthorizationInvalid
	}
	return count, nil
}
