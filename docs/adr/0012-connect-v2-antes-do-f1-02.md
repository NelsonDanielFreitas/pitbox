# 0012 — Connect v2 antes do F1-02

- **Estado:** Aceite (Nelson, 2026-10-12)
- **Data:** 2026-10-12
- **Cartão:** F1-01b
- **Relacionado:** [ADR 0002](0002-connect-protobuf.md) (Connect sobre Protobuf), que continua válido: muda a versão da biblioteca, não a decisão.

## Contexto

O ADR 0002 escolheu o Connect (`connect-go`). A **v2.0.0** foi publicada a 2026-10-07 (módulo `connectrpc.com/connect/v2`, Apache-2.0); a v1 continua suportada (o ramo v1 recebe correções, segundo o guia de migração da própria v2). O repositório só tem código gerado e um teste de contrato: ainda não há handlers, clientes nem interceptors. O F1-02 escreve todos os interceptors da arquitetura §4.4, e a v2 muda a forma de os escrever (a v1 teria de ser reescrita depois).

Verificado nas fontes dos módulos (12/10/2026):

| Componente | Versão com suporte v2 | Nota |
| --- | --- | --- |
| `connectrpc.com/connect/v2` | v2.0.0 | núcleo; HTTP em `connecthttp`, codecs em `connectproto` |
| `connectrpc.com/validate` | v0.9.0 | `NewServerInterceptor`/`NewClientInterceptor`; o módulo **não** tem caminho `/v2` |
| `connectrpc.com/otelconnect` | v0.12.0 | `NewServerInterceptor`/`NewClientInterceptor` (devolvem erro) |
| `connectrpc.com/grpchealth/v2` | v2.0.0 | `Register(server, checker)` |
| `connectrpc.com/grpcreflect/v2` | v2.0.0 | `Register(server)` |
| `protoc-gen-connect-go` | v2.0.0 (`connectrpc.com/connect/v2/cmd/protoc-gen-connect-go`) | o nome mantém-se; só muda o binário |

Numa cópia descartável, a migração do código existente compilou e passou os testes (`go test -race`, `govulncheck` limpo).

## Decisão

Migrar já para `connectrpc.com/connect/v2` v2.0.0, antes do F1-02:

- Gerador fixado em `mise.toml` pelo caminho explícito `go:connectrpc.com/connect/v2/cmd/protoc-gen-connect-go` (o nome curto do registo do mise aponta para a v1). Versão do gerador = versão da biblioteca: `make gen-check` verifica-o (`scripts/check-tool-versions.py`).
- O código gerado é transport-agnóstico (não importa `net/http`); os handlers montam-se com `connect.NewServer(interceptors...)` e `connecthttp.Mount`.
- Os interceptors (F1-02) usam `connect.ServerInterceptor`: `validate.NewServerInterceptor` e `otelconnect.NewServerInterceptor` das versões acima. Saúde e reflection com `grpchealth/v2` e `grpcreflect/v2` (reflection só em desenvolvimento).

**Regras de segurança adotadas com a v2:**

1. **Erros.** Um `error` normal devolvido por um handler nunca é serializado para o cliente na v2; só um `*connect.Error` mostra a sua mensagem. Escreve-se `connect.NewError(code, "mensagem pública fixa").WithCause(err)`: o detalhe fica no servidor (log) e o cliente só vê a mensagem fixa. A ferramenta oficial de migração converte `NewError(code, err)` em `NewError(code, err.Error())` para preservar o comportamento v1, o que **reintroduziria o vazamento de erros internos**: não se usa esse padrão (`err.Error()` como mensagem pública é proibido; ver `.claude/rules/go-backend.md`).
2. **Limites de leitura.** A v2 tem um limite por omissão de 4 MiB por mensagem (a v1 era ilimitada). O limite fica sempre explícito (`connecthttp.WithReadMaxBytes`): 4 MiB na API e 32 MiB só no `pitbox-ingest`. A ferramenta de migração escreve `WithReadMaxBytes(0)` (ilimitado) para preservar a v1: proibido.
3. **Interceptors à volta da chamada inteira.** Na v2 a cadeia de interceptors rodeia toda a chamada, o que permite rejeitar por cabeçalhos antes do trabalho de descompressão e descodificação do corpo (o guia refere os problemas #584 e #620 do v1). A ordem da §4.4 mantém-se.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Ficar na v1 | Suportada, mas o F1-02 escreveria interceptors com a interface antiga e migrá-los depois seria fazer o trabalho duas vezes, com mais código a migrar |
| Esperar pela v2.0.1 | A v2 tem poucos dias de vida; a migração agora custa quase nada (sem handlers) e os pacotes do ecossistema já têm versões etiquetadas. Se surgir uma correção, o Renovate propõe-a |
| Migração com a ferramenta `connect-go-v2-migrate` | Não vem no módulo v2.0.0 e não é precisa: só há código gerado, que o gerador refaz. A ferramenta fica registada para a revisão dos seus avisos se um dia for usada |

## Consequências

- `go.mod` passa de `connectrpc.com/connect` para `connectrpc.com/connect/v2`; o código gerado é regenerado (`make gen`).
- O módulo `connectrpc.com/otelconnect` v0.12.0 e `validate` v0.9.0 trazem as duas versões do Connect no grafo de módulos (compatibilidade); só a v2 é usada.
- O `authn` do ecossistema continua a ser middleware HTTP (v1, sem alterações); a autenticação do F1-03 decide entre middleware e interceptor.
- Renovate: as dependências `connectrpc.com/**` sobem no mesmo grupo e as versões *major* exigem aprovação.
- As duas regras de segurança têm teste (F1-02): erro interno só com código, corpo acima de 4 MiB recusado, e `internal/archtest`. O `forbidigo` não serve para a regra dos erros: só compara o nome qualificado da chamada, não os argumentos (verificado); a regra é uma lista de permitidos sobre a árvore sintática (literal ou constante), que também apanha `msg := err.Error()` e `fmt.Sprintf`.
- Frontend: o cliente TypeScript (`@connectrpc/connect-web`, F1-09) é independente do `connect-go` e não é afetado.
- Reavaliar se o ecossistema (validate, otelconnect) deixar de acompanhar a v2 ou se a v2 mostrar regressões nas primeiras correções.
