-- Fios (threads): conversas que nascem de uma mensagem de um canal de texto. São canais
-- filhos (tipo "fio") que seguem as permissões do canal pai.
ALTER TABLE canais DROP CONSTRAINT canais_tipo_check;
ALTER TABLE canais ADD CONSTRAINT canais_tipo_check CHECK (tipo IN ('texto', 'voz', 'dm', 'grupo', 'fio'));
ALTER TABLE canais
    ADD COLUMN pai_id          BIGINT REFERENCES canais (id) ON DELETE CASCADE,
    ADD COLUMN mensagem_origem BIGINT REFERENCES mensagens (id) ON DELETE SET NULL;
ALTER TABLE canais ADD CONSTRAINT canais_fio_tem_pai CHECK ((tipo = 'fio') = (pai_id IS NOT NULL));
CREATE UNIQUE INDEX canais_fio_origem ON canais (mensagem_origem) WHERE mensagem_origem IS NOT NULL;
CREATE INDEX canais_pai ON canais (pai_id) WHERE pai_id IS NOT NULL;
