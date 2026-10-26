# Prosa

[![CI](https://github.com/Cauan-Silva/prosa/actions/workflows/ci.yml/badge.svg)](https://github.com/Cauan-Silva/prosa/actions/workflows/ci.yml)

Servidores de conversa em tempo real, no estilo do Discord, feitos do zero em Go e React: canais de texto e de voz, cargos com permissões por canal, moderação, convites, menções, reações, anexos e busca. A voz vai direto entre os navegadores em salas pequenas e passa por um servidor de mídia próprio (SFU com o Pion) nas grandes, com câmera e compartilhamento de tela, e o servidor roda em várias instâncias ao mesmo tempo usando o Redis para combinar tudo.

**Ver online:** https://prosa-s4u4.onrender.com (o plano gratuito pode levar ~1 minuto para acordar) · **[Baixar para Windows](https://github.com/Cauan-Silva/prosa/releases/latest)**

## O que tem aqui

**Servidores, cargos e permissões**
- Servidores com **imagem própria** (PNG, JPG, WebP ou GIF animado; sem imagem, um azulejo desenhado a partir do nome), **categorias** e canais de **texto** e de **voz**
- **Cargos** com cor, ordem (hierarquia) e 22 permissões: ver canal, enviar, anexar, reagir, mencionar @todos, gerenciar mensagens, entrar na voz, falar, câmera e tela, silenciar na voz, expulsar, banir, castigar, auditoria, administrador…
- **Permissões por canal**: em cada canal dá para negar ou permitir qualquer permissão para um cargo ou uma pessoa (canais privados, canais de avisos só leitura…)
- **Hierarquia de verdade**: ninguém mexe em cargos ou pessoas acima de si, nem dá uma permissão que não tem
- **Moderação**: expulsar, banir (com lista de banidos), castigar por um tempo (a pessoa só lê) e **registro de auditoria**
- **Convites** por link com validade e limite de usos

**Conversa**
- **Fios (threads)**: conversas paralelas que nascem de uma mensagem, num painel ao lado do canal, com as mesmas permissões dele
- Mensagens com **respostas**, **menções** (`@pessoa`, `@cargo`, `@todos`) com autocompletar, **reações**, edição (↑ edita a última) e exclusão
- **Mensagens fixadas** por canal, com painel para ver todas
- **Prévia de links** com título, descrição e imagem do site (buscada pelo servidor só em endereços públicos; `<link>` manda sem prévia)
- Formatação: `**negrito**`, `*itálico*`, `~~riscado~~`, `||spoiler||`, `` `código` ``, blocos de código e links
- **Emojis do servidor** (até 50, inclusive animados): usados com `:nome:` (com sugestões), pelo seletor de emojis ou como reação, e aparecem grandes quando a mensagem só tem emojis
- **Enquetes** com até 10 opções, uma ou várias escolhas, prazo e resultado ao vivo
- **Anexos** (arrastar, colar ou escolher) com prévia de imagens, vídeo e áudio, guardados num S3 compatível
- **Quem está digitando**, **quem está online**, mensagens **não lidas** por canal e **menções** em destaque
- **Amigos e mensagens diretas**: pedidos de amizade pelo @nome, amigos online, bloqueio, conversas a dois e **grupos de até 10 pessoas**, com tudo o que os canais têm (anexos, reações, menções, quem está digitando)
- **Busca** em português (“pizzas” acha “pizza”) só nos canais que você pode ler
- **Modo lento** por canal e limite de mensagens por pessoa contra flood

**Voz**
- Salas de voz com **microfone, câmera e compartilhamento de tela com som**, até 25 pessoas. Até 5 pessoas a mídia vai **direto entre os computadores** (sem passar pelo servidor); acima disso passa pelo SFU
- Tela em até **1080p e 60 fps**, no modo **Automático** (lê as estatísticas do WebRTC a cada 2 s e sobe ou desce resolução e fps conforme o processador e a internet, inclusive quando o codificador começa a descartar quadros) ou nos modos Nítido, Fluido e 60 fps
- **Codec escolhido pelo hardware**: AV1 ou VP9 quando a placa de vídeo comprime nesses formatos (bem menos banda que o H.264), H.264 da placa de vídeo nos outros casos, e a opção de usar o processador para o AV1. Cada pessoa recebe num codec que consegue abrir
- Quem assiste segura um pouco de vídeo (*jitter buffer*) para a imagem não engasgar quando a internet oscila, e vê o que está chegando (resolução, fps, perda de pacotes)
- Quem está falando acende; dá para silenciar o microfone ou o som, e moderadores silenciam ou tiram alguém da sala
- **Volume por pessoa** (0 a 200%) e da tela de cada um, e silenciar alguém só para você
- A voz continua enquanto você navega pelos canais de texto

**Perfil e configurações**
- **Perfil** com foto, banner, cor, pronomes, “sobre mim” e status (online, ausente, não perturbe, invisível) com texto e emoji; quem divide servidor com você vê o cartão completo ao clicar no seu nome
- **Aparência**: 9 temas (Azulejo, Meia-noite, Neon, Grafite, Floresta, Café, Rosa, Alto contraste…), qualquer cor de destaque, 5 fontes, tamanho da interface, mensagens compactas e cantos retos. Fica salvo na conta e vale em qualquer navegador
- **Voz e vídeo**: escolha de microfone, saída e câmera, teste de microfone com medidor, **supressão de ruído com IA** (RNNoise rodando num AudioWorklet), supressão de eco, **filtro de ruído (portão)** com sensibilidade ajustável e **pressionar para falar** com tecla configurável
- **Notificações do sistema** e **sons** da interface, com volume; o status “não perturbe” silencia tudo
- **Acessibilidade**: escala, alto contraste, fonte de leitura fácil, sem animações, links sublinhados
- **Instalável** como aplicativo (PWA) no computador e no celular
- **Programa para Windows** ([Tauri](desktop/)): pressionar para falar funcionando com o Prosa em segundo plano (até em jogo em tela cheia), ícone na bandeja e abrir junto com o Windows. O instalador é gerado pelo GitHub Actions

**Visual próprio**: logo do Prosa e um **azulejo** desenhado a partir do nome de cada servidor. Funciona no celular.

## Arquitetura

```mermaid
flowchart LR
    N1["Navegador"] & N2["Navegador"] --> LB["Balanceador"]
    LB --> I1 & I2
    subgraph I1["Instância 1 (Go)"]
        A1["API REST"]
        G1["Gateway (WebSocket)"]
        V1["SFU de voz (Pion)"]
    end
    subgraph I2["Instância 2 (Go)"]
        A2["API REST"]
        G2["Gateway (WebSocket)"]
        V2["SFU de voz (Pion)"]
    end
    I1 & I2 <--> R[("Redis: eventos, presença, salas de voz")]
    I1 & I2 --> DB[("PostgreSQL")]
    I1 & I2 --> S3[("S3: anexos")]
    N1 <-. "mídia (WebRTC)" .-> V1
    N2 <-. "mídia (WebRTC)" .-> V1
```

**Permissões como no Discord.** Cada permissão é um bit num inteiro de 64 bits. A base de uma pessoa é a soma (OU) dos seus cargos, incluindo `@todos`. Em cada canal entram os ajustes, nesta ordem: `@todos` (nega, depois permite), os cargos da pessoa (soma das negações, depois soma das permissões) e a própria pessoa. Dono e administradores podem tudo; quem está de castigo só lê; e quem não vê o canal não tem nenhuma outra permissão nele. O cálculo fica num pacote puro (`internal/permissoes`), testado à parte.

**O gateway filtra os eventos por permissão.** Cada aba aberta tem uma conexão WebSocket que assina os tópicos dos servidores da pessoa (`servidor:42`) e o dela (`usuario:7`). Todo evento diz que permissão é preciso ter no canal para recebê-lo, e a conexão confere antes de mandar: uma mensagem de um canal privado nunca sai do servidor para quem não pode ler. As permissões de cada conexão ficam guardadas e são recalculadas quando chega um evento que pode mudá-las (cargo alterado, ajuste de canal, membro alterado); se o que a pessoa vê mudou, a tela recebe `PERMISSOES_ALTERADAS` e recarrega o servidor.

**Várias instâncias com Redis.** Com `REDIS_URL`, os eventos passam pelo Redis Pub/Sub. Cada instância só assina no Redis os tópicos que alguém conectado nela precisa, então uma instância não recebe o tráfego de servidores em que ninguém dela está. A presença conta as conexões de cada pessoa em todas as instâncias; cada instância renova uma chave de vida a cada 10 s, e se uma cair sem avisar, as pessoas dela saem sozinhas quando a chave expira. O limite de mensagens por pessoa também fica no Redis. Sem `REDIS_URL`, tudo roda em memória numa instância só.

**Ordem e reconexão.** As mensagens têm id crescente do PostgreSQL e a tela ordena por ele. Ao reconectar, o navegador recebe o estado de novo (`PRONTO`) e busca o que perdeu em cada canal aberto (`?depois=<última>`). Cada mensagem enviada leva um `nonce`, que troca a mensagem “enviando…” pela real sem duplicar.

**Salas pequenas falam direto.** Com até 5 pessoas (`VOZ_DIRETO_MAX`), cada navegador abre uma conexão WebRTC com cada um dos outros e o áudio e o vídeo vão direto, sem passar pelo servidor, que só repassa a negociação (oferta, resposta e candidatos) pela conexão de voz. Isso deixa a latência menor e não gasta o tráfego da hospedagem (o plano grátis do Render inclui só 5 GB de saída por mês). Quem tem o id menor liga, e cada conexão já nasce com as quatro faixas, então ligar a câmera ou a tela é só trocar a faixa, sem renegociar. Como o servidor não está no caminho, quem recebe é que corta o áudio de quem foi silenciado ou não tem permissão. Quando a sala passa do limite, todo mundo troca para o SFU; quando volta a caber (com folga de uma pessoa, para não ficar trocando), volta para o direto.

**Salas grandes usam o SFU.** Cada pessoa abre uma única conexão WebRTC com o servidor e manda até quatro faixas (microfone, câmera, tela e som da tela). O servidor repassa os pacotes de cada faixa para os outros da sala sem decodificar nada, então cada navegador envia uma vez só, seja para 2 ou para 25 pessoas. A negociação é sempre puxada pelo servidor: ele manda a oferta e o navegador só responde, o que evita ofertas cruzadas. Ligar a câmera é só trocar a faixa (sem renegociar); quando alguém entra ou liga uma faixa nova, o servidor a acrescenta nas conexões dos outros e manda uma oferta nova. Áudio de quem não pode falar ou foi silenciado não é repassado.

**Uma sala de voz mora numa instância.** A mídia de todos tem que passar pelo mesmo SFU, então a primeira instância que recebe alguém na sala reserva a sala no Redis (`SETNX` com validade renovada). Quem cair em outra instância é redirecionado para a dona (`ENDERECO_PUBLICO`). Quem está em cada sala é publicado para todo mundo do servidor pelo mesmo barramento de eventos.

**Teste de carga.** `cmd/carga` cadastra N pessoas, conecta todas no tempo real e coloca algumas mandando mensagens num ritmo fixo, medindo o tempo do envio até a chegada em cada conexão. Numa máquina de 2 vCPUs rodando tudo junto (2 instâncias, PostgreSQL, Redis e o próprio gerador de carga):

| Pessoas conectadas | Mensagens/s | Entregas/s | Perdidas | Latência p50 | p95 | p99 |
|---|---|---|---|---|---|---|
| 1.000 (em 2 instâncias) | 50 | 50.000 | 0 | 78 ms | 196 ms | 265 ms |

Duas otimizações vieram do teste: cada evento é convertido em JSON uma vez só no Hub (não uma vez por conexão), e a compressão por conexão do WebSocket foi desligada; juntas, derrubaram o p99 de 2,2 s para 265 ms.

## Tecnologias

- **Servidor:** Go 1.24 (biblioteca padrão para HTTP e rotas), [coder/websocket](https://github.com/coder/websocket), [Pion WebRTC](https://github.com/pion/webrtc) (SFU), [go-redis](https://github.com/redis/go-redis), PostgreSQL com [lib/pq](https://github.com/lib/pq) e busca de texto em português, migrações SQL embutidas (com trava para várias instâncias subirem juntas)
- **Arquivos:** cliente S3 escrito à mão (assinatura AWS Signature V4, conferida com o exemplo da documentação da AWS), compatível com Backblaze B2, Cloudflare R2 e MinIO; links temporários assinados para baixar
- **Segurança:** senhas com PBKDF2-SHA256 e JWT HS256 com a biblioteca padrão, limite de tentativas de login por IP, tipo dos anexos detectado pelo conteúdo, cabeçalhos de segurança (CSP, nosniff, frame-ancestors)
- **Telas:** React 19 e Vite, sem outras dependências (estado, roteador, WebRTC e formatação de texto escritos à mão)
- **Infra:** Docker (imagem distroless), docker-compose com 2 instâncias + Caddy + Redis + PostgreSQL, Render, Neon e GitHub Actions
- **Testes:** 42 testes em Go, incluindo ponta a ponta com PostgreSQL, WebSocket de verdade e duas instâncias ligadas por Redis

## Como rodar no seu computador

Precisa de [Go 1.24+](https://go.dev/dl/), [Node 22+](https://nodejs.org) e um PostgreSQL (ou Docker).

**Tudo com Docker (2 instâncias, balanceador, Redis e PostgreSQL):**

```bash
docker compose up --build
```

Abra http://localhost:8080. Para testar com outra pessoa, abra uma janela anônima e crie outra conta.

**Para desenvolver (uma instância, com recarga automática das telas):**

```bash
# 1. Banco
docker compose up -d banco

# 2. Servidor (porta 8080)
cd backend
AMBIENTE=desenvolvimento DATABASE_URL="postgres://prosa:prosa@localhost:5432/prosa?sslmode=disable" go run ./cmd/prosa

# 3. Telas (porta 5173, repassa /api para o servidor)
cd frontend
npm install
npm run dev
```

No Windows (PowerShell), troque a linha do servidor por:

```powershell
$env:AMBIENTE="desenvolvimento"; $env:DATABASE_URL="postgres://prosa:prosa@localhost:5432/prosa?sslmode=disable"; go run ./cmd/prosa
```

Para o banco do compose ficar acessível em `localhost:5432`, acrescente `ports: ["5432:5432"]` no serviço `banco`.

**Testes:**

```bash
cd backend
go test -race ./...    # sem banco e sem Redis, os testes de integração são pulados

PROSA_TESTE_BANCO="postgres://prosa:prosa@localhost:5432/postgres?sslmode=disable" \
PROSA_TESTE_REDIS="redis://localhost:6379/15" \
go test -race ./...
```

**Teste de carga:**

```bash
# suba o servidor com LOGIN_POR_MINUTO=100000 (o gerador cadastra muita gente do mesmo IP)
cd backend
go run ./cmd/carga -url http://localhost:8081,http://localhost:8082 -usuarios 1000 -remetentes 50 -taxa 1 -duracao 30s
```

## Variáveis de ambiente

| Variável | Para quê |
|---|---|
| `DATABASE_URL` | Conexão com o PostgreSQL, ex.: `postgres://usuario:senha@host/banco?sslmode=require` |
| `JWT_SEGREDO` | Segredo dos logins, com pelo menos 32 caracteres (em desenvolvimento há um padrão) |
| `PORT` | Porta do servidor (padrão 8080; o Render define sozinho) |
| `AMBIENTE` | `desenvolvimento` libera o segredo padrão e o WebSocket vindo do Vite |
| `ORIGENS_WS` | Endereços aceitos no WebSocket vindo de outro endereço, separados por vírgula (ex.: o do balanceador quando a voz vai direto para uma instância) |
| `REDIS_URL` | Liga o modo de várias instâncias (eventos, presença, limite e salas de voz pelo Redis) |
| `INSTANCIA` | Nome desta instância (gerado se vazio) |
| `ENDERECO_PUBLICO` | Endereço desta instância visto pelo navegador, para redirecionar a sala de voz (só com várias instâncias) |
| `S3_ENDPOINT`, `S3_REGIAO`, `S3_BUCKET`, `S3_CHAVE`, `S3_SEGREDO` | Armazenamento dos anexos. Sem eles, os anexos vão para a pasta `PASTA_ARQUIVOS` (padrão `arquivos`) |
| `MAX_ANEXO_MB` | Tamanho máximo de cada anexo (padrão 8) |
| `CF_TURN_ID`, `CF_TURN_TOKEN` | TURN do Cloudflare Realtime (1.000 GB grátis por mês); o Prosa gera credenciais temporárias sozinho |
| `TURN_URLS`, `TURN_USUARIO`, `TURN_SENHA` | Ou um servidor TURN fixo (obrigatório ter um TURN onde não entra UDP, como no Render) |
| `VOZ_PORTA_UDP` | Porta UDP única para toda a mídia (padrão: portas aleatórias) |
| `VOZ_DIRETO_MAX` | Até quantas pessoas a mídia vai direto entre os navegadores, sem passar pelo servidor (padrão: 5; `0` desliga) |
| `VOZ_IP_PUBLICO` | IP anunciado para a mídia quando o servidor está atrás de NAT ou Docker |
| `LOGIN_POR_MINUTO` | Tentativas de login e cadastro por IP por minuto (padrão 10) |

## Publicar de graça (Render + Neon + Backblaze)

1. **Banco no Neon:** no seu projeto do [Neon](https://neon.tech), crie um banco novo chamado `prosa` e copie a connection string (`postgresql://...`). Tire o `-pooler` do endereço, se houver, e o `&channel_binding=require` do fim.
2. **Anexos no Backblaze B2** (opcional, mas sem isso os anexos somem a cada deploy): crie um bucket **privado** e uma Application Key com acesso a ele. O `S3_ENDPOINT` aparece na página do bucket (ex.: `https://s3.us-east-005.backblazeb2.com`) e a `S3_REGIAO` é o pedaço do meio (`us-east-005`).
3. **Servidor no Render:** em [render.com](https://render.com), **New → Blueprint** e escolha este repositório. Preencha `DATABASE_URL` e, se quiser, as variáveis `S3_*` e `TURN_*`. O `JWT_SEGREDO` é gerado sozinho.
4. Espere o deploy e abra o endereço que o Render mostrar. As tabelas são criadas no primeiro start.

O plano gratuito do Render dorme depois de 15 minutos sem uso; o primeiro acesso leva cerca de 1 minuto, e o tempo real reconecta sozinho.

### Voz no Render (TURN)

Em salas de até 5 pessoas a mídia vai direto entre os navegadores e quase nunca precisa de TURN (só quando a rede de alguém bloqueia a conexão direta). Nas salas maiores ela passa pelo Prosa, e o Render só recebe tráfego HTTP: a mídia (UDP) não chega direto no servidor. Por isso, lá a voz precisa de um servidor **TURN**, que retransmite a mídia entre o navegador e o Prosa.

- **Recomendado: [TURN do Cloudflare Realtime](https://developers.cloudflare.com/realtime/turn/)**, com 1.000 GB grátis por mês. No painel do Cloudflare, crie uma chave TURN em **Realtime → TURN Server** e coloque o **Token ID** em `CF_TURN_ID` e o **API Token** em `CF_TURN_TOKEN`. O Prosa pede credenciais temporárias (24 h) ao Cloudflare e renova sozinho.
- **Ou um TURN fixo**, como o da [Metered](https://www.metered.ca/stun-turn) (o plano grátis tem só 500 MB por mês, que uma tela compartilhada gasta em minutos): preencha `TURN_URLS` (separados por vírgula), `TURN_USUARIO` e `TURN_SENHA`. Sem TURN, todo o resto funciona normalmente; só a voz não conecta. Rodando com Docker no seu computador ou numa VPS com a porta UDP liberada, o TURN não é necessário.

## API

Todas as rotas, menos cadastro, login, saúde e ver convite, pedem `Authorization: Bearer <token>`.

| Método | Rota | Descrição |
|---|---|---|
| POST | `/api/cadastro`, `/api/entrar` | Cria a conta / entra (devolve o token) |
| GET, PATCH | `/api/eu` | Sua conta / muda nome, bio, pronomes, cor e status |
| PUT | `/api/eu/preferencias` | Salva tema, cores, voz e notificações na conta |
| POST, DELETE | `/api/eu/avatar`, `/api/eu/banner` | Envia / tira foto e banner |
| POST, DELETE | `/api/servidores/{id}/icone` | Envia / tira a imagem do servidor |
| POST | `/api/servidores/{id}/emojis` | Emoji novo (multipart: nome e arquivo) |
| PATCH, DELETE | `/api/servidores/{id}/emojis/{emoji}` | Renomear / apagar emoji |
| POST | `/api/canais/{id}/mensagens/{msg}/fio` | Criar o fio de uma mensagem (as mensagens do fio usam `/api/canais/{fio}/...`) |
| GET | `/api/canais/{id}/fios` | Fios do canal |
| PATCH, DELETE | `/api/canais/{fio}/fio` | Renomear / apagar o fio |
| PUT | `/api/canais/{id}/mensagens/{msg}/votos` | Votar na enquete (`{"opcoes": [0]}`) |
| POST | `/api/canais/{id}/mensagens/{msg}/encerrar` | Encerrar a enquete |
| GET, POST | `/api/amigos` | Amigos, pedidos e bloqueados / pedir amizade pelo @nome |
| PUT, DELETE | `/api/amigos/{usuario}` | Pedir ou aceitar / desfazer, recusar ou cancelar |
| PUT, DELETE | `/api/bloqueios/{usuario}` | Bloquear / desbloquear |
| GET, POST | `/api/conversas` | Suas DMs e grupos / abrir DM ou criar grupo (as mensagens usam `/api/canais/{id}/...`) |
| PATCH, DELETE | `/api/conversas/{id}` | Renomear o grupo / fechar a DM |
| POST, DELETE | `/api/conversas/{id}/membros[/{usuario}]` | Adicionar ao grupo / sair ou tirar |
| GET | `/api/usuarios/{u}` | Perfil de quem divide servidor com você |
| GET | `/api/perfis/{u}/{arquivo}` | Imagem de perfil (pública, nome aleatório) |
| GET | `/api/eu/mencoes` | Últimas mensagens que mencionaram você |
| GET, POST | `/api/servidores` | Seus servidores / cria um |
| GET, PATCH, DELETE | `/api/servidores/{s}` | Visão do servidor (canais que você vê, cargos, suas permissões) / altera / apaga |
| DELETE | `/api/servidores/{s}/eu` | Sai do servidor |
| GET | `/api/servidores/{s}/busca?q=&autor=&canal=` | Busca nas mensagens |
| POST, PATCH, DELETE | `/api/servidores/{s}/canais[/{c}]` | Canais |
| PUT | `/api/servidores/{s}/ordem-canais` | Reordena canais |
| POST, PATCH, DELETE | `/api/servidores/{s}/categorias[/{id}]` | Categorias |
| GET, PUT | `/api/servidores/{s}/canais/{c}/permissoes` | Ajustes de permissão do canal |
| GET, POST, PATCH, DELETE | `/api/servidores/{s}/cargos[/{id}]` | Cargos |
| PUT | `/api/servidores/{s}/ordem-cargos` | Reordena cargos |
| GET | `/api/servidores/{s}/membros` | Membros |
| PATCH | `/api/servidores/{s}/membros/{u}/apelido` | Apelido |
| PUT, DELETE | `/api/servidores/{s}/membros/{u}/cargos/{id}` | Dá / tira cargo |
| DELETE | `/api/servidores/{s}/membros/{u}` | Expulsa |
| PUT | `/api/servidores/{s}/membros/{u}/castigo` | Castiga (`minutos: 0` tira o castigo) |
| GET, PUT, DELETE | `/api/servidores/{s}/banimentos[/{u}]` | Banimentos |
| GET | `/api/servidores/{s}/auditoria` | Registro de auditoria |
| GET, POST, DELETE | `/api/servidores/{s}/convites`, `.../canais/{c}/convites`, `.../convites/{codigo}` | Convites |
| GET, POST | `/api/convites/{codigo}` | Vê / aceita convite |
| GET, POST | `/api/canais/{c}/mensagens?antes=&depois=` | Lista (50 por vez) / envia |
| PATCH, DELETE | `/api/canais/{c}/mensagens/{m}` | Edita / apaga |
| PUT, DELETE | `/api/canais/{c}/mensagens/{m}/reacoes/{emoji}` | Reage / tira reação |
| POST | `/api/canais/{c}/lido` | Marca como lido |
| POST | `/api/canais/{c}/anexos` | Envia anexo (multipart, campo `arquivo`) |
| GET | `/api/anexos/{id}/{nome}` | Baixa anexo (redireciona para link temporário do S3) |
| GET | `/api/canais/{c}/voz` | Onde fica a sala de voz e quais servidores ICE usar |
| WS | `/api/gateway?t=` | Tempo real |
| WS | `/api/voz/{c}?t=` | Sinalização da voz |
| GET | `/api/saude` | Verificação de saúde |

**Gateway** (JSON `{"t": tipo, "s": servidor, "c": canal, "d": dados}`):
- **Do servidor:** `PRONTO` (tudo, ao conectar), `USUARIO_ALTERADO`, `SERVIDOR_CRIADO`/`ALTERADO`/`APAGADO`, `CANAL_*`, `CATEGORIA_*`, `CARGO_*`, `MEMBRO_ENTROU`/`ALTERADO`/`SAIU`, `MENSAGEM_CRIADA`/`EDITADA`/`APAGADA`, `REACAO_ADICIONADA`/`REMOVIDA`, `DIGITANDO`, `PRESENCA`, `VOZ_ESTADO`, `PERMISSOES_ALTERADAS`, `EMOJIS_ALTERADOS`, `FIO_ATUALIZADO`, `AMIGO_ALTERADO`, `CONVERSA_ALTERADA` (eventos sem servidor, como os de DMs, chegam com `s` vazio).
- **Do navegador:** `DIGITANDO` e `PING`.

**Voz:** o servidor manda `VOZ_PRONTO`, `VOZ_OFERTA` (com o mapa de quais transceptores são microfone, câmera e tela), `VOZ_ICE`, `VOZ_REDIRECIONAR` e `VOZ_ERRO`; o navegador manda `VOZ_RESPOSTA`, `VOZ_ICE`, `VOZ_ESTADO` e `VOZ_MODERAR`. Em salas pequenas (modo direto), `VOZ_SALA` diz o modo e quem está na sala, e `VOZ_P2P` leva oferta, resposta e candidatos de um navegador para outro.

## Estrutura

```
backend/
├── cmd/
│   ├── prosa/           # main: configuração, banco, Redis, voz e HTTP
│   └── carga/           # teste de carga
└── internal/
    ├── acesso/          # carrega servidor, cargos e ajustes e responde "pode?"
    ├── api/             # JSON, erros e o middleware de login
    ├── arquivos/        # anexos: S3 (SigV4 à mão) ou pasta local
    ├── auth/            # PBKDF2 e JWT com a biblioteca padrão
    ├── banco/           # conexão e migrações SQL embutidas
    ├── config/          # variáveis de ambiente
    ├── diretas/         # amigos, bloqueios, DMs e grupos
    ├── eventos/         # Hub de tópicos, publicação local e pelo Redis
    ├── gateway/         # WebSocket de tempo real com filtro por permissão
    ├── mensagens/       # mensagens, menções, reações, fios, enquetes, fixadas, busca e anexos
    ├── permissoes/      # o cálculo das permissões (puro, sem banco)
    ├── presenca/        # quem está online (memória ou Redis)
    ├── previas/         # prévias de links (só endereços públicos)
    ├── servidor/        # rotas, segurança e testes de ponta a ponta
    ├── servidores/      # servidores, canais, cargos, emojis, membros, moderação e convites
    ├── usuarios/        # cadastro e login
    ├── voz/             # SFU com Pion e coordenação das salas entre instâncias
    └── web/             # serve o React embutido no binário
frontend/src/
├── componentes/         # Canais, Conversa, Mensagem, Compositor, SalaVoz, configurações…
├── paginas/             # Entrar e Aplicativo
└── servicos/            # API, estado + gateway, voz (WebRTC), microfone (RNNoise), permissões, texto, roteador
frontend/public/ruido/   # RNNoise em WebAssembly + o AudioWorklet que o roda
desktop/                 # programa para Windows (Tauri 2, Rust)
```

## Licença

[MIT](LICENSE)
