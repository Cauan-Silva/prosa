-- Personalização do perfil: moldura do avatar, efeito do cartão, tema de cores e estilo do nome.
-- Fica num JSON só porque é tudo enfeite: nenhuma consulta filtra por isso.
ALTER TABLE usuarios ADD COLUMN visual JSONB NOT NULL DEFAULT '{}';
