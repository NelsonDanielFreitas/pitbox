package app.pitbox.secure;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class AuthRedirectTest {

    private static final String BASE = "app.pitbox.mobile:/auth/callback";

    @Test
    public void acceptsTheSuccessShape() {
        AuthRedirect.Result r = AuthRedirect.parse(BASE + "?code=abc123&state=xyz");
        assertNotNull(r);
        assertEquals("abc123", r.code);
        assertEquals("xyz", r.state);
        assertNull(r.error);
    }

    @Test
    public void acceptsAnErrorShape() {
        AuthRedirect.Result r = AuthRedirect.parse(BASE + "?error=access_denied&state=xyz&error_description=no");
        assertNotNull(r);
        assertNull(r.code);
        assertEquals("access_denied", r.error);
    }

    @Test
    public void refusesEverythingElse() {
        String[] bad = {
            null,
            "",
            "https://evil.example/auth/callback?code=a&state=b",
            "app.pitbox.mobile.evil:/auth/callback?code=a&state=b",
            "APP.PITBOX.MOBILE:/auth/callback?code=a&state=b".replace("APP", "other"),
            "app.pitbox.mobile://host/auth/callback?code=a&state=b",
            "app.pitbox.mobile:/auth/callback/extra?code=a&state=b",
            "app.pitbox.mobile:/auth/other?code=a&state=b",
            "app.pitbox.mobile:/auth/callback",
            BASE + "?code=a",
            BASE + "?state=b",
            BASE + "?code=&state=b",
            BASE + "?code=a&state=",
            BASE + "?code=a&state=b&extra=1",
            BASE + "?code=a&code=c&state=b",
            BASE + "?code=a&state=b#frag",
            BASE + "?code=a&error=x&state=b",
            BASE + "?error=x&unknown=1",
            BASE + "?error=",
            BASE + "?code=%zz&state=b",
            BASE + "?code" ,
            BASE + "?code=" + "a".repeat(AuthRedirect.MAX_VALUE + 1) + "&state=b",
            BASE + "?code=a&state=b" + "&".repeat(20),
            "not a uri \u0000",
        };
        for (String s : bad) {
            assertNull("accepted: " + s, AuthRedirect.parse(s));
        }
    }

    @Test
    public void authorizationUrlMustAnnounceOurRedirect() {
        String ok =
            "https://idp.example/oauth/v2/authorize?client_id=c&response_type=code&state=s&redirect_uri=app.pitbox.mobile%3A%2Fauth%2Fcallback";
        assertTrue(AuthRedirect.isAcceptableAuthorizationUrl(ok));
        String[] bad = {
            null,
            "",
            "http://idp.example/a?response_type=code&state=s&redirect_uri=app.pitbox.mobile%3A%2Fauth%2Fcallback",
            "https://user@idp.example/a?response_type=code&state=s&redirect_uri=app.pitbox.mobile%3A%2Fauth%2Fcallback",
            "https://idp.example/a?response_type=code&state=s",
            "https://idp.example/a?response_type=code&state=s&redirect_uri=https%3A%2F%2Fevil.example%2Fcb",
            "https://idp.example/a?response_type=code&redirect_uri=app.pitbox.mobile%3A%2Fauth%2Fcallback",
            "https://idp.example/a?response_type=token&state=s&redirect_uri=app.pitbox.mobile%3A%2Fauth%2Fcallback",
            ok + "&redirect_uri=https%3A%2F%2Fevil.example",
            ok + "#frag",
            "intent://idp.example/a#Intent;end",
            "javascript:alert(1)",
        };
        for (String s : bad) {
            assertFalse("accepted: " + s, AuthRedirect.isAcceptableAuthorizationUrl(s));
        }
    }

    @Test
    public void sessionKeyNamesAreRandomHexOnly() {
        DeviceKeyStore store = new DeviceKeyStore(DeviceKeyStore.SESSION_PREFIX, DeviceKeyStore.SESSION_NAME_PATTERN);
        assertEquals("pitbox.session-key.v1." + "a".repeat(32), store.aliasFor("a".repeat(32)));
        String[] bad = { null, "", "a".repeat(31), "a".repeat(33), "A".repeat(32), "g".repeat(32), "../" + "a".repeat(29), "a".repeat(32) + "\n" };
        for (String s : bad) {
            try {
                store.aliasFor(s);
                throw new AssertionError("accepted: " + s);
            } catch (IllegalArgumentException expected) {
                // fine
            }
        }
        // A sync-key hash is not a session key id, and the other way round.
        try {
            store.aliasFor("a".repeat(64));
            throw new AssertionError("a 64-hex name was accepted as a session key");
        } catch (IllegalArgumentException expected) {
            // fine
        }
    }
}
