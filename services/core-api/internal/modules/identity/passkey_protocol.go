package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// PasskeyProtocol holds immutable RP configuration. The caller persists each
// returned SessionData inside the encrypted, single-use database transaction.
type PasskeyProtocol struct {
	rp *webauthn.WebAuthn
}

type PasskeyUser struct {
	Handle      []byte
	Name        string
	Credentials []webauthn.Credential
}

func (u PasskeyUser) WebAuthnID() []byte                         { return u.Handle }
func (u PasskeyUser) WebAuthnName() string                       { return u.Name }
func (u PasskeyUser) WebAuthnDisplayName() string                { return u.Name }
func (u PasskeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func NewPasskeyProtocol(rpID, rpName string, origins []string) (*PasskeyProtocol, error) {
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute, TimeoutUVD: 5 * time.Minute}
	rp, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: rpName, RPOrigins: slices.Clone(origins),
		AttestationPreference:  protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationPreferred},
		Timeouts:               webauthn.TimeoutsConfig{Registration: timeout, Login: timeout},
	})
	if err != nil {
		return nil, err
	}
	return &PasskeyProtocol{rp: rp}, nil
}

func (p *PasskeyProtocol) RegistrationOptions(user PasskeyUser) (json.RawMessage, *webauthn.SessionData, error) {
	if len(user.Handle) != 64 {
		return nil, nil, errors.New("invalid passkey user handle")
	}
	creation, session, err := p.rp.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()))
	if err != nil {
		return nil, nil, err
	}
	options, err := json.Marshal(creation.Response)
	return options, session, err
}

func (p *PasskeyProtocol) AssertionOptions(user *PasskeyUser) (json.RawMessage, *webauthn.SessionData, error) {
	var assertion *protocol.CredentialAssertion
	var session *webauthn.SessionData
	var err error
	if user == nil {
		assertion, session, err = p.rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	} else {
		if len(user.Handle) != 64 || len(user.Credentials) == 0 {
			return nil, nil, errors.New("no passkey available for step-up")
		}
		assertion, session, err = p.rp.BeginLogin(*user, webauthn.WithUserVerification(protocol.VerificationRequired))
	}
	if err != nil {
		return nil, nil, err
	}
	// Encode an explicit empty array for discoverable login. The same options
	// work with conditional mediation and with a user-initiated browser prompt.
	allowed := assertion.Response.AllowedCredentials
	if allowed == nil {
		allowed = []protocol.CredentialDescriptor{}
	}
	options, err := json.Marshal(struct {
		protocol.PublicKeyCredentialRequestOptions
		AllowCredentials []protocol.CredentialDescriptor `json:"allowCredentials"`
	}{assertion.Response, allowed})
	return options, session, err
}

func (p *PasskeyProtocol) VerifyRegistration(user PasskeyUser, session webauthn.SessionData, raw json.RawMessage) (*webauthn.Credential, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(p.rp.Config.RPOrigins, parsed.Response.CollectedClientData.Origin) ||
		len(parsed.RawID) > 1024 || !bytes.Equal(parsed.RawID, parsed.Response.AttestationObject.AuthData.AttData.CredentialID) {
		return nil, errors.New("invalid passkey registration")
	}
	return p.rp.CreateCredential(user, session, parsed)
}

// ParseAssertion checks the ceremony context before a credential lookup may
// report an unknown ID. Only VerifyAssertion can authenticate the assertion.
func (p *PasskeyProtocol) ParseAssertion(session webauthn.SessionData, raw json.RawMessage) (*protocol.ParsedCredentialAssertionData, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil {
		return nil, err
	}
	response := &parsed.Response
	flags := response.AuthenticatorData.Flags
	if len(parsed.RawID) > 1024 || len(response.Signature) == 0 || flags.HasAttestedCredentialData() ||
		(flags.HasBackupState() && !flags.HasBackupEligible()) ||
		(len(session.UserID) == 0 || len(response.UserHandle) != 0) && len(response.UserHandle) != 64 ||
		!session.Expires.IsZero() && !time.Now().Before(session.Expires) ||
		!slices.Contains(p.rp.Config.RPOrigins, response.CollectedClientData.Origin) {
		return nil, errors.New("invalid passkey assertion")
	}
	if err = response.CollectedClientData.Verify(session.Challenge, protocol.AssertCeremony, p.rp.Config.RPOrigins,
		nil, nil, protocol.TopOriginExplicitVerificationMode, false); err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(p.rp.Config.RPID))
	if err = response.AuthenticatorData.Verify(rpHash[:], nil, session.UserVerification == protocol.VerificationRequired, true); err != nil {
		return nil, err
	}
	return parsed, nil
}

func (p *PasskeyProtocol) VerifyAssertion(user PasskeyUser, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	if len(session.UserID) != 0 {
		return p.rp.ValidateLogin(user, session, parsed)
	}
	_, credential, err := p.rp.ValidatePasskeyLogin(func(_, handle []byte) (webauthn.User, error) {
		if !bytes.Equal(user.Handle, handle) {
			return nil, errors.New("invalid passkey user handle")
		}
		return user, nil
	}, session, parsed)
	return credential, err
}
