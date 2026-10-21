-- F1-07a: uma escala/motorização nova é só uma migração de dados; não muda o esquema
-- do carro nem os contratos. Corre como postgres (simula a migração) e desfaz tudo.
\set QUIET on
BEGIN;

INSERT INTO garage.scale (code, name, active, sort_order) VALUES ('1/16', '1/16', true, 40);
INSERT INTO garage.powertrain (code, family, name, active, sort_order)
VALUES ('electric_test', 'electric', 'Elétrico de teste', true, 40);

INSERT INTO identity.user_account (id, idp_subject, email, display_name)
VALUES ('01900000-0000-7000-8000-00000000ca01', 'catalog-test', 'catalog-test@example.pt', 'Catalog Test');
INSERT INTO identity.team (id, name, created_by)
VALUES ('01900000-0000-7000-8000-00000000ca02', 'Equipa catálogo', '01900000-0000-7000-8000-00000000ca01');
INSERT INTO garage.car (id, team_id, make, model, scale, powertrain, created_by)
VALUES ('01900000-0000-7000-8000-00000000ca03', '01900000-0000-7000-8000-00000000ca02',
        'Teste', 'Catálogo', '1/16', 'electric_test', '01900000-0000-7000-8000-00000000ca01');

DO $$ BEGIN
  ASSERT (SELECT scale FROM garage.car WHERE id = '01900000-0000-7000-8000-00000000ca03') = '1/16',
    'escala nova não foi usada';
  ASSERT (SELECT powertrain FROM garage.car WHERE id = '01900000-0000-7000-8000-00000000ca03') = 'electric_test',
    'motorização nova não foi usada';
  ASSERT (SELECT count(*) FROM garage.scale WHERE code = '1/8') = 1, 'seed 1/8 desapareceu';
  ASSERT (SELECT count(*) FROM garage.powertrain WHERE code = 'nitro') = 1, 'seed nitro desapareceu';
  BEGIN
    INSERT INTO garage.scale (code, name) VALUES ('1/0', 'x');
    RAISE EXCEPTION 'escala 1/0 foi aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
  BEGIN
    INSERT INTO garage.powertrain (code, family, name) VALUES ('Nitro', 'combustion', 'x');
    RAISE EXCEPTION 'código com maiúscula foi aceite';
  EXCEPTION WHEN check_violation THEN NULL; END;
END $$;

ROLLBACK;
\echo 'catalog_test: OK'
