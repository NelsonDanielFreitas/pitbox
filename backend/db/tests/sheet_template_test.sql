-- F2-00: modelos de ficha (ADR 0030). Corre como postgres e desfaz tudo; troca para pitbox_app
-- para provar a RLS e os privilégios como a aplicação os vê.
\set QUIET on
BEGIN;

INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES
  ('01900000-0000-7000-8000-000000000501', 'st-a', 'st-a@example.pt', 'A'),
  ('01900000-0000-7000-8000-000000000502', 'st-b', 'st-b@example.pt', 'B');
INSERT INTO identity.team (id, name, created_by) VALUES
  ('01900000-0000-7000-8000-0000000005a1', 'Equipa A', '01900000-0000-7000-8000-000000000501'),
  ('01900000-0000-7000-8000-0000000005b1', 'Equipa B', '01900000-0000-7000-8000-000000000502');
INSERT INTO garage.car (id, team_id, make, model) VALUES
  ('01900000-0000-7000-8000-0000000005c1', '01900000-0000-7000-8000-0000000005a1', 'Mugen', 'MBX8R'),
  ('01900000-0000-7000-8000-0000000005c2', '01900000-0000-7000-8000-0000000005b1', 'Xray', 'XB8');
-- Modelo privado da equipa B (escrito como dono: a app não escreve modelos globais, mas escreve os seus).
INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, current_version)
VALUES ('01900000-0000-7000-8000-0000000005d2', '01900000-0000-7000-8000-0000000005b1', 'xray-xb8', 'Xray', 'XB8', '1/8', 'nitro', 'Xray XB8', 1);
INSERT INTO setup.sheet_template_version (template_id, version, definition)
VALUES ('01900000-0000-7000-8000-0000000005d2', 1, '{"format":1,"private":"B"}');

DO $$ BEGIN
  ASSERT (SELECT team_id FROM setup.sheet_template_version WHERE template_id = '01900000-0000-7000-8000-0000000005d2') =
         '01900000-0000-7000-8000-0000000005b1', 'o trigger copia o team_id do modelo para a versão';
  ASSERT (SELECT count(*) FROM setup.sheet_template WHERE team_id IS NULL AND slug = 'generic-1-8-nitro') = 1,
    'o catálogo global tem o genérico (migração)';
END $$;

-- ---------- Equipa A ----------
SET LOCAL ROLE pitbox_app;
SET LOCAL app.user_id = '01900000-0000-7000-8000-000000000501';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000005a1';

DO $$
DECLARE n integer;
BEGIN
  ASSERT (SELECT count(*) FROM setup.sheet_template WHERE slug = 'generic-1-8-nitro') = 1, 'A vê o global';
  ASSERT (SELECT count(*) FROM setup.sheet_template WHERE slug = 'xray-xb8') = 0, 'A não vê o modelo privado de B';
  ASSERT (SELECT count(*) FROM setup.sheet_template_version WHERE template_id = '01900000-0000-7000-8000-0000000005d2') = 0,
    'A não vê as versões do modelo de B';
  ASSERT (SELECT count(*) FROM setup.sheet_template_version WHERE team_id IS NULL) >= 1, 'A vê versões globais';

  -- O carro aceita um global e recusa o privado de B e um id inexistente, com o SQLSTATE próprio.
  UPDATE garage.car SET sheet_template_id = (SELECT id FROM setup.sheet_template WHERE slug = 'generic-1-8-nitro')
   WHERE id = '01900000-0000-7000-8000-0000000005c1';
  BEGIN
    UPDATE garage.car SET sheet_template_id = '01900000-0000-7000-8000-0000000005d2'
     WHERE id = '01900000-0000-7000-8000-0000000005c1';
    RAISE EXCEPTION 'FALHOU: carro ligado ao modelo privado de B';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;
  BEGIN
    UPDATE garage.car SET sheet_template_id = gen_random_uuid() WHERE id = '01900000-0000-7000-8000-0000000005c1';
    RAISE EXCEPTION 'FALHOU: carro ligado a um modelo inexistente';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;
  BEGIN
    INSERT INTO garage.car (team_id, make, model, sheet_template_id)
    VALUES (app.current_team_id(), 'X', 'Y', '01900000-0000-7000-8000-0000000005d2');
    RAISE EXCEPTION 'FALHOU: INSERT de carro com o modelo privado de B';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;

  -- A app não altera nem apaga o catálogo global.
  UPDATE setup.sheet_template SET name = 'hack' WHERE team_id IS NULL;
  GET DIAGNOSTICS n = ROW_COUNT;
  ASSERT n = 0, 'UPDATE a um global não devia afetar linhas';
  UPDATE setup.sheet_template SET team_id = app.current_team_id() WHERE team_id IS NULL;
  GET DIAGNOSTICS n = ROW_COUNT;
  ASSERT n = 0, 'A não pode puxar um global para a sua equipa';
  BEGIN DELETE FROM setup.sheet_template WHERE team_id IS NULL; RAISE EXCEPTION 'FALHOU: DELETE a modelos';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template (team_id, slug, make, model, scale, powertrain, name)
    VALUES (NULL, 'mine', 'a', 'b', '1/8', 'nitro', 'x');
    RAISE EXCEPTION 'FALHOU: A criou um modelo global';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template (team_id, slug, make, model, scale, powertrain, name)
    VALUES ('01900000-0000-7000-8000-0000000005b1', 'mine', 'a', 'b', '1/8', 'nitro', 'x');
    RAISE EXCEPTION 'FALHOU: A criou um modelo na equipa B';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;

