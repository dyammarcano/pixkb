CREATE TABLE IF NOT EXISTS econindex_series_point (
  series_code  TEXT        NOT NULL,
  point_date   DATE        NOT NULL,
  value_text   TEXT        NOT NULL,
  synced_at    TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (series_code, point_date)
);
