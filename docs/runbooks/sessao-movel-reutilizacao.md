# Sessão móvel: alarme de reutilização (`auth.mobile_reuse`)

**Quando:** há uma linha `auth.mobile_reuse` (motivo `refresh_reuse`) em `audit.audit_log`, ou o contador de recusas de autenticação sobe com o motivo `mobile_bad_proof`.

**O que significa.** Alguém apresentou uma renovação (`/auth/mobile/refresh`) com um contador já gasto, assinada com a chave do telemóvel. Só quem tem a chave consegue assinar, por isso é uma repetição de um pedido gravado, ou duas cópias da app a usar a mesma sessão. O servidor já apagou a sessão, revogou o refresh token no Zitadel e devolveu 401. O utilizador terá de entrar outra vez no telemóvel.

**O que fazer**
1. Ver o autor e a hora: `SELECT occurred_at, actor_id, meta FROM audit.audit_log WHERE action = 'auth.mobile_reuse' ORDER BY id DESC LIMIT 10;`
2. Perguntar ao utilizador se mudou de telemóvel, restaurou uma cópia ou tem duas instalações. Se sim, é benigno.
3. Se não: tratar como telemóvel comprometido. Terminar as outras sessões da conta (lista de sessões) e revogar o dispositivo (TM-DEV-02).
4. `bad_proof` isolado não é grave (pedido de quem não tem a chave: não gasta contador nem termina nada). Muitos seguidos do mesmo IP pedem limite no edge (F5-05).

**Nunca** copiar tokens, referências de sessão ou provas para um ticket.
