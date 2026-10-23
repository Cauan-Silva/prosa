-- Mensagens diretas e grupos: são canais sem servidor ("dm" entre duas pessoas, "grupo"
-- com até 10). Assim reaproveitam mensagens, anexos, reações e leituras.
ALTER TABLE canais ALTER COLUMN servidor_id DROP NOT NULL;
ALTER TABLE canais DROP CONSTRAINT canais_tipo_check;
ALTER TABLE canais ADD CONSTRAINT canais_tipo_check CHECK (tipo IN ('texto', 'voz', 'dm', 'grupo'));
ALTER TABLE canais ADD CONSTRAINT canais_direta_sem_servidor CHECK ((tipo IN ('dm', 'grupo')) = (servidor_id IS NULL));
ALTER TABLE canais ADD COLUMN dono_id BIGINT REFERENCES usuarios (id) ON DELETE SET NULL; -- quem criou o grupo

CREATE TABLE conversa_membros (
    canal_id   BIGINT      NOT NULL REFERENCES canais (id) ON DELETE CASCADE,
    usuario_id BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    aberta     BOOLEAN     NOT NULL DEFAULT TRUE, -- aparece na lista (fechar só esconde)
    entrou_em  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (canal_id, usuario_id)
);
CREATE INDEX conversa_membros_usuario ON conversa_membros (usuario_id);

-- Uma DM só por par de pessoas.
CREATE TABLE dm_pares (
    menor    BIGINT NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    maior    BIGINT NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    canal_id BIGINT NOT NULL UNIQUE REFERENCES canais (id) ON DELETE CASCADE,
    PRIMARY KEY (menor, maior),
    CHECK (menor < maior)
);

-- Amizade: pedido de "de" para "para"; vira "aceita" quando a outra pessoa aceita.
CREATE TABLE amizades (
    de_id     BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    para_id   BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    estado    VARCHAR(8)  NOT NULL CHECK (estado IN ('pendente', 'aceita')),
    criada_em TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (de_id, para_id),
    CHECK (de_id <> para_id)
);
CREATE UNIQUE INDEX amizades_par ON amizades (LEAST(de_id, para_id), GREATEST(de_id, para_id));
CREATE INDEX amizades_para ON amizades (para_id);

CREATE TABLE bloqueios (
    quem_id      BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    bloqueado_id BIGINT      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    criado_em    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (quem_id, bloqueado_id)
);

-- Anexos de conversas diretas não têm servidor.
ALTER TABLE anexos ALTER COLUMN servidor_id DROP NOT NULL;
