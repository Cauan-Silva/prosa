-- Mensagens fixadas no canal.
ALTER TABLE mensagens
    ADD COLUMN fixada_em  TIMESTAMPTZ,
    ADD COLUMN fixada_por BIGINT REFERENCES usuarios (id) ON DELETE SET NULL;
CREATE INDEX mensagens_fixadas ON mensagens (canal_id, fixada_em DESC) WHERE fixada_em IS NOT NULL;

-- Prévias de links (título, descrição e imagem do site), guardadas para não buscar de novo.
CREATE TABLE previas_link (
    url         VARCHAR(2048) PRIMARY KEY,
    ok          BOOLEAN       NOT NULL,
    titulo      VARCHAR(300)  NOT NULL DEFAULT '',
    descricao   VARCHAR(500)  NOT NULL DEFAULT '',
    imagem      VARCHAR(2048) NOT NULL DEFAULT '',
    site        VARCHAR(100)  NOT NULL DEFAULT '',
    cor         VARCHAR(20)   NOT NULL DEFAULT '',
    buscada_em  TIMESTAMPTZ   NOT NULL DEFAULT now()
);