-- A duplica o global: modelo da equipa + versão (o team_id da versão vem do trigger, mesmo que o cliente envie outro).
INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, based_on_id, created_by)
SELECT '01900000-0000-7000-8000-0000000005d1', app.current_team_id(), 'copia-a', make, model, scale, powertrain, 'Cópia A', id, app.current_user_id()
  FROM setup.sheet_template WHERE slug = 'generic-1-8-nitro';
INSERT INTO setup.sheet_template_version (template_id, team_id, version, definition)
VALUES ('01900000-0000-7000-8000-0000000005d1', '01900000-0000-7000-8000-0000000005b1', 1, '{"format":1,"copy":"A"}');
DO $$ BEGIN
  ASSERT (SELECT team_id FROM setup.sheet_template_version WHERE template_id = '01900000-0000-7000-8000-0000000005d1') =
         app.current_team_id(), 'team_id da versão vem do modelo, não do cliente';

  BEGIN
    INSERT INTO setup.sheet_template (team_id, slug, make, model, scale, powertrain, name, based_on_id)
    VALUES (app.current_team_id(), 'roubo', 'a', 'b', '1/8', 'nitro', 'x', '01900000-0000-7000-8000-0000000005d2');
    RAISE EXCEPTION 'FALHOU: based_on_id aponta para o modelo privado de B';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template_version (template_id, version, definition)
    VALUES ('01900000-0000-7000-8000-0000000005d2', 2, '{"format":1}');
    RAISE EXCEPTION 'FALHOU: versão acrescentada ao modelo de B';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template_version (template_id, version, definition)
    SELECT id, 99, '{"format":1}' FROM setup.sheet_template WHERE team_id IS NULL LIMIT 1;
    RAISE EXCEPTION 'FALHOU: versão acrescentada a um modelo global';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template_version (template_id, version, definition)
    VALUES ('01900000-0000-7000-8000-0000000005d1', 2, '[]');
    RAISE EXCEPTION 'FALHOU: definition que não é objeto';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO setup.sheet_template_version (template_id, version, definition)
    VALUES ('01900000-0000-7000-8000-0000000005d1', 2, jsonb_build_object('pad', repeat('x', 262144)));
    RAISE EXCEPTION 'FALHOU: definition acima do limite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    UPDATE setup.sheet_template_version SET definition = '{}' WHERE template_id = '01900000-0000-7000-8000-0000000005d1';
    RAISE EXCEPTION 'FALHOU: versão alterada';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    DELETE FROM setup.sheet_template_version WHERE template_id = '01900000-0000-7000-8000-0000000005d1';
    RAISE EXCEPTION 'FALHOU: versão apagada';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;

  -- Colunas de identidade imutáveis; só o nome e o estado mudam.
  UPDATE setup.sheet_template SET name = 'Cópia A (renomeada)' WHERE id = '01900000-0000-7000-8000-0000000005d1';
  ASSERT (SELECT version FROM setup.sheet_template WHERE id = '01900000-0000-7000-8000-0000000005d1') = 2, 'touch incrementa version';
  BEGIN
    UPDATE setup.sheet_template SET based_on_id = NULL WHERE id = '01900000-0000-7000-8000-0000000005d1';
    RAISE EXCEPTION 'FALHOU: based_on_id alterado';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;

  -- Ligar o carro à cópia da equipa é permitido.
  UPDATE garage.car SET sheet_template_id = '01900000-0000-7000-8000-0000000005d1' WHERE id = '01900000-0000-7000-8000-0000000005c1';
