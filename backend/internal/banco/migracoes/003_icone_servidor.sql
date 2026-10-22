-- Imagem (ícone) do servidor, mostrada na barra lateral e nos convites.
ALTER TABLE servidores
    ADD COLUMN icone VARCHAR(200) NOT NULL DEFAULT '';   -- chave da imagem no armazenamento ('' = azulejo)
