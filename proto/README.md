# proto

Contratos Protobuf: fonte de verdade da API. Pacotes `pitbox.<módulo>.v1`. O Buf gera código para `backend/gen` (Go + Connect) e `app/src/gen` (TypeScript, `@bufbuild/protoc-gen-es`).

Regras em `.claude/rules/database-and-contracts.md`.

## Fluxo

```bash
make gen        # apaga e regenera o código gerado (vai para o Git; nunca se edita à mão)
make lint       # buf lint, buf format, golangci-lint
make breaking   # buf breaking contra a main
make test       # inclui os testes de contrato (protovalidate)
```

Mudar a API começa sempre no `.proto`. O código gerado commita-se no mesmo PR; o CI (F0-05) falha se `make gen` alterar alguma coisa.

## Convenções

- Campos nunca reutilizam números; remoções com `reserved`. Num `enum`, o valor `0` é sempre `..._UNSPECIFIED`.
- Todo o pedido tem regras protovalidate, com tecto em strings, listas e `page_size` (máx. 200). Os limites de texto contam **caracteres** (como o `length()` do Postgres), não bytes.
- Escritas levam `request_id` (UUIDv7); listas usam cursor opaco (`page_token`).
- O utilizador nunca vai num pedido: vem do principal autenticado.
- Plugins locais e fixados (`mise.toml` e `app/pnpm-lock.yaml`); a dependência do protovalidate fica fixada em `buf.lock`.
- O `go_package` é gerido pelo *managed mode* do `buf.gen.yaml`, que exclui o módulo do protovalidate (o código gerado importa o `go_package` original dele).
