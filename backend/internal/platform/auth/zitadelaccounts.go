package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Limits of the account client (ADR 0034).
const (
	accountsTimeout = 5 * time.Second
	accountsMaxBody = 64 << 10
)

// The errors carry constant text on purpose: they end up in logs, and nothing from the
// request (an email, a user id) or from the response may reach them.
var (
	errAccountsUnavailable = errors.New("auth: identity provider account API unavailable")
	errAccountsBadReply    = errors.New("auth: identity provider account API sent an unexpected reply")
	errAccountsBadSubject  = errors.New("auth: not an identity provider user id")
)

// subjectShape is a Zitadel user id: decimal digits. It is checked before the id goes into a
// URL path, so a hostile value can never add a path segment, a query or a second host.
var subjectShape = regexp.MustCompile(`^[0-9]{1,32}$`)

// AuthMethods says which kinds of sign-in credential an account has.
type AuthMethods struct {
	Passkey  bool
	Password bool
}

// AccountDirectory is what the recovery of a passkey-only account needs from the identity
// provider (ADR 0034). The Zitadel account used for it has one custom role with two
// permissions (read users, write passkeys): it cannot lock, delete or edit users, and cannot
// change settings.
type AccountDirectory interface {
	// FindUserByEmail returns the subject of the single active human account with this
	// verified email. ok is false when there is none (or more than one: never guess).
	FindUserByEmail(ctx context.Context, email string) (subject string, ok bool, err error)
	// AuthMethods lists the credential kinds of an account.
	AuthMethods(ctx context.Context, subject string) (AuthMethods, error)
	// SendPasskeyRegistrationLink asks the identity provider to email a passkey registration
	// link to the account. The code in the link never reaches this process.
	SendPasskeyRegistrationLink(ctx context.Context, subject string) error
}

// ZitadelAccounts implements AccountDirectory with the Zitadel v2 user API.
type ZitadelAccounts struct {
	base   string // origin of the issuer
	pat    string // never logged, never put in an error
	client *http.Client
}

var _ AccountDirectory = (*ZitadelAccounts)(nil)

// NewZitadelAccounts takes the issuer URL (the same host the login uses, so the instance is
// the right one), the personal access token of the recovery account and the HTTP client of
// the provider (which carries the local dial of the phone profile).
func NewZitadelAccounts(issuer, pat string, client *http.Client) (*ZitadelAccounts, error) {
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("auth: invalid issuer for the account API")
	}
	if pat == "" || strings.ContainsAny(pat, " \r\n\t") {
		return nil, errors.New("auth: invalid recovery token")
	}
	if client == nil {
		client = &http.Client{}
	}
	return &ZitadelAccounts{base: u.Scheme + "://" + u.Host, pat: pat, client: client}, nil
}

// LogValue keeps the token out of any log line that receives the value by mistake.
func (z *ZitadelAccounts) LogValue() slog.Value { return slog.StringValue("ZitadelAccounts") }

// String and GoString do the same for fmt verbs.
func (z *ZitadelAccounts) String() string   { return "ZitadelAccounts" }
func (z *ZitadelAccounts) GoString() string { return "ZitadelAccounts" }

// do sends one request and decodes the JSON answer into out (nil: discard). Every failure,
// whatever its cause, comes back as one of the constant errors.
func (z *ZitadelAccounts) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, accountsTimeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return errAccountsBadReply
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, z.base+path, body)
	if err != nil {
		return errAccountsUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+z.pat)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := z.client.Do(req)
	if err != nil {
		return errAccountsUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, accountsMaxBody+1))
	if err != nil || len(raw) > accountsMaxBody {
		return errAccountsBadReply
	}
	if resp.StatusCode != http.StatusOK {
		return errAccountsUnavailable
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errAccountsBadReply
	}
	return nil
}

// FindUserByEmail implements AccountDirectory.
func (z *ZitadelAccounts) FindUserByEmail(ctx context.Context, email string) (string, bool, error) {
	var reply struct {
		Result []struct {
			UserID string `json:"userId"`
			State  string `json:"state"`
			Human  *struct {
				Email struct {
					Email      string `json:"email"`
					IsVerified bool   `json:"isVerified"`
				} `json:"email"`
			} `json:"human"`
		} `json:"result"`
	}
	query := map[string]any{
		"queries": []any{map[string]any{"emailQuery": map[string]any{
			"emailAddress": email, "method": "TEXT_QUERY_METHOD_EQUALS",
		}}},
		"query": map[string]any{"limit": 2}, // two is enough to tell "exactly one" from "ambiguous"
	}
	if err := z.do(ctx, http.MethodPost, "/v2/users", query, &reply); err != nil {
		return "", false, err
	}
	if len(reply.Result) != 1 {
		return "", false, nil
	}
	u := reply.Result[0]
	if !subjectShape.MatchString(u.UserID) {
		return "", false, errAccountsBadReply
	}
	if u.State != "USER_STATE_ACTIVE" || u.Human == nil || !u.Human.Email.IsVerified ||
		!strings.EqualFold(u.Human.Email.Email, email) {
		return "", false, nil
	}
	return u.UserID, true, nil
}

// AuthMethods implements AccountDirectory.
func (z *ZitadelAccounts) AuthMethods(ctx context.Context, subject string) (AuthMethods, error) {
	if !subjectShape.MatchString(subject) {
		return AuthMethods{}, errAccountsBadSubject
	}
	var reply struct {
		Types []string `json:"authMethodTypes"`
	}
	if err := z.do(ctx, http.MethodGet, "/v2/users/"+subject+"/authentication_methods", nil, &reply); err != nil {
		return AuthMethods{}, err
	}
	var m AuthMethods
	for _, t := range reply.Types {
		switch t {
		case "AUTHENTICATION_METHOD_TYPE_PASSKEY":
			m.Passkey = true
		case "AUTHENTICATION_METHOD_TYPE_PASSWORD":
			m.Password = true
		}
	}
	return m, nil
}

// SendPasskeyRegistrationLink implements AccountDirectory. It always asks for the link to be
// sent by email (sendLink): the other mode, returnCode, would hand the code to this process.
func (z *ZitadelAccounts) SendPasskeyRegistrationLink(ctx context.Context, subject string) error {
	if !subjectShape.MatchString(subject) {
		return errAccountsBadSubject
	}
	err := z.do(ctx, http.MethodPost, "/v2/users/"+subject+"/passkeys/registration_link",
		map[string]any{"sendLink": map[string]any{}}, nil)
	if err != nil {
		return fmt.Errorf("send passkey registration link: %w", err)
	}
	return nil
}
