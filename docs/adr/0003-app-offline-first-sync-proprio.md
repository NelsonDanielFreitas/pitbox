# 0003 — App offline-first com SQLite local e protocolo de sync próprio

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §3, §5.2, §8

## Contexto

A box nunca pode depender da rede: o modo final tem de funcionar 100% offline. A app lê sempre da base local.

## Decisão

PWA + Capacitor com SQLite local (wa-sqlite na web, SQLCipher no telemóvel) e um protocolo de sync próprio sobre o `SyncService` (`Push`, `Pull`, `IssueOfflinePass`):

- Operações com `op_id` (UUIDv7) idempotente e `base_version`; registos de acrescento nunca entram em conflito; fichas editáveis juntam campo a campo e geram `conflict` quando o mesmo campo muda dos dois lados.
- Cada operação é assinada pela chave do dispositivo (ECDSA P-256, gerada no armazenamento seguro); o servidor valida a assinatura, o passe e as permissões atuais.
- Passe offline de 72 h (token assinado pelo servidor com `team_id`, `device_id`, papel); dispositivo revogado vê as suas operações rejeitadas e enviadas para revisão do dono.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Motor de sync genérico | A autorização teria de ser replicada fora do servidor; o controlo total no servidor é um requisito de segurança |

## Consequências

- Maior superfície a testar (conflitos, idempotência, relógios): o `SyncService` é a área com mais testes e é protegida por CODEOWNERS.
- Um dispositivo perdido ou roubado guarda dados locais cifrados e um passe de até 72 h: a revogação e a expiração são a mitigação (ver threat model).
- O passe de 72 h é um compromisso entre usabilidade em provas de fim de semana e a janela de abuso.
