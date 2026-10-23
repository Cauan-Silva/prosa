-- Emojis próprios de cada servidor (usados como :nome: e guardados como <:nome:id>).
CREATE TABLE emojis (
    id          BIGSERIAL PRIMARY KEY,
    servidor_id BIGINT       NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    nome        VARCHAR(32)  NOT NULL,
    chave       VARCHAR(200) NOT NULL,
    animado     BOOLEAN      NOT NULL DEFAULT FALSE,
    criado_por  BIGINT       REFERENCES usuarios (id) ON DELETE SET NULL,
    criado_em   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX emojis_nome ON emojis (servidor_id, lower(nome));

-- Enquetes: uma por mensagem, de 2 a 10 opções.
CREATE TABLE enquetes (
    mensagem_id BIGINT       PRIMARY KEY REFERENCES mensagens (id) ON DELETE CASCADE,
    pergunta    VARCHAR(300) NOT NULL,
    opcoes      TEXT[]       NOT NULL CHECK (cardinality(opcoes) BETWEEN 2 AND 10),
    multipla    BOOLEAN      NOT NULL DEFAULT FALSE,
    encerra_em  TIMESTAMPTZ
);
CREATE TABLE votos (
    mensagem_id BIGINT   NOT NULL REFERENCES enquetes (mensagem_id) ON DELETE CASCADE,
    usuario_id  BIGINT   NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    opcao       SMALLINT NOT NULL,
    PRIMARY KEY (mensagem_id, usuario_id, opcao)
);
