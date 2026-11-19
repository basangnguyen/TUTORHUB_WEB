package identity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/webauthn"
)

// The same authenticator drives protocol tests and the runtime-role PostgreSQL
// journey. Only the authenticator creates a private key or signs an assertion.
type virtualPasskey struct {
	key        *ecdsa.PrivateKey
	ID, Handle []byte
}

func newVirtualPasskey(t testing.TB) *virtualPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	rand.Read(id)
	return &virtualPasskey{key: key, ID: id}
}

func passkeyJSON(t testing.TB, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func passkeyCBOR(t testing.TB, value any) []byte {
	t.Helper()
	b, err := webauthncbor.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func virtualAuthData(rpID string, uv, be, bs, attested bool, count uint32) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	flags := protocol.FlagUserPresent
	if uv {
		flags |= protocol.FlagUserVerified
	}
	if be {
		flags |= protocol.FlagBackupEligible
	}
	if bs {
		flags |= protocol.FlagBackupState
	}
	if attested {
		flags |= protocol.FlagAttestedCredentialData
	}
	return binary.BigEndian.AppendUint32(append(rpHash[:], byte(flags)), count)
}

func (a *virtualPasskey) response(t testing.TB, response map[string]any) json.RawMessage {
	id := base64.RawURLEncoding.EncodeToString(a.ID)
	return passkeyJSON(t, map[string]any{"id": id, "rawId": id, "type": "public-key", "response": response})
}

func (a *virtualPasskey) Register(t testing.TB, options json.RawMessage, origin string, uv, be, bs bool, count uint32) json.RawMessage {
	t.Helper()
	var opts struct {
		RP        struct{ ID string }                    `json:"rp"`
		User      struct{ ID protocol.URLEncodedBase64 } `json:"user"`
		Challenge string
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		t.Fatal(err)
	}
	a.Handle = opts.User.ID
	// Uncompressed P-256 keys carry a 0x04 prefix and two 32-byte coordinates.
	point, err := a.key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	cose := passkeyCBOR(t, map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:]})
	authData := virtualAuthData(opts.RP.ID, uv, be, bs, true, count)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.ID)))
	authData = append(authData, a.ID...)
	authData = append(authData, cose...)
	att := passkeyCBOR(t, map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	client := passkeyJSON(t, map[string]string{"type": "webauthn.create", "challenge": opts.Challenge, "origin": origin})
	return a.response(t, map[string]any{"clientDataJSON": protocol.URLEncodedBase64(client),
		"attestationObject": protocol.URLEncodedBase64(att), "transports": []string{"internal"}})
}

