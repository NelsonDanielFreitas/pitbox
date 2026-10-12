# 0002 — Connect sobre Protobuf como API única

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §1, §4.3
- **Atualizado por:** [ADR 0012](0012-connect-v2-antes-do-f1-02.md): a decisão mantém-se; a biblioteca passa a ser `connectrpc.com/connect/v2`

## Contexto

A web, o Android, o iOS, o firmware (esquemas) e a comunicação interna precisam do mesmo contrato. A app web tem de falar com o backend sem proxy extra.

## Decisão

Protobuf + Buf como fonte de verdade (`proto/pitbox/<módulo>/v1`), com Connect (`connect-go`), que fala gRPC, gRPC-Web e HTTP/JSON com o mesmo código. Contratos primeiro: mudar a API começa no `.proto`; `buf breaking` tem de passar ou a mudança vai para `v2`. Pedidos validados com protovalidate; escritas com `request_id` idempotente; listas com paginação por cursor. O código gerado é commitado e nunca editado à mão (`make gen-check`).

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| gRPC puro + Envoy para gRPC-Web | Mais uma peça a operar só para o browser |

## Consequências

- A validação de entrada vive no contrato; regras que o protovalidate não cobre (por exemplo versão do UUID, lista ISO de países) ficam no domínio.
- O reflection fica desligado em produção (arquitetura §12.1).
- Um contrato mal desenhado custa uma `v2`: por isso o F0-04 define os contratos antes do código.
