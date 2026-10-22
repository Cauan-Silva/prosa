// Package previas busca o título, a descrição e a imagem de um link (as tags Open Graph
// do site) para mostrar um cartão embaixo da mensagem.
//
// Buscar uma URL que qualquer pessoa escolheu é perigoso: alguém poderia usar o servidor
// para acessar endereços internos (o banco, o painel da hospedagem). Por isso a conexão
// só é aberta para IPs públicos, conferidos depois da resolução do DNS e de novo a cada
// redirecionamento, só nas portas 80 e 443, com tempo e tamanho limitados.
package previas

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/Cauan-Silva/prosa/internal/api"
)

const (
	maxCorpo       = 768 << 10 // só o começo da página: as tags ficam no <head>
	validadeBoa    = 7 * 24 * time.Hour
	validadeRuim   = 6 * time.Hour
	pedidosPorMin  = 30
	tamanhoMaxURL  = 2048
	agenteDoPedido = "Mozilla/5.0 (compatible; ProsaBot/1.0; +https://github.com/Cauan-Silva/prosa)"
)

// Previa é o cartão de um link.
type Previa struct {
	URL       string `json:"url"`
	Titulo    string `json:"titulo"`
	Descricao string `json:"descricao"`
	Imagem    string `json:"imagem"`
	Site      string `json:"site"`
	Cor       string `json:"cor"`
}

type Servico struct {
	db       *sql.DB
	cliente  *http.Client
	agora    func() time.Time
	mu       sync.Mutex
	pedidos  map[int64][]time.Time
	buscando sync.Map // url -> *sync.Mutex (duas pessoas abrindo o mesmo link buscam uma vez)
}

// Novo cria o serviço. permitirPrivados só serve para testes (servidor de teste em 127.0.0.1).
func Novo(db *sql.DB, permitirPrivados bool) *Servico {
	discador := &net.Dialer{Timeout: 4 * time.Second, Control: controle(permitirPrivados)}
	transporte := &http.Transport{
		DialContext:           discador.DialContext,
		Proxy:                 nil, // sempre direto: o controle de IP precisa valer
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
	}
	cliente := &http.Client{
		Transport: transporte,
		Timeout:   8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("redirecionamentos demais")
			}
			return conferirURL(req.URL)
		},
	}
	return &Servico{db: db, cliente: cliente, agora: time.Now, pedidos: map[int64][]time.Time{}}
}

// controle roda antes de cada conexão, já com o IP resolvido: recusa endereços internos.
func controle(permitirPrivados bool) func(rede, endereco string, _ syscall.RawConn) error {
	return func(rede, endereco string, _ syscall.RawConn) error {
		host, porta, err := net.SplitHostPort(endereco)
		if err != nil {
			return err
		}
		if porta != "80" && porta != "443" && !permitirPrivados {
			return errors.New("porta não permitida")
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.New("endereço inválido")
		}
		if !permitirPrivados && !ipPublico(ip) {
			return errors.New("endereço interno não permitido")
		}
		return nil
	}
}

func ipPublico(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 (CGNAT, redes internas de nuvem), 0.0.0.0/8, 192.0.0.0/24, 198.18.0.0/15, 240.0.0.0/4
		switch {
		case v4[0] == 0, v4[0] == 100 && v4[1]&0xc0 == 64, v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), v4[0] >= 240:
			return false
		}
	} else if ip[0] == 0xfc || ip[0] == 0xfd { // fc00::/7
		return false
	}
	return true
}

func conferirURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("só links http e https")
	}
	if u.User != nil || u.Hostname() == "" {
		return errors.New("link inválido")
	}
	return nil
}

