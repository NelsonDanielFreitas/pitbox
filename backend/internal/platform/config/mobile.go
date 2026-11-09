package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// maxCORSOrigins keeps the allow-list a short, reviewable list.
const maxCORSOrigins = 8

// mobileRedirectPath is the only path the native app's redirect may have.
const mobileRedirectPath = "/auth/callback"

var mobileClientID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,128}$`)

// privateScheme is the syntax of a URI scheme; validateMobileRedirect also demands a dot (RFC 8252,
// section 7.1: a reverse-domain name), so it
// cannot be a well-known scheme such as http, https or intent.
var privateScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// MobileEnabled reports whether the mobile login is turned on (ADR 0032).
func (c Config) MobileEnabled() bool { return c.OIDCMobileClientID != "" }

// validateMobile is all or nothing, and fails closed: CORS without the mobile login would
// open an origin for nothing, and the mobile login without CORS cannot work.
func (c Config) validateMobile() []error {
	if !c.MobileEnabled() {
		if len(c.CORSOrigins) > 0 {
			return []error{errors.New("PITBOX_CORS_ORIGINS: only valid with PITBOX_OIDC_MOBILE_CLIENT_ID")}
		}
		return nil
	}
	var errs []error
	if !mobileClientID.MatchString(c.OIDCMobileClientID) {
		errs = append(errs, errors.New("PITBOX_OIDC_MOBILE_CLIENT_ID: not a valid client id"))
	}
	if c.OIDCMobileClientID == c.OIDCClientID {
		errs = append(errs, errors.New("PITBOX_OIDC_MOBILE_CLIENT_ID: must differ from PITBOX_OIDC_CLIENT_ID (two applications, two audiences)"))
	}
	if !c.AuthConfigured() {
		errs = append(errs, errors.New("PITBOX_OIDC_MOBILE_CLIENT_ID: needs the web authentication to be configured too"))
	}
	if err := validateMobileRedirect(c.MobileRedirectURI); err != nil {
		errs = append(errs, err)
	}
	if len(c.CORSOrigins) == 0 {
		errs = append(errs, errors.New("PITBOX_CORS_ORIGINS: required with the mobile login (the WebView origin, for example https://localhost)"))
	}
	if len(c.CORSOrigins) > maxCORSOrigins {
		errs = append(errs, fmt.Errorf("PITBOX_CORS_ORIGINS: at most %d origins", maxCORSOrigins))
	}
	for i, origin := range c.CORSOrigins {
		if err := c.validateCORSOrigin(origin); err != nil {
			errs = append(errs, err)
		}
		if slices.Contains(c.CORSOrigins[:i], origin) {
			errs = append(errs, errors.New("PITBOX_CORS_ORIGINS: an origin is listed twice"))
		}
	}
	return errs
}

// validateMobileRedirect accepts only <reverse-domain scheme>:/auth/callback, with no
// authority, query or fragment. Errors never repeat the value.
func validateMobileRedirect(raw string) error {
	const name = "PITBOX_MOBILE_REDIRECT_URI"
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || !privateScheme.MatchString(u.Scheme) || !strings.Contains(u.Scheme, ".") {
		return fmt.Errorf("%s: scheme must be a reverse-domain private-use scheme (for example app.pitbox.mobile)", name)
	}
	if u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path != mobileRedirectPath {
		return fmt.Errorf("%s: must be <scheme>:%s with no authority, query or fragment", name, mobileRedirectPath)
	}
	// url.Parse lowercases nothing; a redirect compared as bytes must already be canonical.
	if raw != u.Scheme+":"+mobileRedirectPath {
		return fmt.Errorf("%s: not in canonical form", name)
	}
	return nil
}

// validateCORSOrigin accepts an exact origin as a browser sends it: scheme://host[:port], no
// trailing slash. Wildcards, paths and lists inside one entry do not parse as one.
func (c Config) validateCORSOrigin(raw string) error {
	const name = "PITBOX_CORS_ORIGINS"
	if raw == "" || strings.ContainsAny(raw, "*, ") {
		return fmt.Errorf("%s: an entry is empty or has a wildcard", name)
	}
	if err := c.validateOrigin(name, raw, false); err != nil {
		return errors.New(name + ": an entry is not a valid origin (https, or http for localhost in local)")
	}
	u, _ := url.Parse(raw) // validateOrigin parsed it
	if u.Path != "" || raw != u.Scheme+"://"+u.Host {
		return fmt.Errorf("%s: an entry is not in the form scheme://host[:port]", name)
	}
	return nil
}
