# Runbook — a base local do Android está cifrada (prova manual)

Cartão F2-06, ADR 0033, TM-DEV-01. Mostra que o ficheiro da base copiado do telemóvel **não** se lê sem a chave, e que o Android não o leva num backup. Faz-se depois de qualquer mudança em `app/src/db/` ou no plugin SQLite.

## O que se prova

1. O ficheiro copiado não começa por `SQLite format 3`.
2. Um texto conhecido (a *canary* `PITBOX-CANARY-7f3a91c2`) e nomes de tabelas não aparecem no ficheiro.
3. O pacote não permite backup do Android (`allowBackup=false`).

## Variante A — telemóvel com a app de debug (sem root)

A app de debug é `debuggable`, por isso `adb shell run-as` abre a pasta privada da app. (Numa build de release isto não funciona, que é o objetivo.)

```bash
ADB=~/Android/Sdk/platform-tools/adb
# 1. Construir com `make android-diagnostics`, instalar, e no telemóvel (página de diagnóstico) e tocar "Deixar base de teste (adb)".
# 2. Copiar o ficheiro e inspecionar:
F=$($ADB shell run-as app.pitbox.mobile ls databases | grep -E "SQLite.db$" | head -1 | tr -d '\r')
$ADB exec-out run-as app.pitbox.mobile cat databases/$F > /tmp/copia.db
head -c 16 /tmp/copia.db | xxd                                   # bytes aleatórios; nunca "SQLite format 3"
grep -c -a -E "PITBOX-CANARY|schema_migrations|diag_canary" /tmp/copia.db   # tem de dar 0
$ADB shell dumpsys package app.pitbox.mobile | grep -ci ALLOW_BACKUP        # tem de dar 0
rm /tmp/copia.db
```

A base de teste apaga-se sozinha na próxima abertura da página de diagnóstico.

**Resultado de 2026-11-09** (Samsung SM-S911B, Android 17, ficheiro de 352 256 bytes): primeiros bytes `c3a2 fcbf 66e3 f9dd b213 d630 721b 3fac`; canary, `schema_migrations` e `CREATE TABLE` não aparecem (0 ocorrências); 256 valores de byte distintos nos primeiros 4096 (aspeto de dados cifrados); sem `ALLOW_BACKUP` nas flags do pacote.

## Variante B — emulador com root (a fazer)

Prova o mesmo contra um atacante com root, que também lê a pasta da app mas não a chave: a passphrase está no `SharedPreferences` do plugin, cifrada pela chave do Keystore, que não sai do hardware.

1. Emulador AVD **sem** Google Play (permite `adb root`), mesma app de debug.
2. `adb root`, deixar a base de teste como na variante A e copiar `/data/data/app.pitbox.mobile/databases/*.db` e a pasta `shared_prefs/`.
3. Abrir a cópia com `sqlcipher` (ou DB Browser for SQLite com SQLCipher) sem chave: tem de falhar («file is not a database»).
4. Abrir `shared_prefs/`: o valor da passphrase tem de estar cifrado (não é texto legível) e não serve sem o Keystore.

## Quando falha

- Aparece `SQLite format 3` ou a canary: a base está em claro. **Parar**: a `NativeDatabase` devia ter recusado abri-la (`local database is not encrypted`). Verificar `androidIsEncryption` em `capacitor.config.ts` e o modo `secret` no driver.
- `ALLOW_BACKUP` presente: rever `allowBackup`, `fullBackupContent` e `dataExtractionRules` no `AndroidManifest.xml`.