// Rota: GET /api/previa?url=...
func (s *Servico) Rota(w http.ResponseWriter, r *http.Request) error {
	bruta := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(bruta)
	if err != nil || len(bruta) > tamanhoMaxURL || conferirURL(u) != nil {
		return api.NovoErro(http.StatusBadRequest, "Link inválido.")
	}
	u.Fragment = ""
	chave := u.String()

	if p, ok, achou := s.doCache(r.Context(), chave); achou {
		return responder(w, p, ok)
	}
	if !s.permitir(api.Quem(r).UsuarioID) {
		return api.NovoErro(http.StatusTooManyRequests, "Calma! Muitas prévias de link em pouco tempo.")
	}
	trava, _ := s.buscando.LoadOrStore(chave, &sync.Mutex{})
	trava.(*sync.Mutex).Lock()
	defer trava.(*sync.Mutex).Unlock()
	if p, ok, achou := s.doCache(r.Context(), chave); achou { // outra pessoa buscou enquanto esperava
		return responder(w, p, ok)
	}

	ctx, cancelar := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancelar()
	p, err := s.buscar(ctx, u)
	ok := err == nil && (p.Titulo != "" || p.Descricao != "" || p.Imagem != "")
	if err != nil {
		slog.Debug("prévia de link", "url", chave, "erro", err)
	}
	p.URL = chave
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO previas_link (url, ok, titulo, descricao, imagem, site, cor, buscada_em)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (url) DO UPDATE SET ok = $2, titulo = $3, descricao = $4, imagem = $5, site = $6, cor = $7, buscada_em = now()`,
		chave, ok, p.Titulo, p.Descricao, p.Imagem, p.Site, p.Cor); err != nil {
		slog.Warn("prévia de link: guardar", "erro", err)
	}
	return responder(w, p, ok)
}

func responder(w http.ResponseWriter, p Previa, ok bool) error {
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if !ok {
		w.WriteHeader(http.StatusNoContent) // sem prévia: o navegador só mostra o link
		return nil
	}
	api.Responder(w, http.StatusOK, p)
	return nil
}

func (s *Servico) doCache(ctx context.Context, chave string) (Previa, bool, bool) {
	var p Previa
	var ok bool
	var quando time.Time
	err := s.db.QueryRowContext(ctx, `SELECT url, ok, titulo, descricao, imagem, site, cor, buscada_em FROM previas_link WHERE url = $1`, chave).
		Scan(&p.URL, &ok, &p.Titulo, &p.Descricao, &p.Imagem, &p.Site, &p.Cor, &quando)
	if err != nil {
		return p, false, false
	}
	validade := validadeBoa
	if !ok {
		validade = validadeRuim
	}
	return p, ok, s.agora().Sub(quando) < validade
}

func (s *Servico) permitir(usuarioID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	agora := s.agora()
	recentes := s.pedidos[usuarioID][:0]
	for _, t := range s.pedidos[usuarioID] {
		if agora.Sub(t) < time.Minute {
			recentes = append(recentes, t)
		}
	}
	if len(recentes) >= pedidosPorMin {
		s.pedidos[usuarioID] = recentes
		return false
	}
	s.pedidos[usuarioID] = append(recentes, agora)
	if len(s.pedidos) > 10000 { // não deixa o mapa crescer para sempre
		s.pedidos = map[int64][]time.Time{}
	}
	return true
}

func (s *Servico) buscar(ctx context.Context, u *url.URL) (Previa, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Previa{}, err
	}
	req.Header.Set("User-Agent", agenteDoPedido)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.5")
	resp, err := s.cliente.Do(req)
	if err != nil {
		return Previa{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Previa{}, errors.New("status " + resp.Status)
	}
	final := resp.Request.URL
	tipo := resp.Header.Get("Content-Type")
	if strings.HasPrefix(tipo, "image/") {
		// link direto para uma imagem: a própria imagem é a prévia
		return Previa{Imagem: final.String(), Site: final.Hostname()}, nil
	}
	if !strings.Contains(tipo, "html") {
		return Previa{}, errors.New("não é uma página")
	}
	p := lerPagina(io.LimitReader(resp.Body, maxCorpo), final)
	if p.Site == "" {
		p.Site = strings.TrimPrefix(final.Hostname(), "www.")
	}
	return p, nil
}

// lerPagina procura as tags Open Graph / Twitter e, na falta delas, o <title>.
func lerPagina(r io.Reader, base *url.URL) Previa {
	var p Previa
	var tituloTag, descricaoMeta string
	z := html.NewTokenizer(r)
	noTitulo := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			goto fim
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "title":
				noTitulo = tituloTag == ""
			case "body":
				goto fim // o <head> acabou
			case "meta":
				var chave, valor string
				for _, a := range t.Attr {
					switch strings.ToLower(a.Key) {
					case "property", "name":
						chave = strings.ToLower(a.Val)
					case "content":
						valor = strings.TrimSpace(a.Val)
					}
				}
				if valor == "" {
					continue
				}
				switch chave {
				case "og:title":
					p.Titulo = valor
				case "twitter:title":
					if p.Titulo == "" {
						p.Titulo = valor
					}
				case "og:description":
					p.Descricao = valor
				case "twitter:description":
					if p.Descricao == "" {
						p.Descricao = valor
					}
				case "description":
					descricaoMeta = valor
				case "og:image", "og:image:url", "og:image:secure_url":
					if p.Imagem == "" {
						p.Imagem = valor
					}
				case "twitter:image", "twitter:image:src":
					if p.Imagem == "" {
						p.Imagem = valor
					}
				case "og:site_name":
					p.Site = valor
				case "theme-color":
					p.Cor = valor
				}
			}
		case html.TextToken:
			if noTitulo {
				tituloTag += string(z.Text())
			}
		case html.EndTagToken:
			if t, _ := z.TagName(); string(t) == "title" {
				noTitulo = false
			} else if string(t) == "head" {
				goto fim
			}
		}
	}
fim:
	if p.Titulo == "" {
		p.Titulo = strings.Join(strings.Fields(tituloTag), " ")
	}
	if p.Descricao == "" {
		p.Descricao = descricaoMeta
	}
	if p.Imagem != "" {
		if img, err := base.Parse(p.Imagem); err == nil && (img.Scheme == "https" || img.Scheme == "http") {
			p.Imagem = img.String()
		} else {
			p.Imagem = ""
		}
	}
	if len(p.Cor) > 20 || !strings.HasPrefix(p.Cor, "#") {
		p.Cor = ""
	}
	p.Titulo = cortar(p.Titulo, 300)
	p.Descricao = cortar(p.Descricao, 500)
	p.Site = cortar(p.Site, 100)
	if len(p.Imagem) > tamanhoMaxURL {
		p.Imagem = ""
	}
	return p
}

func cortar(texto string, max int) string {
	if utf8.RuneCountInString(texto) <= max {
		return texto
	}
	r := []rune(texto)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