func (a *virtualPasskey) Assert(t testing.TB, options json.RawMessage, origin string, uv, be, bs bool, count uint32) json.RawMessage {
	t.Helper()
	var opts struct {
		RPID      string `json:"rpId"`
		Challenge string
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		t.Fatal(err)
	}
	authData := virtualAuthData(opts.RPID, uv, be, bs, false, count)
	client := passkeyJSON(t, map[string]string{"type": "webauthn.get", "challenge": opts.Challenge, "origin": origin})
	clientHash := sha256.Sum256(client)
	signed := sha256.Sum256(append(bytes.Clone(authData), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		t.Fatal(err)
	}
	return a.response(t, map[string]any{"clientDataJSON": protocol.URLEncodedBase64(client),
		"authenticatorData": protocol.URLEncodedBase64(authData), "signature": protocol.URLEncodedBase64(signature),
		"userHandle": protocol.URLEncodedBase64(a.Handle)})
}

func TestPasskeySignedCeremonies(t *testing.T) {
	const origin = "https://class.tutorhub.test"
	rp, err := NewPasskeyProtocol("tutorhub.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	user := PasskeyUser{Handle: make([]byte, 64), Name: "Fixture"}
	rand.Read(user.Handle)
	a := newVirtualPasskey(t)
	registration, session, err := rp.RegistrationOptions(user)
	if err != nil {
		t.Fatal(err)
	}
	var opts protocol.PublicKeyCredentialCreationOptions
	if err = json.Unmarshal(registration, &opts); err != nil {
		t.Fatal(err)
	}
	if opts.AuthenticatorSelection.ResidentKey != protocol.ResidentKeyRequirementRequired ||
		opts.AuthenticatorSelection.UserVerification != protocol.VerificationPreferred ||
		opts.Attestation != protocol.PreferNoAttestation || opts.Timeout != 300000 {
		t.Fatalf("unexpected registration policy: %#v", opts)
	}
	attestation := a.Register(t, registration, origin, true, true, true, 0)
	credential, err := rp.VerifyRegistration(user, *session, attestation)
	if err != nil {
		t.Fatal(err)
	}
	user.Credentials = []webauthn.Credential{*credential}
	registration2, rs2, err := rp.RegistrationOptions(user)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(registration2, &opts); err != nil || len(opts.CredentialExcludeList) != 1 || !bytes.Equal(opts.CredentialExcludeList[0].CredentialID, a.ID) {
		t.Fatal("missing exclusion")
	}
	if _, err = rp.VerifyRegistration(user, *rs2, attestation); err == nil {
		t.Fatal("accepted wrong registration challenge")
	}
	for _, badOrigin := range []string{"https://attacker.test", origin + "/fake"} {
		if _, err = rp.VerifyRegistration(user, *session, a.Register(t, registration, badOrigin, true, true, true, 0)); err == nil {
			t.Fatal("accepted invalid registration origin")
		}
	}
	// Validate the stored credential against assertions with and without UV. A
	// synchronized passkey whose counter stays zero is valid in both cases.
	for _, uv := range []bool{false, true} {
		options, s, err := rp.AssertionOptions(nil)
		if err != nil {
			t.Fatal(err)
		}
		var login protocol.PublicKeyCredentialRequestOptions
		if err = json.Unmarshal(options, &login); err != nil || len(login.AllowedCredentials) != 0 || login.UserVerification != protocol.VerificationPreferred {
			t.Fatal("discoverable policy")
		}
		raw := a.Assert(t, options, origin, uv, true, true, 0)
		parsed, err := rp.ParseAssertion(*s, raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rp.VerifyAssertion(user, *s, parsed)
		if err != nil || got.Authenticator.CloneWarning || parsed.Response.AuthenticatorData.Flags.HasUserVerified() != uv {
			t.Fatalf("valid assertion rejected: %v", err)
		}
		impostor := newVirtualPasskey(t)
		impostor.ID, impostor.Handle = a.ID, a.Handle
		wrongKey, err := rp.ParseAssertion(*s, impostor.Assert(t, options, origin, uv, true, true, 0))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = rp.VerifyAssertion(user, *s, wrongKey); err == nil {
			t.Fatal("accepted wrong signing key")
		}
		if _, err = rp.ParseAssertion(*s, a.Assert(t, options, "https://attacker.test", uv, true, true, 0)); err == nil {
			t.Fatal("accepted wrong origin")
		}
		login.RelyingPartyID = "attacker.test"
		if _, err = rp.ParseAssertion(*s, a.Assert(t, passkeyJSON(t, login), origin, uv, true, true, 0)); err == nil {
			t.Fatal("accepted wrong RP ID")
		}
		newOptions, newSession, err := rp.AssertionOptions(nil)
		if err != nil || bytes.Equal(newOptions, options) {
			t.Fatal("expected fresh challenge")
		}
		if _, err = rp.ParseAssertion(*newSession, raw); err == nil {
			t.Fatal("accepted wrong challenge")
		}
	}
	user.Credentials[0].Authenticator.SignCount = 5
	step, s, err := rp.AssertionOptions(&user)
	if err != nil {
		t.Fatal(err)
	}
	var stepOpts protocol.PublicKeyCredentialRequestOptions
	if err = json.Unmarshal(step, &stepOpts); err != nil || stepOpts.UserVerification != protocol.VerificationRequired ||
		len(stepOpts.AllowedCredentials) != 1 || !bytes.Equal(stepOpts.AllowedCredentials[0].CredentialID, a.ID) {
		t.Fatal("step-up policy")
	}
	if _, err = rp.ParseAssertion(*s, a.Assert(t, step, origin, false, true, true, 6)); err == nil {
		t.Fatal("step-up accepted without UV")
	}
	for _, count := range []uint32{0, 5, 6} {
		parsed, err := rp.ParseAssertion(*s, a.Assert(t, step, origin, true, true, false, count))
		if err != nil {
			t.Fatal(err)
		}
		got, err := rp.VerifyAssertion(user, *s, parsed)
		if err != nil || got.Authenticator.CloneWarning != (count <= 5) || got.Flags.BackupState {
			t.Fatalf("wrong counter/backup state for count=%d: %v", count, err)
		}
	}
}
