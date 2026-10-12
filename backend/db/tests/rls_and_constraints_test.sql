-- Testes do esquema: isolamento entre equipas, FKs compostas, imutabilidade, triggers e vistas.
-- Correr numa base de dados descartável:  psql -v ON_ERROR_STOP=1 -f tests/rls_and_constraints_test.sql
\set QUIET on
BEGIN;
SET ROLE pitbox_app;

-- ---------- Equipa A ----------
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000a1';
INSERT INTO identity.user_account (id, idp_subject, email, display_name)
VALUES (app.current_user_id(), 'sub-a', 'mecanico@a.pt', 'Mecânico A');
INSERT INTO identity.team (id, name, created_by) VALUES (app.current_team_id(), 'Equipa A', app.current_user_id());
INSERT INTO identity.membership (team_id, user_id, role) VALUES (app.current_team_id(), app.current_user_id(), 'owner');
INSERT INTO garage.car (id, team_id, make, model, transponder_number)
VALUES ('01900000-0000-7000-8000-0000000000c1', app.current_team_id(), 'Mugen', 'MBX8R', '1234567');
INSERT INTO garage.engine (id, team_id, make, model, tanks_total)
VALUES ('01900000-0000-7000-8000-0000000000e1', app.current_team_id(), 'REDS', 'R7', 40);
INSERT INTO events.track (id, team_id, name) VALUES ('01900000-0000-7000-8000-0000000000f1', app.current_team_id(), 'Pista de teste');
INSERT INTO setup.setup (id, team_id, car_id, name)
VALUES ('01900000-0000-7000-8000-0000000000d1', app.current_team_id(), '01900000-0000-7000-8000-0000000000c1', 'Base terra seca');
INSERT INTO setup.setup_version (team_id, setup_id, version, data, schema_version)
VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000d1', 1,
        '{"diffs":{"front_cst":7000,"center_cst":10000,"rear_cst":3000},"transmission":{"clutch_bell_teeth":13,"spur_teeth":46}}', 1);
INSERT INTO events.session (id, team_id, track_id, kind, label, car_id, engine_id, setup_id, setup_version, status, started_at, ended_at)
VALUES ('01900000-0000-7000-8000-0000000000b1', app.current_team_id(), '01900000-0000-7000-8000-0000000000f1', 'final', 'Final A',
        '01900000-0000-7000-8000-0000000000c1', '01900000-0000-7000-8000-0000000000e1',
        '01900000-0000-7000-8000-0000000000d1', 1, 'finished', now() - interval '30 min', now());
INSERT INTO events.lap (team_id, session_id, lap_no, time_ms, source)
SELECT app.current_team_id(), '01900000-0000-7000-8000-0000000000b1', g, 41000 + (g % 5) * 300, 'import'
FROM generate_series(1, 20) g;
INSERT INTO events.pit_stop (team_id, session_id, seq, actions, lap_no, in_at)
VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000b1', 1, '{refuel}', 9, now() - interval '20 min');
INSERT INTO maintenance.rule (team_id, name, target_kind, metric, threshold, action)
VALUES (app.current_team_id(), 'Rolamentos do motor', 'engine', 'tanks', 30, 'Trocar rolamentos');

-- Colunas geradas a partir do JSONB
DO $$ BEGIN
  ASSERT (SELECT diff_rear_cst FROM setup.setup_version WHERE version = 1) = 3000, 'coluna gerada diff_rear_cst';
  ASSERT (SELECT version FROM garage.car) = 1, 'version inicial';
END $$;
UPDATE garage.car SET nickname = 'Carro 1';
DO $$ BEGIN ASSERT (SELECT version FROM garage.car) = 2, 'touch() incrementa version'; END $$;

-- pit_stop: fechar uma vez é permitido; mudar outra coluna não
UPDATE events.pit_stop SET out_at = in_at + interval '6 seconds';
DO $$ BEGIN ASSERT (SELECT duration_ms FROM events.pit_stop) = 6000, 'duração calculada'; END $$;
DO $$ BEGIN
  BEGIN UPDATE events.pit_stop SET lap_no = 10; RAISE EXCEPTION 'FALHOU: pit_stop alterado';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;

-- Versões de setup são imutáveis
DO $$ BEGIN
  BEGIN UPDATE setup.setup_version SET schema_version = 2; RAISE EXCEPTION 'FALHOU: setup_version alterada';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;

-- Um motor não pode estar em dois sítios ao mesmo tempo
INSERT INTO garage.car (id, team_id, make, model) VALUES ('01900000-0000-7000-8000-0000000000c2', app.current_team_id(), 'Xray', 'XB8');
INSERT INTO garage.engine_mount (team_id, engine_id, car_id, mounted_at)
VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000e1', '01900000-0000-7000-8000-0000000000c1', now() - interval '10 days');
DO $$ BEGIN
  BEGIN
    INSERT INTO garage.engine_mount (team_id, engine_id, car_id, mounted_at)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000e1', '01900000-0000-7000-8000-0000000000c2', now());
    RAISE EXCEPTION 'FALHOU: motor montado em dois carros';
  EXCEPTION WHEN exclusion_violation THEN NULL; END;
END $$;

-- Vistas
DO $$ BEGIN
  ASSERT (SELECT best_lap_ms FROM events.session_lap_stats) = 41000, 'melhor volta';
  ASSERT (SELECT used_since_last FROM maintenance.engine_rule_status) = 40, 'uso desde a última revisão';
  ASSERT (SELECT count(*) FROM sync.change_log WHERE entity = 'garage.car') >= 3, 'change log';
END $$;

