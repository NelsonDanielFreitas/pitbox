# 0032 — Login móvel e sessão ligada ao dispositivo

- **Estado:** Aceite (confirmado pelo Nelson em 2026-11-09; implementado no F2-06b)
- **Data:** 2026-11-09
- **Cartão:** F2-06b (#166); inclui #154 (back-channel logout em local) e #158 (conta só com passkey, ver ADR 0034)
- **Relacionado:** [ADR 0004](0004-zitadel-bff.md), [ADR 0013](0013-sessoes-bff-e-tokens.md) (estende: transporte Bearer), [ADR 0017](0017-app-web-bff-mesma-origem.md) (CORS), [ADR 0025](0025-formato-de-assinatura-e-cursor-do-sync.md), [ADR 0028](0028-app-instalavel-e-arranque-sem-rede.md), [ADR 0029](0029-acesso-do-telemovel-pela-tailnet.md), [ADR 0033](0033-base-e-chaves-nativas-no-android.md); arquitetura §5.1, §7.4; ameaças TM-AUTH-03, TM-AUTH-04, TM-AUTH-20, TM-DEV-01, TM-DEV-03.

## Contexto

A app Android (F2-06) tem base cifrada e chave no Keystore, mas não consegue autenticar-se: a API só aceita o cookie `__Host-` do BFF e recusa a origem `https://localhost` do WebView. O F2-06b introduz a primeira credencial aceite de um cliente não confiável fora do browser, por isso o desenho tem de limitar o que um token roubado ou um código intercetado permitem.

Factos que condicionam a decisão:

- A spec §3.4 exige que a reutilização de um refresh token revogue a família. O Zitadel roda o refresh token mas não o faz (ADR 0004/0013). Um refresh token no telemóvel seria por isso um segredo de longa duração sem deteção de roubo.
- O Zitadel não documenta DPoP; os tempos de vida dos tokens são da instância, não por aplicação.
- O AppAuth-Android não tem versão desde dezembro de 2021; os plugins Capacitor de OAuth são poucos e o da Capawesome é pago.
- O Auth Tab (`androidx.browser` 1.9.0, Chrome 137+) devolve o resultado do redirect por *callback* à app, sem *intent filter*, logo outra app não o pode intercetar. App Links HTTPS exigem Digital Asset Links num domínio público, impossível no host da tailnet (o Funnel está proibido, ADR 0029).
- O `go-oidc` `RemoteKeySet` volta ao JWKS a cada `kid` desconhecido, sem TTL nem limite. O endpoint público `POST /auth/backchannel-logout` já hoje valida JWT com ele.

## Decisão

### 1. Sessão no servidor ligada a uma chave do telemóvel (token-mediating backend)

O telemóvel **não guarda refresh token nem valida JWT**. O refresh token do Zitadel fica selado no servidor (Valkey, `Sealer`), como na web. O telemóvel recebe um **Bearer opaco** de 10 min, `pbm1_<43 base64url>`, e uma chave de sessão P-256 própria no Keystore, distinta da chave do sync.

- A sessão `kind=mobile` usa o mesmo `Store`, o mesmo `Authenticator` e produz o **mesmo `Principal`** (`user_id`, `session_ref`, `amr`, `auth_time` vêm do ID token capturado no login). O step-up funciona sem alterações.
- O Bearer guarda-se como hash (`pitbox:mob:at:<sha256>` → ref, TTL 10 min). Qualquer JWT ou ID token apresentado como Bearer não tem o formato e é recusado antes de qualquer consulta.
- **Bearer e cookie no mesmo pedido: recusado.** O Bearer não é credencial ambiente, por isso não passa pela verificação `Origin`/`Sec-Fetch-Site`; a CORS limita quem o pode enviar.

### 2. Login em três passos (Auth Code + PKCE, cliente nativo público)

Segunda aplicação Zitadel `pitbox-android` (nativa, `auth method NONE`, PKCE S256, grants code + refresh, access token opaco, redirect `app.pitbox.mobile:/auth/callback`). O pedido de autorização leva `prompt=login` (telemóvel partilhado: nunca SSO silencioso).

1. `POST /auth/mobile/start` `{session_pub, code_challenge}`: guarda `{challenge, nonce, session_pub}` em Valkey (5 min, uso único, teto global de pendentes) e devolve o URL de autorização.
2. A app abre-o no **Auth Tab, obrigatório e com falha fechada** (`CustomTabsClient.isAuthTabSupported`; sem suporte, mensagem a pedir atualização do Chrome, sem alternativa por Custom Tab). O plugin valida o redirect de forma estrita (esquema e caminho exatos; só `code`+`state` ou `error`; tamanhos).
3. `POST /auth/mobile/token` `{state, code, code_verifier, proof}`: consome o pendente (`GETDEL`), confere o PKCE, verifica `proof` (assinatura da chave de sessão sobre `state‖code`; quem intercetar o `code` não tem a chave), troca o código no Zitadel, valida o ID token com um verificador do cliente móvel (`iss`, `aud`, `exp`, `nonce`, algoritmos fixos), consulta o `userinfo`, resolve o utilizador, aplica a regra do fator forte para donos e cria a sessão.

### 3. Renovação e deteção de reutilização (cumpre a spec §3.4)

`POST /auth/mobile/refresh` `{counter, proof}`; `proof` assina `refresh‖session_ref‖counter` com a chave de sessão. O servidor exige `counter == guardado+1` (CAS em Valkey) e renova no Zitadel pelo código de refresh existente. **Contador repetido ou em atraso é reutilização:** apaga a sessão, revoga o refresh token no Zitadel, regista `auth.mobile_reuse` e devolve 401. Um reenvio idêntico (mesmo contador e mesma prova) em 30 s é repetição de rede, não reutilização.

Efeito sobre o ADR 0013/0004: a revogação da família passa a existir **na nossa camada**, para a sessão móvel, enquanto o Zitadel não a fizer.

### 4. Logout

`POST /auth/mobile/logout` (Bearer): revoga no Zitadel e apaga a sessão. A app apaga depois a chave de sessão, a base e o Bearer em memória, mantendo a regra «só depois de sincronizado» do ADR 0033. O back-channel logout e «terminar sessão» da web apagam por `sub`/`sid`, logo também cortam o telemóvel.

### 5. O que fica no telemóvel

Bearer só em memória do JS (arranque a frio: um refresh; sem rede: sessão offline pelo passe de 72 h do ADR 0028, inalterado). `{key_id, counter}` em ficheiro privado sem backup (não são segredos). Chave de sessão no Keystore (StrongBox ou TEE), alias aleatório de 128 bits validado como 32 hex, domínio de assinatura `pitbox-mobile-v1/…`.

### 6. CORS

Lista exata de origens em `PITBOX_CORS_ORIGINS` (para já `https://localhost`); nunca `*`, nunca refletir a origem; `Vary: Origin`; **sem** `Access-Control-Allow-Credentials`; métodos e cabeçalhos fixos (`Authorization`, `Content-Type`, `Connect-*`); preflight 204 só para origem permitida. Pedidos com cookie de origem estrangeira continuam recusados.

### 7. JWKS limitado

`boundedKeySet` (implementa `oidc.KeySet`, sobre `go-jose/v4`, já no grafo): TTL 10 min; refetch por `kid` desconhecido no máximo a cada 30 s (≤ 2 pedidos/min); *single-flight*; cache negativa de `kid` (60 s, limitada); corpo ≤ 256 KiB; timeout 5 s; o mesmo `HTTPClient` do provider (inclui `IssuerDial`). Aplica-se aos verificadores do ID token (web e móvel) e do logout token, o que corrige também a fraqueza já existente no back-channel.

### 8. Reinterpretação dos critérios do cartão

Como o Bearer é opaco, os critérios «ID token como Bearer rejeitado» e «token de outro `client_id`, emissor, `alg` none/HS256 ou expirado» cumprem-se assim: (a) qualquer JWT como Bearer é rejeitado por formato (teste com ID token real); (b) a tabela de casos, no estilo `TestIDTokenValidation`, aplica-se ao ID token e ao logout token do cliente móvel; (c) o limite do JWKS aplica-se a esses dois verificadores. O texto do cartão e `docs/cartoes.json` são atualizados em conformidade.

## Alternativas

- **Refresh token no telemóvel (AppAuth clássico).** Segredo de longa duração sem deteção de roubo, porque o Zitadel não revoga a família. Recusado.
- **JWT como Bearer, validado localmente.** Alarga a superfície (JWKS por pedido, confusão com o ID token, sem revogação imediata). Recusado.
- **AppAuth-Android ou plugin de terceiros.** Sem manutenção ou pago; o código necessário é pequeno e fica no plugin próprio. Recusado.
- **Custom Tab como alternativa ao Auth Tab.** Exigiria *intent filter* por esquema, que outra app pode registar. Recusado por ti (falha fechada).
- **App Link HTTPS.** Requer domínio público; fica para o F5.

## Consequências

- O servidor passa a ter estado por sessão móvel (chave pública, contador, hash do access token), tudo em Valkey com TTL.
- O Bearer de 10 min na memória do WebView é o resíduo principal: um XSS que contornasse a CSP estrita poderia lê-lo. Endurecimento futuro: cabeçalho injetado por HTTP nativo.
- Código malicioso com o telemóvel em root pode assinar renovações; o bloqueio biométrico é só de UI. Aceite, como a chave do sync.
- Sem Auth Tab não há login. Mudar o domínio do Zitadel invalida as passkeys (ADR 0029).
- Dependências: `go-jose/v4` passa a direta; `androidx.browser` 1.9.0 (a confirmar a versão antes de fixar).

## Resultado dos spikes (2026-11-09, contra o Zitadel 4.19.5 local)

1. **Aplicação nativa (provado).** `CreateApplication` aceita `OIDC_APP_TYPE_NATIVE` + `OIDC_AUTH_METHOD_TYPE_NONE` + redirect `app.pitbox.mobile:/auth/callback` **sem** `developmentMode`, e não devolve segredo (só `clientId`). O Zitadel deriva sozinho `allowedOrigins: ["app.pitbox.mobile://"]`; a deteção de desvio do provisionamento tem de o ignorar. A aplicação de teste foi apagada.
2. **#154 (viável).** Sem `extra_hosts`, o `host.docker.internal` do Docker Desktop (192.168.65.254) chega, a partir de um contentor da rede `pitbox_default`, a um servidor em `127.0.0.1` do WSL2 (modo mirrored): um `POST /auth/backchannel-logout` chegou ao listener. O `host-gateway` do Docker Desktop resolve para IPv6 inacessível, por isso **não** se usa `extra_hosts`. O Zitadel aceita `http://host.docker.internal:8081/auth/backchannel-logout` como `backChannelLogoutUri` (com `developmentMode`, e a denylist local já substituída). **Por provar na implementação:** o `POST` real do Zitadel a terminar uma sessão e o `pitbox-api` a aceitá-lo (verificar se o endpoint olha para o `Host`).
3. **#158 (ver ADR 0034).** A API v2 do Zitadel permite, para um utilizador sem palavra-passe, `PasswordReset` e `CreatePasskeyRegistrationLink` (ambos com `returnCode`). O login v2 só mostra «Use password» na página da passkey; não há nenhuma entrada «perdi a passkey» para quem só tem passkey. As duas chamadas precisam de uma credencial de gestão.
4. **Telemóvel (Samsung SM-S911B, Android 17), só leitura via `adb`:** o browser por omissão é o **Chrome 156** (o Auth Tab exige Chrome ≥ 137) e é o único fornecedor de Custom Tabs instalado; há dois gestores de credenciais com passkeys (Google Password Manager e Samsung Pass). As condições para o Auth Tab e para passkeys estão cumpridas. **Por provar quando o plugin existir (passo 5 do plano):** o Auth Tab a devolver o redirect e a passkey a abrir com o RP ID do domínio do Zitadel na tailnet; se falhar, volta-se a este ADR antes de seguir.

## Resultado da implementação (2026-11-09)

Provado no Samsung SM-S911B (Chrome 156) com o Zitadel 4.19.5 local pela tailnet: login com passkey no Auth Tab, sessão criada, `/auth/session` com o Bearer, sync assinado com Bearer, logout, e o `logcat` sem nenhum Bearer.

Achados que mudaram o código (não o desenho):

- **`<queries>` no manifest.** Sem ele o Android 11+ esconde o Chrome da app e `CustomTabsClient.getPackageName/isAuthTabSupported` falham sempre, o que a app mostrava como «atualiza o Chrome».
- **Renovação no Zitadel com o cliente certo.** O refresh token de uma sessão móvel pertence ao cliente nativo; a renovação usa a configuração OAuth do cliente móvel (o Zitadel liga o token ao cliente).
- **JWKS limitado** serve também o back-channel logout (fraqueza anterior ao F2-06b).
- **Sem limitador, os endpoints móveis não servem** (503): falha fechada.
- **Registo do Capacitor** (`loggingBehavior`) desligado fora do build de debug: no modo verboso o código de autorização e as instruções SQL iam para o `logcat`.
- **Correções do F2-06 descobertas no primeiro uso real** (não são do login): o bloqueio da app apagava a sessão em memória e entrava em ciclo; a base nativa recusava `INSERT … SELECT … RETURNING` e perdia a linha de `INSERT … RETURNING` quando o `rowid` era menor que o da fila de sync; o Android escondia o motivo das restrições, o que mostrava sempre «conflito com outro registo».

Fora deste cartão, por provar: renovação aos ~9 minutos no telemóvel, modo avião, e o #154/#158 (ver ADR 0034).

## Emenda (F2-06b, 2026-11-10): o Bearer chega à página «Conta e segurança»

- **Endpoints de conta com Bearer.** `GET`/`DELETE /auth/sessions`, `DELETE /auth/sessions/{ref}`, `GET`/`POST /auth/recovery-codes` aceitam o cookie da web **ou** o Bearer do telemóvel, nunca os dois (401), e passam pelo mesmo `Authenticator` das RPC (utilizador desativado ou sessão revogada = recusado). O Bearer não é uma credencial ambiente, por isso não leva a verificação cross-site; o cookie leva (`Origin`/`Sec-Fetch-Site`: antes destes endpoints não a tinham, é uma correção).
- **Passo-up para gerar códigos:** fator forte usado nos últimos 5 minutos (`StepUpWindow`), como o resto das operações sensíveis (antes só se exigia o fator forte, sem prazo). No telemóvel, o passo-up é **uma nova sessão**: `/auth/mobile/start` com `step_up: true` e o Bearer atual; o `token` exige a **mesma conta** e um fator forte (`user` ou `otp`, senão `weak_factor`), cria a sessão nova (chave nova) e **termina a antiga** (tokens no Zitadel incluídos). Uma recusa deixa a sessão antiga como estava.
- **Lista de sessões** sem identificadores internos (`ref`, tipo, datas, agente, IP, «atual»); terminar uma sessão revoga também o refresh token no Zitadel; «terminar as outras» nunca toca na que pede. Uma referência alheia ou desconhecida dá o mesmo 404. A sessão terminada aqui **não** termina a sessão do navegador do Zitadel (documentado no ecrã).
- **Risco aceite:** um Bearer roubado (10 min, preso à chave do Keystore) pode listar e terminar sessões do dono; não pode gerar códigos sem um fator forte recente. Auditoria: `auth.session_revoked`, `auth.sessions_revoked_others`, `auth.recovery_codes_generated`.
