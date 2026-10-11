# firmware

Workspace Rust (Cargo) para o ESP32-S3. Toolchain fixa em `rust-toolchain.toml` (canal `esp`, instalado com `espup`).

| Crate | Conteúdo |
| --- | --- |
| `protocol/` | `no_std`: formato dos blocos no cartão (com CRC) e do pacote LoRa; partilhado pelos outros crates e espelhado no leitor em Go |
| `car-module/` | Firmware do módulo no carro: sensores, gravação em cartão, LoRa, BLE |
| `box-receiver/` | Firmware do recetor na box: LoRa → BLE para a app |
| `tools/` | Ferramentas de host (ex.: leitor de ficheiros do cartão, assinatura de firmware) |
| `hardware/` | Esquemas, lista de material (BOM), fotos da montagem, desenho da caixa |

Regras em `.claude/rules/firmware-rust.md`.