END $$;

-- ---------- Vista de análise e link público ----------
INSERT INTO setup.setup (id, team_id, car_id, name)
VALUES ('01900000-0000-7000-8000-0000000005e1', app.current_team_id(), '01900000-0000-7000-8000-0000000005c1', 'Setup');
INSERT INTO setup.setup_version (team_id, setup_id, version, data, schema_version) VALUES
  (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 1,
   '{"diffs":{"front_cst":7000},"shocks":{"front":{"oil_cst":400}},"transmission":{"spur_teeth":46}}', 1),
  (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 2,
   ('{"template":{"id":"01900000-0000-7000-8000-0000000005d1","version":1},"values":{},' ||
    '"canonical":{"diffs.front_cst":8000,"shocks.front.oil_cst":450,"transmission.spur_teeth":47}}')::jsonb, 2),
  (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 3,
   '{"template":{"id":"01900000-0000-7000-8000-0000000005d1","version":1},"values":{},"canonical":{"diffs.front_cst":"x","spur":1}}', 2);
DO $$ BEGIN
  ASSERT (SELECT (diff_front_cst, shock_front_oil_cst, spur_teeth) FROM setup.setup_version_metrics WHERE version = 1) =
         (7000, 400, 46::smallint), 'a vista dá o mesmo que as colunas geradas nas versões v1';
  ASSERT (SELECT (diff_front_cst, shock_front_oil_cst, spur_teeth) FROM setup.setup_version_metrics WHERE version = 2) =
         (8000, 450, 47::smallint), 'a vista lê data.canonical nas versões v2';
  ASSERT (SELECT diff_front_cst IS NULL AND diff_rear_cst IS NULL FROM setup.setup_version_metrics WHERE version = 3),
    'canonical malformado dá NULL e não um erro';
  ASSERT (SELECT count(*) FROM setup.setup_version_metrics) = 3, 'A vê as suas três versões';
END $$;

INSERT INTO setup.share_link (team_id, setup_id, pinned_version, token_hash, created_by) VALUES
  (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 1, decode(repeat('a1', 32), 'hex'), app.current_user_id()),
  (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 2, decode(repeat('a2', 32), 'hex'), app.current_user_id());
-- Uma versão cujo data aponta para o modelo privado de B (não devia acontecer; a função não pode vazá-lo).
INSERT INTO setup.setup_version (team_id, setup_id, version, data, schema_version)
VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 4,
        '{"template":{"id":"01900000-0000-7000-8000-0000000005d2","version":1},"values":{}}', 2);
INSERT INTO setup.share_link (team_id, setup_id, pinned_version, token_hash, created_by)
VALUES (app.current_team_id(), '01900000-0000-7000-8000-0000000005e1', 4, decode(repeat('a4', 32), 'hex'), app.current_user_id());

SET LOCAL app.team_id = '';
DO $$ BEGIN
  ASSERT (SELECT template_definition IS NULL FROM setup.open_share_link_v2(decode(repeat('a1', 32), 'hex'))),
    'versão v1: sem definição de modelo';
  ASSERT (SELECT template_definition FROM setup.open_share_link_v2(decode(repeat('a2', 32), 'hex'))) = '{"format":1,"copy":"A"}'::jsonb,
    'versão v2: devolve a definição do modelo da versão partilhada';
  ASSERT (SELECT count(*) FROM setup.open_share_link_v2(decode(repeat('a2', 32), 'hex'))) = 1, 'uma linha por link';
  ASSERT (SELECT template_definition IS NULL FROM setup.open_share_link_v2(decode(repeat('a4', 32), 'hex'))),
    'o link não vaza o modelo privado de outra equipa';
  ASSERT (SELECT count(*) FROM setup.open_share_link_v2(decode(repeat('dd', 32), 'hex'))) = 0, 'token inexistente não abre';
  ASSERT (SELECT count(*) FROM setup.open_share_link(decode(repeat('a1', 32), 'hex'))) = 1, 'a função antiga continua a funcionar';
