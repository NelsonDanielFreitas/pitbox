---
paths:
  - "firmware/**/*"
---

# Regras do firmware (Rust, ESP32-S3)

## Segurança do carro (prioridade máxima)
- O módulo só lê sensores. Nunca está no caminho do sinal do recetor para os servos nem partilha linhas de sinal com eles.
- Uma avaria, bloqueio ou reinício do módulo não pode afetar o controlo do carro. Watchdog de hardware sempre ativo.

## Código
- `#![no_std]`, `#![deny(unsafe_code)]` por omissão. Qualquer `unsafe` tem de ter um comentário `// SAFETY:` que explique a invariante.
- `esp-hal` + Embassy: uma tarefa por sensor, comunicação entre tarefas com `embassy-sync` (canais), sem alocação dinâmica no caminho de aquisição (`heapless`).
- O crate `protocol` é `no_std`, partilhado entre `car-module`, `box-receiver` e o leitor em Go; mudanças ao formato incrementam a versão do formato.
- Formato no cartão: blocos de 4 KiB com cabeçalho, versão e CRC; nunca perder um bloco inteiro por uma escrita interrompida.
- Explica-me ownership, lifetimes e o modelo async do Embassy quando aparecerem pela primeira vez.

## Segurança do dispositivo
- Secure boot v2 e cifra da flash ativos nas builds de produção; chaves nunca no repositório.
- Firmware assinado (Ed25519) e verificado antes de instalar; anti-rollback por contador de segurança.
- Emparelhamento com código de uso único da app; certificado/chave por dispositivo.
- LoRa: AEAD (ChaCha20-Poly1305) com chave de sessão; número de sequência no nonce; rejeitar repetições e pacotes fora de ordem.
- Respeitar os limites da banda 868 MHz na UE (sub-banda, potência, duty cycle). Não emitir a 2,4 GHz durante sessões.

## Qualidade
- `cargo fmt`, `cargo clippy -- -D warnings`, `cargo audit`, `cargo deny check`.
- Testes do `protocol` no host (`cargo test -p protocol`); testes de ida e volta com o leitor em Go.
- Toolchain fixa em `firmware/rust-toolchain.toml` (canal `esp`, instalado com `espup`).
