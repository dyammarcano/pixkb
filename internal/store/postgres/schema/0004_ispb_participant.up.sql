CREATE TABLE IF NOT EXISTS ispb_participant (
  ispb_code           VARCHAR(8)  PRIMARY KEY,
  institution_name    TEXT        NOT NULL,
  legal_name          TEXT        NOT NULL DEFAULT '',
  compe_code          TEXT        NOT NULL DEFAULT '',
  participates_compe  BOOLEAN     NOT NULL DEFAULT FALSE,
  access_type         TEXT        NOT NULL DEFAULT '',
  operation_start     DATE        NOT NULL DEFAULT '0001-01-01',
  str_synced_at       TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
  cnpj                VARCHAR(14) NOT NULL DEFAULT '',
  pix_authorized      BOOLEAN     NOT NULL DEFAULT FALSE,
  pix_synced_at       TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
