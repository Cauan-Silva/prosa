CREATE TABLE usuarios (
    id            BIGSERIAL PRIMARY KEY,
    nome_usuario  VARCHAR(32)  NOT NULL,             -- @identificador, único
    nome_exibicao VARCHAR(40)  NOT NULL,
    email         VARCHAR(160) NOT NULL,
    senha_hash    VARCHAR(200) NOT NULL,
    criado_em     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX usuarios_nome ON usuarios (lower(nome_usuario));
CREATE UNIQUE INDEX usuarios_email ON usuarios (lower(email));

CREATE TABLE servidores (
    id         BIGSERIAL PRIMARY KEY,
    nome       VARCHAR(60) NOT NULL,
    dono_id    BIGINT      NOT NULL REFERENCES usuarios (id),
    criado_em  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- O cargo padrão (@todos) tem padrao = TRUE e posição 0. Quanto maior a posição, mais alto.
CREATE TABLE cargos (
    id           BIGSERIAL PRIMARY KEY,
    servidor_id  BIGINT      NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    nome         VARCHAR(40) NOT NULL,
    cor          INT         NOT NULL DEFAULT 0,
    permissoes   BIGINT      NOT NULL DEFAULT 0,
    posicao      INT         NOT NULL DEFAULT 0,
    padrao       BOOLEAN     NOT NULL DEFAULT FALSE,
    mencionavel  BOOLEAN     NOT NULL DEFAULT FALSE,
    criado_em    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cargos_servidor ON cargos (servidor_id);
CREATE UNIQUE INDEX cargos_um_padrao ON cargos (servidor_id) WHERE padrao;

CREATE TABLE membros (
    servidor_id    BIGINT      NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    usuario_id     BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    apelido        VARCHAR(40),
    castigado_ate  TIMESTAMPTZ,
    entrou_em      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (servidor_id, usuario_id)
);
CREATE INDEX membros_usuario ON membros (usuario_id);

CREATE TABLE membros_cargos (
    servidor_id  BIGINT NOT NULL,
    usuario_id   BIGINT NOT NULL,
    cargo_id     BIGINT NOT NULL REFERENCES cargos (id) ON DELETE CASCADE,
    PRIMARY KEY (servidor_id, usuario_id, cargo_id),
    FOREIGN KEY (servidor_id, usuario_id) REFERENCES membros (servidor_id, usuario_id) ON DELETE CASCADE
);

CREATE TABLE categorias (
    id           BIGSERIAL PRIMARY KEY,
    servidor_id  BIGINT      NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    nome         VARCHAR(40) NOT NULL,
    posicao      INT         NOT NULL DEFAULT 0
);
CREATE INDEX categorias_servidor ON categorias (servidor_id);

CREATE TABLE canais (
    id             BIGSERIAL PRIMARY KEY,
    servidor_id    BIGINT       NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    categoria_id   BIGINT       REFERENCES categorias (id) ON DELETE SET NULL,
    nome           VARCHAR(40)  NOT NULL,
    tipo           VARCHAR(5)   NOT NULL CHECK (tipo IN ('texto', 'voz')),
    topico         VARCHAR(300) NOT NULL DEFAULT '',
    posicao        INT          NOT NULL DEFAULT 0,
    modo_lento     INT          NOT NULL DEFAULT 0 CHECK (modo_lento BETWEEN 0 AND 21600), -- segundos
    criado_em      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX canais_servidor ON canais (servidor_id);

-- Ajustes de permissão de um canal para um cargo ou para uma pessoa.
CREATE TABLE permissoes_canal (
    canal_id   BIGINT     NOT NULL REFERENCES canais (id) ON DELETE CASCADE,
    tipo       VARCHAR(6) NOT NULL CHECK (tipo IN ('cargo', 'membro')),
    alvo_id    BIGINT     NOT NULL,
    permitir   BIGINT     NOT NULL DEFAULT 0,
    negar      BIGINT     NOT NULL DEFAULT 0,
    PRIMARY KEY (canal_id, tipo, alvo_id)
);

CREATE TABLE convites (
    codigo       VARCHAR(10) PRIMARY KEY,
    servidor_id  BIGINT      NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    criador_id   BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    usos         INT         NOT NULL DEFAULT 0,
    max_usos     INT,
    expira_em    TIMESTAMPTZ,
    criado_em    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX convites_servidor ON convites (servidor_id);

CREATE TABLE banimentos (
    servidor_id  BIGINT       NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    usuario_id   BIGINT       NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    motivo       VARCHAR(300) NOT NULL DEFAULT '',
    por_id       BIGINT       REFERENCES usuarios (id) ON DELETE SET NULL,
    criado_em    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (servidor_id, usuario_id)
);

-- O id das mensagens é crescente: é ele que garante a ordem do canal.
CREATE TABLE mensagens (
    id          BIGSERIAL PRIMARY KEY,
    canal_id    BIGINT      NOT NULL REFERENCES canais (id) ON DELETE CASCADE,
    autor_id    BIGINT      REFERENCES usuarios (id) ON DELETE SET NULL,
    conteudo    TEXT        NOT NULL DEFAULT '' CHECK (length(conteudo) <= 4000),
    responde_a  BIGINT      REFERENCES mensagens (id) ON DELETE SET NULL,
    menciona_todos BOOLEAN  NOT NULL DEFAULT FALSE,
    editada_em  TIMESTAMPTZ,
    criada_em   TIMESTAMPTZ NOT NULL DEFAULT now(),
    busca       TSVECTOR GENERATED ALWAYS AS (to_tsvector('portuguese', conteudo)) STORED
);
CREATE INDEX mensagens_canal ON mensagens (canal_id, id DESC);
CREATE INDEX mensagens_busca ON mensagens USING GIN (busca);

CREATE TABLE mencoes (
    mensagem_id  BIGINT NOT NULL REFERENCES mensagens (id) ON DELETE CASCADE,
    usuario_id   BIGINT NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    PRIMARY KEY (mensagem_id, usuario_id)
);
CREATE INDEX mencoes_usuario ON mencoes (usuario_id, mensagem_id DESC);

CREATE TABLE reacoes (
    mensagem_id  BIGINT      NOT NULL REFERENCES mensagens (id) ON DELETE CASCADE,
    usuario_id   BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    emoji        VARCHAR(32) NOT NULL,
    criada_em    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (mensagem_id, usuario_id, emoji)
);

CREATE TABLE anexos (
    id            BIGSERIAL PRIMARY KEY,
    mensagem_id   BIGINT       REFERENCES mensagens (id) ON DELETE CASCADE,
    enviado_por   BIGINT       NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    servidor_id   BIGINT       NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    nome          VARCHAR(200) NOT NULL,
    tipo          VARCHAR(100) NOT NULL,
    tamanho       BIGINT       NOT NULL,
    chave         VARCHAR(200) NOT NULL UNIQUE,
    criado_em     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX anexos_mensagem ON anexos (mensagem_id);

-- Até onde cada pessoa leu cada canal (para o indicador de não lidas).
CREATE TABLE leituras (
    usuario_id   BIGINT NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    canal_id     BIGINT NOT NULL REFERENCES canais (id) ON DELETE CASCADE,
    ultima_lida  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (usuario_id, canal_id)
);

CREATE TABLE auditoria (
    id           BIGSERIAL PRIMARY KEY,
    servidor_id  BIGINT       NOT NULL REFERENCES servidores (id) ON DELETE CASCADE,
    autor_id     BIGINT       REFERENCES usuarios (id) ON DELETE SET NULL,
    acao         VARCHAR(40)  NOT NULL,
    alvo         VARCHAR(120) NOT NULL DEFAULT '',
    detalhes     VARCHAR(500) NOT NULL DEFAULT '',
    criado_em    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX auditoria_servidor ON auditoria (servidor_id, id DESC);
