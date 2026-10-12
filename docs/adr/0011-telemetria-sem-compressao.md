# 0011 — Telemetria sem compressão enquanto houver RLS

- **Estado:** Aceite
- **Data:** 2026-10-12
- **Cartão:** F0-06

## Contexto

A arquitetura (§7.1) previa compressão de `telemetry.sample` aos 7 dias. Com a TimescaleDB 2.30.2 (imagem do compose) o `ALTER TABLE ... SET (timescaledb.compress)` falha: *"columnstore cannot be used on table with row security"* (a documentação de compressão diz o mesmo: RLS não é suportado em chunks comprimidos). A regra de arquitetura nº 5 exige RLS em toda a tabela de equipa, e `telemetry.sample` é a tabela onde um erro de `team_id` na ingestão expõe mais dados.

## Decisão

- Manter o **RLS** em `telemetry.sample`; **retirar a compressão**. Mantêm-se a hypertable (chunks de 1 dia) e a retenção de **24 meses**.
- A TimescaleDB passa a ser **obrigatória** (extensão do bootstrap; sem o `IF EXISTS` do schema): a mesma migração não pode produzir bases diferentes consoante a imagem.
- `pitbox_owner` tem `LOGIN` **sem palavra-passe**: a TimescaleDB só corre as políticas (retenção) se o dono da hypertable o tiver ("permission denied to start background process"). Sem palavra-passe nenhuma ligação por TCP o autentica; `default_privileges_test.sql` falha se alguém lhe puser uma.

## Consequências

- Custo (estimativa a confirmar com dados reais): IMU a 200 Hz, ~2 h de pista por fim de semana ≈ 1,4 M linhas ≈ 0,3 GB; ~7 GB/ano e ~15 GB em 24 meses por equipa. Aceitável para uma equipa.
- Gatilho de revisão: quando `telemetry.sample` passar de ~20 GB, reduzir a retenção da tabela (os ficheiros brutos ficam 24 meses no armazenamento de objetos e reprocessam-se a partir deles) ou rever esta decisão.
- **Agregações contínuas** (§7.1, por volta e por segundo): são vistas materializadas e o Postgres não aplica RLS a vistas materializadas. Quando chegarem (F4), o acesso tem de ser por vista `security_barrier` ou função que filtra por `app.current_team_id()`, com teste "outra equipa" próprio.
- Arquitetura §7.1 atualizada (sem compressão). Alternativa rejeitada: tirar o RLS só desta tabela (abre uma exceção à regra nº 5).
