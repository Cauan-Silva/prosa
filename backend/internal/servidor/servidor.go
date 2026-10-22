// Package servidor monta as rotas HTTP do Prosa.
package servidor

import (
	"bufio"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/gateway"
	"github.com/Cauan-Silva/prosa/internal/mensagens"
	"github.com/Cauan-Silva/prosa/internal/presenca"
	"github.com/Cauan-Silva/prosa/internal/servidores"
	"github.com/Cauan-Silva/prosa/internal/usuarios"
	"github.com/Cauan-Silva/prosa/internal/voz"
	"github.com/Cauan-Silva/prosa/internal/web"
)

// Opcoes junta o que as rotas precisam.
type Opcoes struct {
	DB         *sql.DB
	Emissor    *auth.Emissor
	Publicador eventos.Publicador
	Hub        *eventos.Hub      // onde as conexões desta instância assinam
	Presenca   presenca.Registro // nil = em memória
	OrigensWS  []string
	Limitador  mensagens.Limitador // nil = em memória
	Voz        *voz.SFU            // nil = sem canais de voz funcionando
	// LoginPorMinuto: tentativas de entrar/cadastrar por IP por minuto (0 = 10).
	LoginPorMinuto int
	Armazem        arquivos.Armazem
	MaxAnexoMB     int
	Instancia      string
	// Rotas extras montadas por outros pacotes (gateway, voz), já com o caminho completo.
	Extras map[string]http.Handler
	// Telas é o frontend (nil = o embutido no binário).
	Telas http.Handler
}

// Servicos são os serviços criados por Novo (o gateway e os testes usam).
type Servicos struct {
	Usuarios   *usuarios.Servico
	Servidores *servidores.Servico
	Mensagens  *mensagens.Servico
	Gateway    *gateway.Gateway
}

