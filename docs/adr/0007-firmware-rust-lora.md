# 0007 — Firmware em Rust com Embassy; LoRa 868 MHz para tempo real

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §9; contexto §3 e §8

## Contexto

O módulo no carro grava telemetria em cartão e envia um resumo em tempo real para a box. O firmware vai para o terreno durante anos e sem rede de correção rápida; o rádio está em banda regulada.

## Decisão

- **Firmware em Rust** (`esp-hal` + Embassy, async, sem SO) em ESP32-S3, `postcard` para serialização, secure boot v2 e cifra da flash ativos, anti-rollback por contador, OTA verificada por assinatura.
- **LoRa 868 MHz (SX1262)** para o resumo a 1 Hz (~24 B), autenticado e cifrado com ChaCha20-Poly1305; o número de sequência faz parte do nonce e o recetor rejeita repetições. O módulo **só lê sensores**: nunca está no caminho do sinal de controlo do carro.
- **Assinatura do firmware:** Ed25519 (arquitetura §9.2).

## Agilidade criptográfica: migração futura para ML-DSA

- **Contexto:** Ed25519 e as assinaturas usadas no arranque seguro (o secure boot v2 do ESP32-S3 usa RSA ou ECDSA: **confirmar qual na documentação da Espressif**) são quebráveis por um computador quântico relevante. A assinatura de firmware é das primeiras coisas a migrar, porque o hardware fica no terreno anos.
- **Decisão:** o formato do pacote de firmware leva desde a primeira versão um **identificador de algoritmo** e admite **mais do que uma assinatura** (híbrida Ed25519 + ML-DSA, FIPS 204), para a migração não exigir um formato novo. A chave de verificação do módulo é atualizável por OTA assinada pela chave antiga. Data da migração: a decidir.
- **Consequências (valores a confirmar no F4):** assinaturas ML-DSA-44 de ~2,4 KB e chaves públicas de ~1,3 KB cabem na OTA por BLE, mas não num pacote LoRa (e não precisam: o LoRa usa AEAD simétrico, e ChaCha20-Poly1305 com chave de 256 bits resiste bem ao Grover). O que falta proteger é a **derivação da chave de sessão no emparelhamento** (§5.3): se for ECDH, é o ponto a migrar para ML-KEM (FIPS 203) ou para um esquema híbrido.

## Risco conhecido: tempo de antena

A §9.3 diz que 1 Hz em SF7 "mantém-se bem abaixo" dos 10% de duty cycle. Com a fórmula de tempo de antena da Semtech (SF7, 125 kHz, CR 4/5, preâmbulo de 8, cabeçalho explícito, CRC):

| Carga útil no ar | Tempo por pacote | Duty cycle a 1 Hz |
| --- | --- | --- |
| 24 B (só o resumo, sem cripto) | 61,7 ms | 6,2% |
| 44 B (24 B + tag Poly1305 de 16 B + sequência de 4 B) | 92,4 ms | 9,2% |
| 48 B (+ identificador do módulo) | 97,5 ms | 9,8% |

Com a autenticação exigida, fica **à beira dos 10%**, sem margem para retransmissões nem para mais um campo. Opções (a decidir no F5-02, com medição real): largura de banda de 250 kHz (metade do tempo: 30,8 / 46,2 / 48,8 ms; confirmar que é permitido na sub-banda), 0,5 Hz, ou compactar o resumo. **Truncar a tag de autenticação não é opção.**

## Questões em aberto

| Questão | O que se sabe | Fechar em |
| --- | --- | --- |
| Limites da sub-banda 869,4–869,65 MHz | A confirmar: 500 mW e.r.p. e 10% de duty cycle (ERC/REC 70-03; ETSI EN 300 220; QNAF da ANACOM) | F5-01 |
| Tempo de antena com o pacote autenticado | 9,2–9,8% a 1 Hz em SF7/125 kHz (acima) | F5-02 |
| O regulamento das provas permite telemetria a bordo? | Em aberto (contexto §8) | Antes do F4 (compra do hardware) |
| Obrigações se o módulo for distribuído | Uso próprio: não se aplica. Se for vendido ou distribuído: Cyber Resilience Act (reporte desde 11/09/2026; requisitos completos a 11/12/2027) e, por ser equipamento de rádio, a Diretiva RED com o Regulamento Delegado (UE) 2022/30 (cibersegurança, aplicável desde 1/08/2025; normas EN 18031). **A confirmar** | Antes de qualquer distribuição |

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| C++ com ESP-IDF | Sem segurança de memória no código que vai para o terreno |

## Consequências

- O módulo funciona sem rede e sem a box (grava sempre no cartão); o rádio é um extra.
- Um módulo roubado ou adulterado é tratado no threat model (secure boot, flash cifrada, certificados por módulo com revogação).
