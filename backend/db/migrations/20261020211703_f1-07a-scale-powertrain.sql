-- F1-07a: catálogos globais de escala e motorização e atributos do carro (ADR 0016).
CREATE TYPE garage.powertrain_family AS ENUM ('combustion', 'electric');

CREATE TABLE garage.scale (
  code        text        PRIMARY KEY CHECK (code ~ '^1/[1-9][0-9]?$'),
  name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
  active      boolean     NOT NULL DEFAULT true,
  sort_order  smallint    NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE garage.powertrain (
  code        text        PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{0,31}$'),
  family      garage.powertrain_family NOT NULL,
  name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  active      boolean     NOT NULL DEFAULT true,
  sort_order  smallint    NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- Na v0.1 só existe 1/8 nitro; outras escalas/motorizações entram na Fase 6.
INSERT INTO garage.scale (code, name, active, sort_order) VALUES ('1/8', '1/8', true, 10);
INSERT INTO garage.powertrain (code, family, name, active, sort_order)
VALUES ('nitro', 'combustion', 'Nitro', true, 10);

ALTER TABLE garage.car
  ADD COLUMN scale      text NOT NULL DEFAULT '1/8',
  ADD COLUMN powertrain text NOT NULL DEFAULT 'nitro';

ALTER TABLE garage.car
  ADD CONSTRAINT car_scale_fk      FOREIGN KEY (scale)      REFERENCES garage.scale (code)      NOT VALID,
  ADD CONSTRAINT car_powertrain_fk FOREIGN KEY (powertrain) REFERENCES garage.powertrain (code) NOT VALID;

ALTER TABLE garage.car VALIDATE CONSTRAINT car_scale_fk;
ALTER TABLE garage.car VALIDATE CONSTRAINT car_powertrain_fk;

-- Dados de referência globais: a aplicação só lê; as migrações revistas escrevem.
REVOKE INSERT, UPDATE, DELETE ON garage.scale, garage.powertrain FROM pitbox_app;
REVOKE INSERT, UPDATE, DELETE ON garage.scale, garage.powertrain FROM pitbox_readonly;