END $$;

-- ---------- Equipa B ----------
SET LOCAL app.user_id = '01900000-0000-7000-8000-000000000502';
SET LOCAL app.team_id = '01900000-0000-7000-8000-0000000005b1';
DO $$ BEGIN
  ASSERT (SELECT count(*) FROM setup.sheet_template WHERE slug IN ('copia-a')) = 0, 'B não vê o modelo da equipa A';
  ASSERT (SELECT count(*) FROM setup.sheet_template WHERE slug = 'xray-xb8') = 1, 'B vê o seu modelo';
  ASSERT (SELECT count(*) FROM setup.setup_version_metrics) = 0, 'a vista respeita a RLS (security_invoker)';
  BEGIN
    UPDATE garage.car SET sheet_template_id = '01900000-0000-7000-8000-0000000005d1'
     WHERE id = '01900000-0000-7000-8000-0000000005c2';
    RAISE EXCEPTION 'FALHOU: B ligou o carro ao modelo de A';
  EXCEPTION WHEN SQLSTATE 'PB020' THEN NULL; END;
END $$;

-- ---------- Como dono: o catálogo global ----------
RESET ROLE;
DO $$ BEGIN
  BEGIN
    UPDATE setup.sheet_template_version SET definition = '{}' WHERE template_id = '01900000-0000-7000-8000-0000000005d2';
    RAISE EXCEPTION 'FALHOU: nem o dono altera uma versão publicada';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  BEGIN
    UPDATE setup.sheet_template SET current_version = 0 WHERE id = '01900000-0000-7000-8000-0000000005d2';
    RAISE EXCEPTION 'FALHOU: current_version = 0';
  EXCEPTION WHEN insufficient_privilege OR check_violation THEN NULL; END;
  -- current_version tem de existir: a FK é diferida, por isso forçamos a verificação.
  SET CONSTRAINTS setup.sheet_template_current_version_fk IMMEDIATE;
  BEGIN
    UPDATE setup.sheet_template SET current_version = 7 WHERE id = '01900000-0000-7000-8000-0000000005d2';
    RAISE EXCEPTION 'FALHOU: current_version sem versão correspondente';
  EXCEPTION WHEN foreign_key_violation THEN NULL; END;
  SET CONSTRAINTS setup.sheet_template_current_version_fk DEFERRED;
  -- Sobe com a nova versão acrescentada na mesma transação; nunca desce.
  INSERT INTO setup.sheet_template_version (template_id, version, definition)
  VALUES ('01900000-0000-7000-8000-0000000005d2', 2, '{"format":1,"private":"B2"}');
  UPDATE setup.sheet_template SET current_version = 2 WHERE id = '01900000-0000-7000-8000-0000000005d2';
  BEGIN
    UPDATE setup.sheet_template SET current_version = 1 WHERE id = '01900000-0000-7000-8000-0000000005d2';
    RAISE EXCEPTION 'FALHOU: current_version desceu';
  EXCEPTION WHEN insufficient_privilege THEN NULL; END;
  -- Slug global único.
  BEGIN
    INSERT INTO setup.sheet_template (team_id, slug, make, model, scale, powertrain, name, current_version)
    VALUES (NULL, 'generic-1-8-nitro', 'a', 'b', '1/8', 'nitro', 'dup', 1);
    RAISE EXCEPTION 'FALHOU: slug global duplicado';
  EXCEPTION WHEN unique_violation THEN NULL; END;
END $$;

-- Tudo o que ficou por validar (FK diferida) está consistente, como estaria no COMMIT.
SET CONSTRAINTS ALL IMMEDIATE;

ROLLBACK;
\echo 'sheet_template_test: OK'
