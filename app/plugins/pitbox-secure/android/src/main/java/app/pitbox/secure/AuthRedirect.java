package app.pitbox.secure;

import java.net.URI;
import java.net.URISyntaxException;
import java.nio.charset.StandardCharsets;
import java.net.URLDecoder;
import java.util.HashMap;
import java.util.Map;

/**
 * Strict checks for the browser login (ADR 0032). No Android dependency, so it is unit tested.
 *
 * Two things cross this class: the authorization URL the app asks the browser to open, and the
 * redirect the browser hands back. Both are untrusted until they pass here, and any doubt is a
 * refusal (fail closed).
 */
final class AuthRedirect {

    /** The only redirect URI of the native client (registered at the identity provider). */
    static final String REDIRECT_URI = "app.pitbox.mobile:/auth/callback";
    static final String REDIRECT_SCHEME = "app.pitbox.mobile";
    static final String REDIRECT_PATH = "/auth/callback";

    static final int MAX_URL = 4096;
    static final int MAX_VALUE = 2048;

    private AuthRedirect() {}

    /** Result of a redirect: exactly one of (code, state) or error is set. */
    static final class Result {

        final String code;
        final String state;
        final String error;

        private Result(String code, String state, String error) {
            this.code = code;
            this.state = state;
            this.error = error;
        }
    }

    /**
     * The URL the browser will open: https, no user info, and it must announce our own redirect
     * URI, so the JavaScript side cannot use the Auth Tab for any other login.
     */
    static boolean isAcceptableAuthorizationUrl(String url) {
        if (url == null || url.isEmpty() || url.length() > MAX_URL) {
            return false;
        }
        URI uri;
        try {
            uri = new URI(url);
        } catch (URISyntaxException e) {
            return false;
        }
        if (!"https".equals(uri.getScheme()) || uri.getHost() == null || uri.getUserInfo() != null || uri.getRawFragment() != null) {
            return false;
        }
        Map<String, String> query = parseQuery(uri.getRawQuery());
        return query != null
            && REDIRECT_URI.equals(query.get("redirect_uri"))
            && "code".equals(query.get("response_type"))
            && query.get("state") != null
            && !query.get("state").isEmpty();
    }

    /**
     * Validates what the Auth Tab returned. Accepts only our scheme and path, no authority, no
     * fragment, and a query of exactly code+state or error[+error_description][+state].
     *
     * @return the parsed result, or null when anything is off
     */
    static Result parse(String redirect) {
        if (redirect == null || redirect.isEmpty() || redirect.length() > MAX_URL) {
            return null;
        }
        URI uri;
        try {
            uri = new URI(redirect);
        } catch (URISyntaxException e) {
            return null;
        }
        if (
            !REDIRECT_SCHEME.equals(uri.getScheme()) ||
            uri.getRawAuthority() != null ||
            !REDIRECT_PATH.equals(uri.getRawPath()) ||
            uri.getRawFragment() != null
        ) {
            return null;
        }
        Map<String, String> query = parseQuery(uri.getRawQuery());
        if (query == null) {
            return null;
        }
        String code = query.get("code");
        String state = query.get("state");
        if (code != null) {
            // The success shape: code and state, and nothing else.
            if (query.size() != 2 || state == null || code.isEmpty() || state.isEmpty()) {
                return null;
            }
            return new Result(code, state, null);
        }
        String error = query.get("error");
        if (error == null || error.isEmpty()) {
            return null;
        }
        for (String name : query.keySet()) {
            if (!name.equals("error") && !name.equals("error_description") && !name.equals("state")) {
                return null;
            }
        }
        return new Result(null, state, error);
    }

    /** Query parameters, each name once, bounded in number and size; null when malformed. */
    static Map<String, String> parseQuery(String rawQuery) {
        Map<String, String> out = new HashMap<>();
        if (rawQuery == null || rawQuery.isEmpty()) {
            return out;
        }
        String[] pairs = rawQuery.split("&", -1);
        if (pairs.length > 16) {
            return null;
        }
        for (String pair : pairs) {
            int eq = pair.indexOf('=');
            if (eq <= 0) {
                return null;
            }
            String name;
            String value;
            try {
                name = URLDecoder.decode(pair.substring(0, eq), StandardCharsets.UTF_8);
                value = URLDecoder.decode(pair.substring(eq + 1), StandardCharsets.UTF_8);
            } catch (IllegalArgumentException e) {
                return null;
            }
            if (value.length() > MAX_VALUE || out.containsKey(name)) {
                return null;
            }
            out.put(name, value);
        }
        return out;
    }
}
