-- Perfil personalizável e preferências da conta.
ALTER TABLE usuarios
    ADD COLUMN avatar       VARCHAR(200) NOT NULL DEFAULT '',   -- chave da imagem no armazenamento
    ADD COLUMN banner       VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN cor_perfil   INT          NOT NULL DEFAULT 0,    -- 0 = cor automática
    ADD COLUMN bio          VARCHAR(400) NOT NULL DEFAULT '',
    ADD COLUMN pronomes     VARCHAR(40)  NOT NULL DEFAULT '',
    ADD COLUMN status       VARCHAR(12)  NOT NULL DEFAULT 'online'
        CHECK (status IN ('online', 'ausente', 'ocupado', 'invisivel')),
    ADD COLUMN status_texto VARCHAR(80)  NOT NULL DEFAULT '',
    ADD COLUMN status_emoji VARCHAR(16)  NOT NULL DEFAULT '',
    ADD COLUMN preferencias JSONB        NOT NULL DEFAULT '{}';
