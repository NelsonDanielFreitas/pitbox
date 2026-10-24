# Runbook — rotação das chaves de autenticação

Aplica-se ao anel `PITBOX_AUTH_KEYS` (ADR 0013): cookies de pré-login, tokens de sessão e códigos de recuperação.

## Antes de retirar uma chave

O anel é ordenado: a primeira chave sela os valores novos; todas as chaves abrem ou verificam valores antigos. Uma chave não pode sair enquanto existirem:

- sessões seladas com ela;
- códigos de recuperação não usados com `key_id` igual ao `kid` dessa chave.

### Códigos de recuperação

Consultar, com o papel de migração (não com `pitbox_app`):

```sql
SELECT key_id, count(*) AS unused
FROM identity.recovery_code
WHERE used_at IS NULL
GROUP BY key_id
ORDER BY key_id;
```

Antes de remover o `kid` `kX`, o resultado tem de ser zero para `kX`.

O valor `legacy` identifica códigos anteriores ao HMAC. Já não verificam e não correspondem a nenhuma chave do anel; devem ser regenerados pelo utilizador.

Se houver códigos por usar:

1. manter a chave no anel;
2. avisar os utilizadores afetados para regenerarem os códigos;
3. repetir a consulta;
4. só depois retirar a chave.

### Sessões

As sessões guardadas no Valkey estão seladas com a chave ativa no momento da criação. A chave antiga tem de permanecer até essas sessões expirarem (`PITBOX_SESSION_ABSOLUTE_TIMEOUT`). O mesmo se aplica aos cookies de pré-login, que têm vida curta.

## Procedimento

1. Acrescentar a chave nova à frente de `PITBOX_AUTH_KEYS` (novo `kid`) e reiniciar o `pitbox-api`.
2. Confirmar que os logins novos usam o novo `kid` e que os antigos continuam a funcionar.
3. Esperar pela expiração das sessões seladas com a chave antiga.
4. Executar a consulta de códigos de recuperação e garantir que a chave antiga já não tem códigos por usar.
5. Retirar a chave antiga e reiniciar.
6. Verificar login, logout, refresh e consumo de um código de recuperação de teste.

## Chave comprometida

Se uma chave mestra for comprometida, o objetivo deixa de ser esperar pela expiração: é fechar já as sessões seladas com ela.

1. Acrescentar uma chave nova à frente do anel e **retirar imediatamente** a chave comprometida.
2. Reiniciar o `pitbox-api`. As sessões e cookies de pré-login selados com a chave antiga deixam de abrir; os utilizadores voltam a autenticar-se, que é o efeito desejado.
3. Os códigos de recuperação com esse `key_id` deixam de verificar. A consulta acima lista os utilizadores afetados; devem regenerar os códigos depois do próximo login com fator forte.
4. Uma fuga da chave HMAC **não** torna os códigos quebráveis: têm 128 bits e a chave é uma camada adicional. Retirar a chave serve para fechar as sessões, não para proteger os códigos.

## Notas

- Nunca colocar chaves, cookies, códigos ou hashes em logs, tickets ou mensagens.
- Sem `key_id` não seria possível saber que chaves antigas ainda tinham códigos por usar; esse é o motivo da coluna.
- O rate limiting de `/auth/*` não está no servidor; fica para o F5-05. Os códigos têm 128 bits, por isso a força bruta online não é viável, mas o abuso/DoS continua possível.