-- ---------- Equipa B ----------
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000b';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000b9';
INSERT INTO identity.user_account (id, idp_subject, email, display_name)
VALUES (app.current_user_id(), 'sub-b', 'rival@b.pt', 'Rival B');
INSERT INTO identity.team (id, name, created_by) VALUES (app.current_team_id(), 'Equipa B', app.current_user_id());
INSERT INTO identity.membership (team_id, user_id, role) VALUES (app.current_team_id(), app.current_user_id(), 'owner');

DO $$ BEGIN
  ASSERT (SELECT count(*) FROM garage.car) = 0,           'RLS: B não vê carros de A';
  ASSERT (SELECT count(*) FROM setup.setup_version) = 0,  'RLS: B não vê setups de A';
  ASSERT (SELECT count(*) FROM events.lap) = 0,           'RLS: B não vê voltas de A';
  ASSERT (SELECT count(*) FROM events.track) = 0,         'RLS: pista privada de A invisível';
  ASSERT (SELECT count(*) FROM identity.team) = 1,        'RLS: B só vê a sua equipa';
  ASSERT (SELECT count(*) FROM identity.user_account) = 1,'RLS: B não vê utilizadores de A';
  ASSERT (SELECT count(*) FROM sync.change_log) = 0,      'RLS: change log isolado';
END $$;

-- B não consegue escrever com o team_id de A
DO $$ BEGIN
  BEGIN
    INSERT INTO garage.car (team_id, make, model) VALUES ('01900000-0000-7000-8000-0000000000a1', 'X', 'Y');
    RAISE EXCEPTION 'FALHOU: escrita noutra equipa';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;

-- B não consegue ligar um setup seu a um carro de A (FK composta)
DO $$ BEGIN
  BEGIN
    INSERT INTO setup.setup (team_id, car_id, name)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000c1', 'roubo');
    RAISE EXCEPTION 'FALHOU: referência a carro de outra equipa';
  EXCEPTION WHEN foreign_key_violation THEN NULL; END;
END $$;

-- UPDATE/DELETE em massa não tocam em A
UPDATE garage.car SET notes = 'x';
DELETE FROM events.lap;

-- ---------- De volta a A: nada mudou ----------
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000a1';
DO $$ BEGIN
  ASSERT (SELECT count(*) FROM events.lap) = 20, 'voltas de A intactas';
  ASSERT (SELECT count(*) FROM garage.car WHERE notes = 'x') = 0, 'carros de A intactos';
  ASSERT (SELECT count(*) FROM identity.team) = 1, 'A só vê a sua equipa';
END $$;

-- Idempotência: a chave é (user_id, request_id). Outro utilizador pode usar o mesmo request_id
-- sem colidir e nunca vê a linha alheia.
INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash)
VALUES ('01900000-0000-7000-8000-0000000000aa', app.current_user_id(), '/pitbox.identity.v1.TeamService/CreateTeam', '\x00');
DO $$ BEGIN
  BEGIN
    INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash)
    VALUES ('01900000-0000-7000-8000-0000000000aa', app.current_user_id(), 'x', '\x00');
    RAISE EXCEPTION 'FALHOU: request_id repetido pelo mesmo utilizador';
  EXCEPTION WHEN unique_violation THEN NULL; END;
END $$;
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000b';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000b9';
INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash)
VALUES ('01900000-0000-7000-8000-0000000000aa', app.current_user_id(), '/pitbox.identity.v1.TeamService/CreateTeam', '\x01');
DO $$ BEGIN
  ASSERT (SELECT count(*) FROM platform.idempotency_key) = 1, 'RLS: B só vê a sua chave de idempotência';
  BEGIN
    INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash)
    VALUES ('01900000-0000-7000-8000-0000000000ab', '01900000-0000-7000-8000-00000000000a', 'x', '\x00');
    RAISE EXCEPTION 'FALHOU: B escreveu chave em nome de A';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000a1';
DO $$ BEGIN ASSERT (SELECT count(*) FROM platform.idempotency_key) = 1, 'RLS: A só vê a sua chave'; END $$;

-- Equipa: category e club têm limites de tamanho (alinhados com o protovalidate)
DO $$ DECLARE v text; BEGIN
  FOREACH v IN ARRAY ARRAY['', repeat('x', 41)] LOOP
    BEGIN
      INSERT INTO identity.team (id, name, category, created_by) VALUES (app.current_team_id(), 'T', v, app.current_user_id());
      RAISE EXCEPTION 'FALHOU: category inválida aceite';
    EXCEPTION WHEN check_violation THEN NULL; END;
  END LOOP;
  FOREACH v IN ARRAY ARRAY['', repeat('x', 81)] LOOP
    BEGIN
      INSERT INTO identity.team (id, name, club, created_by) VALUES (app.current_team_id(), 'T', v, app.current_user_id());
      RAISE EXCEPTION 'FALHOU: club inválido aceite';
    EXCEPTION WHEN check_violation THEN NULL; END;
  END LOOP;
END $$;

-- Sem contexto de equipa: nada visível
RESET app.team_id;
SET LOCAL app.team_id = '';
DO $$ BEGIN ASSERT (SELECT count(*) FROM garage.car) = 0, 'sem contexto, sem dados'; END $$;

-- UUIDv7 é ordenável no tempo e tem versão 7
DO $$ DECLARE a uuid := app.uuid_v7(); b uuid; BEGIN
  PERFORM pg_sleep(0.002); b := app.uuid_v7();
  ASSERT a < b, 'uuid_v7 ordenado';
  ASSERT substr(a::text, 15, 1) = '7', 'versão 7';
END $$;

\echo 'Todos os testes passaram.'
ROLLBACK;