func Novo(o Opcoes) (http.Handler, Servicos) {
	sv := Servicos{
		Usuarios:   usuarios.NovoServico(o.DB, o.Emissor),
		Servidores: servidores.NovoServico(o.DB, o.Publicador),
		Mensagens:  mensagens.NovoServico(o.DB, o.Publicador, o.Armazem, o.MaxAnexoMB),
	}
	u, s, m := sv.Usuarios, sv.Servidores, sv.Mensagens
	if o.Limitador != nil {
		m.UsarLimitador(o.Limitador)
	}
	if o.Presenca == nil {
		o.Presenca = presenca.NovoLocal()
	}
	sv.Gateway = &gateway.Gateway{
		DB: o.DB, Emissor: o.Emissor, Hub: o.Hub, Publicador: o.Publicador, Presenca: o.Presenca,
		Servidores: s, Usuarios: u, Origens: o.OrigensWS, Instancia: o.Instancia,
	}

	mux := http.NewServeMux()
	livre := func(padrao string, h api.Manipulador) { mux.Handle(padrao, h) }
	logado := func(padrao string, h api.Manipulador) { mux.Handle(padrao, api.ExigirLogin(o.Emissor, h)) }

	mux.Handle("POST /api/cadastro", limitarPorIP(api.Manipulador(u.Cadastrar), o.LoginPorMinuto))
	mux.Handle("POST /api/entrar", limitarPorIP(api.Manipulador(u.Entrar), o.LoginPorMinuto))
	mux.HandleFunc("GET /api/saude", func(w http.ResponseWriter, r *http.Request) {
		if err := o.DB.PingContext(r.Context()); err != nil {
			api.Responder(w, http.StatusServiceUnavailable, map[string]string{"estado": "sem banco"})
			return
		}
		api.Responder(w, http.StatusOK, map[string]any{"estado": "ok", "instancia": o.Instancia, "conexoes": sv.Gateway.Abertas()})
	})

	// conta
	logado("GET /api/eu", u.Eu)
	logado("PATCH /api/eu", u.AlterarPerfil)
	logado("GET /api/eu/mencoes", m.MinhasMencoes)

	// servidores
	logado("GET /api/servidores", s.Meus)
	logado("POST /api/servidores", s.Criar)
	logado("GET /api/servidores/{servidor}", s.RotaVisao)
	logado("PATCH /api/servidores/{servidor}", s.Alterar)
	logado("DELETE /api/servidores/{servidor}", s.Apagar)
	logado("DELETE /api/servidores/{servidor}/eu", s.Sair)
	logado("GET /api/servidores/{servidor}/busca", m.Buscar)
	logado("GET /api/servidores/{servidor}/auditoria", s.Auditoria)

	// canais e categorias
	logado("POST /api/servidores/{servidor}/canais", s.CriarCanal)
	logado("PATCH /api/servidores/{servidor}/canais/{canal}", s.AlterarCanal)
	logado("DELETE /api/servidores/{servidor}/canais/{canal}", s.ApagarCanal)
	logado("PUT /api/servidores/{servidor}/ordem-canais", s.OrdenarCanais)
	logado("POST /api/servidores/{servidor}/categorias", s.CriarCategoria)
	logado("PATCH /api/servidores/{servidor}/categorias/{categoria}", s.AlterarCategoria)
	logado("DELETE /api/servidores/{servidor}/categorias/{categoria}", s.ApagarCategoria)
	logado("GET /api/servidores/{servidor}/canais/{canal}/permissoes", s.Sobrescritas)
	logado("PUT /api/servidores/{servidor}/canais/{canal}/permissoes", s.DefinirSobrescrita)

	// cargos
	logado("GET /api/servidores/{servidor}/cargos", s.ListarCargos)
	logado("POST /api/servidores/{servidor}/cargos", s.CriarCargo)
	logado("PATCH /api/servidores/{servidor}/cargos/{cargo}", s.AlterarCargo)
	logado("DELETE /api/servidores/{servidor}/cargos/{cargo}", s.ApagarCargo)
	logado("PUT /api/servidores/{servidor}/ordem-cargos", s.OrdenarCargos)

	// membros e moderação
	logado("GET /api/servidores/{servidor}/membros", s.ListarMembros)
	logado("PATCH /api/servidores/{servidor}/membros/{usuario}/apelido", s.Apelido)
	logado("PUT /api/servidores/{servidor}/membros/{usuario}/cargos/{cargo}", s.DarCargo)
	logado("DELETE /api/servidores/{servidor}/membros/{usuario}/cargos/{cargo}", s.TirarCargo)
	logado("DELETE /api/servidores/{servidor}/membros/{usuario}", s.Expulsar)
	logado("PUT /api/servidores/{servidor}/membros/{usuario}/castigo", s.Castigar)
	logado("GET /api/servidores/{servidor}/banimentos", s.ListarBanidos)
	logado("PUT /api/servidores/{servidor}/banimentos/{usuario}", s.Banir)
	logado("DELETE /api/servidores/{servidor}/banimentos/{usuario}", s.Desbanir)

	// convites
	logado("GET /api/servidores/{servidor}/convites", s.ListarConvites)
	logado("POST /api/servidores/{servidor}/canais/{canal}/convites", s.CriarConvite)
	logado("DELETE /api/servidores/{servidor}/convites/{codigo}", s.ApagarConvite)
	livre("GET /api/convites/{codigo}", s.VerConvite)
	logado("POST /api/convites/{codigo}", s.AceitarConvite)

	// mensagens
	logado("GET /api/canais/{canal}/mensagens", m.Listar)
	logado("POST /api/canais/{canal}/mensagens", m.Enviar)
	logado("PATCH /api/canais/{canal}/mensagens/{mensagem}", m.Editar)
	logado("DELETE /api/canais/{canal}/mensagens/{mensagem}", m.Apagar)
	logado("PUT /api/canais/{canal}/mensagens/{mensagem}/reacoes/{emoji}", m.Reagir)
	logado("DELETE /api/canais/{canal}/mensagens/{mensagem}/reacoes/{emoji}", m.Desreagir)
	logado("POST /api/canais/{canal}/lido", m.MarcarLido)
	logado("POST /api/canais/{canal}/anexos", m.EnviarAnexo)
	// O <img> do navegador não manda o cabeçalho Authorization: o token também pode vir em ?t=
	mux.Handle("GET /api/anexos/{anexo}/{nome}", tokenNaURL(api.ExigirLogin(o.Emissor, api.Manipulador(m.BaixarAnexo))))

	mux.Handle("GET /api/gateway", sv.Gateway)
	if o.Voz != nil {
		sv.Gateway.ExtrasPronto = o.Voz.ExtrasPronto
		logado("GET /api/canais/{canal}/voz", o.Voz.Info)
		mux.HandleFunc("GET /api/voz/{canal}", o.Voz.Conectar)
	}
	for padrao, h := range o.Extras {
		mux.Handle(padrao, h)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		api.Responder(w, http.StatusNotFound, map[string]string{"erro": "Rota não encontrada."})
	})
	telas := o.Telas
	if telas == nil {
		telas = web.Telas()
	}
	mux.Handle("/", telas)

	return registrar(seguranca(mux)), sv
}

// tokenNaURL copia ?t=TOKEN para o cabeçalho Authorization (só na rota de anexos).
func tokenNaURL(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+t)
		}
		proximo.ServeHTTP(w, r)
	})
}

// seguranca põe os cabeçalhos que protegem o navegador.
func seguranca(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(self), microphone=(self), display-capture=(self), geolocation=()")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: https:; media-src 'self' blob: https:; "+
				"connect-src 'self' ws: wss: https:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; frame-ancestors 'none'; base-uri 'self'")
		}
		proximo.ServeHTTP(w, r)
	})
}

type gravador struct {
	http.ResponseWriter
	status int
}

func (g *gravador) WriteHeader(s int) { g.status = s; g.ResponseWriter.WriteHeader(s) }

// Unwrap deixa o WebSocket e o http.ResponseController acharem o ResponseWriter original.
func (g *gravador) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gravador) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	g.status = http.StatusSwitchingProtocols
	return http.NewResponseController(g.ResponseWriter).Hijack()
}

func (g *gravador) Flush() { _ = http.NewResponseController(g.ResponseWriter).Flush() }

// registrar anota no log as rotas da API que deram erro ou demoraram.
func registrar(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inicio := time.Now()
		g := &gravador{ResponseWriter: w, status: http.StatusOK}
		proximo.ServeHTTP(g, r)
		d := time.Since(inicio)
		conexaoLonga := g.status == http.StatusSwitchingProtocols // WebSocket: dura o tempo da conexão
		if strings.HasPrefix(r.URL.Path, "/api/") && !conexaoLonga && (g.status >= 500 || d > time.Second) {
			slog.Warn("rota", "metodo", r.Method, "caminho", r.URL.Path, "status", g.status, "duracao", d.Round(time.Millisecond))
		}
	})
}
