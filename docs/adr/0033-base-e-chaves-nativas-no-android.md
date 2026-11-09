# 0033 — Base e chaves nativas no Android

- **Estado:** Aceite (Nelson, 2026-11-09)
- **Data:** 2026-11-09
- **Cartão:** F2-06 (#49)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0018](0018-base-local-web.md) (estende: motor nativo), [ADR 0025](0025-formato-de-assinatura-e-cursor-do-sync.md) (estende: chave no Keystore), [ADR 0028](0028-app-instalavel-e-arranque-sem-rede.md) (o service worker não corre no nativo); arquitetura §3, §5.1, §7.4; ameaças TM-DEV-01, TM-DEV-03, TM-SYNC-01.

## Contexto

A app web guarda a base em wa-sqlite/OPFS, sem cifra (ADR 0018), e a chave ECDSA do dispositivo em IndexedDB como `CryptoKey` não extraível (ADR 0025). No Android o cartão F2-06 exige a base cifrada com SQLCipher e a chave de assinatura no Keystore, não exportável.

O spike ao código de `@capacitor-community/sqlite` 8.1.1 e `@capacitor/android` 8.6.0 (lido, não executado) mostrou:

1. **O plugin já guarda a passphrase no Keystore.** `setEncryptionSecret(passphrase)` grava-a em `EncryptedSharedPreferences` (chave mestra AES-256-GCM no AndroidKeyStore) e é **global**: uma só passphrase para todas as bases, e falha se já existir uma. Com `mode: "secret"` o plugin vai buscá-la sozinho a cada abertura; o JavaScript não volta a vê-la.
2. **O plugin não aplica nenhum pragma nosso.** Não encontrei `foreign_keys`, `trusted_schema`, `synchronous` nem `quick_check` no código Java: têm de correr a cada abertura, a partir do JavaScript.
3. **Dependências nativas do plugin:** `net.zetetic:sqlcipher-android:4.17.0`, `androidx.security:security-crypto:1.1.0-alpha06` (biblioteca descontinuada pela Google, ainda sem sucessora oficial para este caso) e `androidx.biometric:biometric:1.1.0`.
4. **`minSdk` por omissão do Capacitor 8 e do plugin é 24.** O projeto fixa 29 (Android 10, spec §13).
5. **Nenhum plugin de armazenamento seguro que avaliei** (por exemplo `@aparajita/capacitor-secure-storage`) gera ou assina com chaves do Keystore: guardam segredos que voltam ao JavaScript. Não serve a TM-SYNC-01.

## Decisão

### 1. Dois motores atrás de uma interface

`app/src/db` passa a ter uma interface `LocalDatabase` (`exec`, `query`, `transaction`, `close`, `deleteUserDatabase`, `markLogout`). `WebDatabase` é o código atual (worker wa-sqlite, sem mudanças de comportamento). `NativeDatabase` usa `@capacitor-community/sqlite` com SQLCipher. A escolha é feita num único ponto, por `Capacitor.isNativePlatform()`. As migrações `0001…00NN` e o runner com checksum são os mesmos nos dois.

Em cada abertura nativa: pragmas (`foreign_keys=ON`, `trusted_schema=OFF`, `synchronous=FULL`) e `PRAGMA quick_check`. Se a chave não abrir a base ou o `quick_check` falhar, **não se cria uma base nova em silêncio**: mostra-se um erro sem detalhes internos e a base fica fechada.

### 2. Chave da base: a do plugin, gerada uma vez

- Na primeira execução, a app gera 32 bytes com `crypto.getRandomValues`, codifica-os e chama `setEncryptionSecret` **uma única vez**; o valor não é guardado em mais lado nenhum (nem `localStorage`, nem `Preferences`, nem IndexedDB, nem logs).
- As bases abrem-se com `mode: "secret"`: a passphrase fica só dentro do plugin, protegida pela chave mestra do Keystore.
- **Logout** (depois de a outbox estar sincronizada): marca o utilizador, apaga a base e, **só se já não restar nenhum ficheiro de base** na lista do plugin, chama `clearEncryptionSecret`. A passphrase é global; perdê-la com uma base ainda presente tornaria essa base ilegível, por isso qualquer nome na listagem, seja qual for o formato, a mantém. Uma passphrase sem base não protege nem expõe nada. A marca de logout (para repetir o apagamento se a app morrer a meio) fica em IndexedDB, num ficheiro à parte com exceção ESLint própria; contém só o hash do utilizador, que já faz parte do nome do ficheiro.
- **Resíduo aceite:** a passphrase existe na memória do JavaScript durante a criação. Não existe forma de a evitar com este plugin sem o bifurcar.
- **Risco de dependência:** `security-crypto` está descontinuada. Fica registado no F2-06c (MASVS) e na revisão do Renovate; uma troca futura implica migrar a passphrase.

*Alteração ao plano original:* não crio um segundo «envelope» de chave no plugin próprio. Seria uma segunda cópia da mesma passphrase, mais código nativo e nenhuma proteção a mais.

### 3. Chave do dispositivo do sync: plugin próprio `pitbox-secure` (Java)

*Emenda (F2-06, Fases C e E, 2026-11-09):* as chaves são **por utilizador do telemóvel**: cada chamada leva o `userHash` (64 hex, o mesmo que dá nome à base local) e o alias do Keystore é `pitbox.device-key.v1.<userHash>`, construído a partir de um hash validado e nunca de texto livre. Sem isto, o logout ou a revogação de um utilizador apagaria a chave de outro no mesmo telemóvel. O cliente só vê `DeviceKeys`/`DeviceSigner` ("dá-me a chave pública", "assina estes bytes"), nunca um objeto de chave. Uma chave que não esteja em hardware seguro (`strongbox` ou `tee`) é **recusada**, e o motor não sincroniza em vez de usar uma chave mais fraca; só um build feito de propósito com `VITE_ALLOW_SOFTWARE_KEYSTORE=1` (emulador) a aceita.

*Emenda anterior (Fase C):* o plugin é escrito em **Java**, não em Kotlin. O projeto Android do Capacitor 8 e todos os plugins que usamos são Java e não têm o plugin Gradle de Kotlin; acrescentá-lo era mais uma peça na cadeia de build (F2-06c) sem ganho de segurança. A conversão DER → P1363 passou do nativo para o TypeScript (`app/src/sync/ecdsa-der.ts`), onde tem testes com assinaturas reais e propriedades que correm no CI sem Android.


- `deviceKey.ensure()`: gera um par P-256 no AndroidKeyStore (`setIsStrongBoxBacked(true)` quando houver StrongBox, senão TEE) e devolve **só a SPKI DER** (91 bytes).
- `deviceKey.sign({ message })`: ECDSA-SHA256 no Keystore; o plugin devolve a assinatura **em DER**, tal como o Keystore a produz, e o cliente converte-a para P1363 (r‖s, 64 bytes, o formato do ADR 0025) com um parser estrito de DER canónico (rejeita formas longas, bytes a mais, inteiros negativos, não mínimos ou nulos, e mais de 32 bytes). Mensagem entre 1 byte e 256 KiB, em base64 estrito.
- `deviceKey.delete()`.
- Nenhum método devolve a chave privada, e o par não tem `setUserAuthenticationRequired` (a sincronização em segundo plano não pode pedir biometria).
- A interface `KeyStore` do cliente deixa de assentar em `CryptoKeyPair`: passa a `publicKey()`, `sign(bytes)` e `forget()`. `WebKeyStore` mantém o comportamento atual; `NativeKeyStore` chama o plugin. O cliente envia `platform: "android"`. **O servidor não muda** (já aceita `android` e P1363).

### 4. Biometria

O mesmo plugin expõe `biometric.authenticate()` com `androidx.biometric` (`BIOMETRIC_STRONG` ou credencial do dispositivo). A app pede-a depois de 15 minutos em segundo plano (spec §3.4). Cancelar ou falhar mantém a vista bloqueada; **o bloqueio é de interface e não cifra a base** (a cifra é a do ponto 2). Não se usa o `biometricAuth` do plugin SQLite, que serve outro fim (proteger a criação da chave mestra).

### 5. Projeto Android

- `appId` fixo, `minSdk` 29, **sem `server.url`**; só ficheiros do bundle. CSP revista para a origem `https://localhost`, sem `unsafe-inline`.
- Manifest: `allowBackup="false"` com regras de extração que excluem tudo; `usesCleartextTraffic=false` **também no debug** (a tailnet do ADR 0029 já é HTTPS por `tailscale serve`, não é preciso exceção); só as permissões `POST_NOTIFICATIONS`, `VIBRATE` e `USE_BIOMETRIC`; componentes com `exported` explícito.
- O service worker não se regista dentro do Capacitor: o build nativo (`vite build --mode android`) não gera `sw.js` e `UpdatePrompt` não monta `useRegisterSW` no nativo. A CSP vai numa `<meta>` injetada nesse build, com a origem da API (só `https`, sem caminho) vinda de `PITBOX_API_ORIGIN`.
- APK de debug com a chave de debug fora do Git. Sem Play Store.
- Teste de sync no F2-06: contra a API real com uma sessão de teste só no build de debug. O login real é do F2-06b.

## Alternativas

- **Plugin de armazenamento seguro de terceiros para a chave de assinatura.** Rejeitada: a chave passa pelo JavaScript e o critério «não exportável» falha.
- **Envelope de chave próprio para a SQLCipher (AES-GCM no Keystore).** Rejeitada: duplica a custódia que o plugin já faz.
- **Bifurcar `@capacitor-community/sqlite` para a chave nunca passar pelo JavaScript.** Rejeitada para já: custo de manutenção alto para um resíduo pequeno; reavaliar no F2-06c.
- **Ed25519 no Keystore.** Rejeitada no ADR 0025 (suporte universal só para P-256).
- **SQLCipher também na web.** Fora de âmbito (ADR 0018).
- **Biometria com plugin de comunidade (`@capgo/capacitor-native-biometric`, MPL-2.0).** Rejeitada: acrescenta uma dependência e superfície quando o plugin próprio já existe.

## Consequências

- TM-DEV-01 passa a **implementado no nativo** (base cifrada em repouso, chave no Keystore); TM-DEV-03 e TM-SYNC-01 passam a implementados com a prova do cartão. A web continua sem cifra (ADR 0018).
- Fica código Java próprio a manter e a rever (~300 linhas em três classes). **Compilado e executado num telemóvel real em 2026-11-09** (Samsung SM-S911B, Android 17): a chave nasce em StrongBox, a assinatura verifica-se com a chave pública, a base abre cifrada, migra, sobrevive ao fecho e apaga-se. Os testes instrumentados no emulador continuam por fazer.
- Confirmado no telemóvel, com correção: (1) uma base acabada de criar é um ficheiro vazio e o plugin dá-a como **não cifrada**; escreve-se o cabeçalho antes de perguntar (`PRAGMA user_version = 1`); (2) `deleteDatabase` só funciona através de uma ligação registada; (3) `getDatabaseList` com a pasta vazia lança «No databases available» em vez de devolver lista vazia (só essa mensagem se trata como vazia; qualquer outro erro propaga, para a passphrase nunca ser largada por engano). Transações (as migrações usam-nas) e parâmetros nomeados confirmados.
- Ainda por confirmar: `RETURNING` no plugin, várias bases por utilizador em simultâneo, o ramo Android 10 do `BiometricPrompt` (`setDeviceCredentialAllowed`; o telemóvel de teste é API 37), o `securityLevel` num emulador, e o ecrã de bloqueio numa sessão real (precisa do login, F2-06b). O apagamento do logout não consegue apagar uma base que não abra (chave perdida): fica por tratar.
- A migração local corre em dois motores; a mesma bateria de testes tem de passar nos dois, e a divergência passa a ser um risco gerido pela interface única.
- Dependências novas a justificar no PR: `@capacitor/core|cli|android`, `haptics`, `local-notifications`, `@capacitor-community/keep-awake`, `@capacitor-community/sqlite`, versões fixas.
- Os documentos que ainda dizem «Bearer e CORS no F2-06» (ADR 0004, 0013, 0017; arquitetura §7.4) são atualizados para F2-06b.

💼 Portfólio: «base cifrada igual em web e Android» e «chave de assinatura não exportável no Keystore» cobrem MASVS-STORAGE e MASVS-CRYPTO; bom artigo sobre assinatura de dispositivo num protocolo offline-first.
