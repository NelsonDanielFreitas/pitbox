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

-- Idempotência: expires_at tem de ser posterior a created_at.
DO $$ BEGIN
  BEGIN
    INSERT INTO platform.idempotency_key
      (request_id, user_id, method, request_hash, created_at, expires_at)
    VALUES
      ('01900000-0000-7000-8000-0000000000ac', app.current_user_id(), 'x', '\x00', now(), now() - interval '1 second');
    RAISE EXCEPTION 'FALHOU: expires_at anterior a created_at aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
END $$;

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

-- ---------- F1-06: membros, convites e transferência ----------
-- Um segundo e um terceiro utilizador na equipa A (para os testes de dono e transferência).
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000a1';
SET LOCAL app.user_id = '01900000-0000-7000-8000-0000000000ba';
INSERT INTO identity.user_account (id, idp_subject, email, display_name)
VALUES (app.current_user_id(), 'sub-ba', 'mecanico-ba@a.pt', 'Mecânico BA');
SET LOCAL app.user_id = '01900000-0000-7000-8000-0000000000bb';
INSERT INTO identity.user_account (id, idp_subject, email, display_name)
VALUES (app.current_user_id(), 'sub-bb', 'piloto-bb@a.pt', 'Piloto BB');
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';
INSERT INTO identity.membership (team_id, user_id, role) VALUES
  (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', 'mechanic'),
  (app.current_team_id(), '01900000-0000-7000-8000-0000000000bb', 'pilot');

-- Um só dono por equipa, garantido na base.
DO $$ BEGIN
  BEGIN
    INSERT INTO identity.membership (team_id, user_id, role)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', 'owner');
    RAISE EXCEPTION 'FALHOU: dois donos aceites';
  EXCEPTION WHEN unique_violation THEN NULL; END;
END $$;

-- Convite: token_hash tem de ter 32 bytes; expires_at > created_at; (team_id,id) único.
DO $$ BEGIN
  BEGIN
    INSERT INTO identity.invitation (team_id, role, token_hash, expires_at, created_by)
    VALUES (app.current_team_id(), 'pilot', decode(repeat('11', 31), 'hex'), now() + interval '1 day', app.current_user_id());
    RAISE EXCEPTION 'FALHOU: token_hash curto aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO identity.invitation (team_id, role, token_hash, expires_at, created_by)
    VALUES (app.current_team_id(), 'pilot', decode(repeat('12', 32), 'hex'), now() - interval '1 second', app.current_user_id());
    RAISE EXCEPTION 'FALHOU: convite já expirado aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  INSERT INTO identity.invitation (team_id, role, token_hash, expires_at, created_by)
  VALUES (app.current_team_id(), 'pilot', decode(repeat('13', 32), 'hex'), now() + interval '1 day', app.current_user_id());
END $$;

-- Transferência: from <> to, validade <= 24 h e um só pendente por equipa.
DO $$ BEGIN
  BEGIN
    INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', '01900000-0000-7000-8000-0000000000ba',
            app.current_user_id(), now() + interval '1 hour');
    RAISE EXCEPTION 'FALHOU: transferência para o próprio aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', '01900000-0000-7000-8000-0000000000bb',
            app.current_user_id(), now() + interval '25 hours');
    RAISE EXCEPTION 'FALHOU: transferência com mais de 24 h aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
  VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', '01900000-0000-7000-8000-0000000000bb',
          app.current_user_id(), now() + interval '1 hour');
  BEGIN
    INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
    VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000bb', '01900000-0000-7000-8000-0000000000ba',
            app.current_user_id(), now() + interval '1 hour');
    RAISE EXCEPTION 'FALHOU: duas transferências pendentes aceites';
  EXCEPTION WHEN unique_violation THEN NULL; END;
  UPDATE identity.ownership_transfer SET cancelled_at = now()
   WHERE team_id = app.current_team_id() AND confirmed_at IS NULL AND cancelled_at IS NULL;
  INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
  VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000000ba', '01900000-0000-7000-8000-0000000000bb',
          app.current_user_id(), now() + interval '1 hour');
END $$;

-- accept_invitation: sem app.user_id falha fechado com o mesmo SQLSTATE das recusas.
RESET app.user_id;
DO $$ BEGIN
  BEGIN
    PERFORM identity.accept_invitation(decode(repeat('00', 32), 'hex'));
    RAISE EXCEPTION 'FALHOU: accept_invitation sem contexto';
  EXCEPTION WHEN SQLSTATE 'PB010' THEN NULL; END;
END $$;
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';


-- ---------- F1-07a: escala e motorização ----------
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000000a1';
SET LOCAL app.user_id = '01900000-0000-7000-8000-00000000000a';

-- Sem os campos, o carro fica 1/8/nitro.
INSERT INTO garage.car (id, team_id, make, model)
VALUES ('01900000-0000-7000-8000-0000000000c3', app.current_team_id(), 'Xray', 'XB8');
DO $$ BEGIN
  ASSERT (SELECT scale FROM garage.car WHERE id = '01900000-0000-7000-8000-0000000000c3') = '1/8', 'scale por omissão';
  ASSERT (SELECT powertrain FROM garage.car WHERE id = '01900000-0000-7000-8000-0000000000c3') = 'nitro', 'powertrain por omissão';
END $$;

-- Escala/motorização inexistente é recusada pela FK.
DO $$ BEGIN
  BEGIN
    INSERT INTO garage.car (id, team_id, make, model, scale)
    VALUES ('01900000-0000-7000-8000-0000000000c4', app.current_team_id(), 'X', 'Y', '1/100');
    RAISE EXCEPTION 'FALHOU: escala inexistente aceite';
  EXCEPTION WHEN foreign_key_violation THEN NULL; END;
  BEGIN
    INSERT INTO garage.car (id, team_id, make, model, powertrain)
    VALUES ('01900000-0000-7000-8000-0000000000c5', app.current_team_id(), 'X', 'Y', 'nuclear');
    RAISE EXCEPTION 'FALHOU: motorização inexistente aceite';
  EXCEPTION WHEN foreign_key_violation THEN NULL; END;
END $$;

-- A aplicação não escreve nos catálogos globais.
DO $$ BEGIN
  BEGIN
    INSERT INTO garage.scale (code, name) VALUES ('1/16', '1/16');
    RAISE EXCEPTION 'FALHOU: app escreveu em garage.scale';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    UPDATE garage.scale SET active = false;
    RAISE EXCEPTION 'FALHOU: app atualizou garage.scale';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    DELETE FROM garage.powertrain;
    RAISE EXCEPTION 'FALHOU: app apagou garage.powertrain';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;


\echo 'Todos os testes passaram.'
ROLLBACK;
