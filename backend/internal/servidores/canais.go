package servidores

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const maxCanais = 200

var espacos = regexp.MustCompile(`\s+`)

// NomeCanalTexto deixa o nome no formato de canal de texto: minúsculas e hífens ("dúvidas-de-cálculo").
func NomeCanalTexto(nome string) string {
	nome = strings.ToLower(strings.TrimSpace(nome))
	nome = espacos.ReplaceAllString(nome, "-")
	nome = strings.Trim(nome, "-")
	return nome
}

type pedidoCanal struct {
	Nome         *string `json:"nome"`
	Tipo         string  `json:"tipo"`
	CategoriaID  *int64  `json:"categoriaId"`
	Topico       *string `json:"topico"`
	ModoLento    *int    `json:"modoLento"`
	SemCategoria bool    `json:"semCategoria"` // para tirar o canal da categoria ao alterar
}

func (s *Servico) validarCategoria(r *http.Request, servidorID int64, categoriaID *int64) error {
	if categoriaID == nil {
		return nil
	}
	var existe bool
	err := s.db.QueryRowContext(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM categorias WHERE id = $1 AND servidor_id = $2)`, *categoriaID, servidorID).Scan(&existe)
	if err != nil {
		return err
	}
	if !existe {
		return api.NovoErro(http.StatusBadRequest, "Categoria não encontrada neste servidor.")
	}
	return nil
}

func validarCamposCanal(p *pedidoCanal, tipo string) error {
	if p.Nome != nil {
		nome := strings.Join(strings.Fields(*p.Nome), " ")
		if tipo == "texto" {
			nome = NomeCanalTexto(nome)
		}
		if n := utf8.RuneCountInString(nome); n < 1 || n > 40 {
			return api.NovoErro(http.StatusBadRequest, "O nome do canal precisa ter de 1 a 40 caracteres.")
		}
		p.Nome = &nome
	}
	if p.Topico != nil {
		t := strings.TrimSpace(*p.Topico)
		if utf8.RuneCountInString(t) > 300 {
			return api.NovoErro(http.StatusBadRequest, "O tópico pode ter até 300 caracteres.")
		}
		p.Topico = &t
	}
	if p.ModoLento != nil && (*p.ModoLento < 0 || *p.ModoLento > 21600) {
		return api.NovoErro(http.StatusBadRequest, "O modo lento vai de 0 a 21600 segundos (6 horas).")
	}
	return nil
}

func (s *Servico) CriarCanal(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCanais); err != nil {
		return err
	}
	var p pedidoCanal
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	if p.Tipo != "texto" && p.Tipo != "voz" {
		return api.NovoErro(http.StatusBadRequest, "O tipo do canal é \"texto\" ou \"voz\".")
	}
	if p.Nome == nil {
		return api.NovoErro(http.StatusBadRequest, "Dê um nome ao canal.")
	}
	if err := validarCamposCanal(&p, p.Tipo); err != nil {
		return err
	}
	if err := s.validarCategoria(r, servidorID, p.CategoriaID); err != nil {
		return err
	}
	if len(c.Canais) >= maxCanais {
		return api.NovoErro(http.StatusBadRequest, "O servidor já tem 200 canais, o máximo.")
	}
	topico, lento := "", 0
	if p.Topico != nil {
		topico = *p.Topico
	}
	if p.ModoLento != nil {
		lento = *p.ModoLento
	}
	canal, err := lerCanal(s.db.QueryRowContext(r.Context(), `
		INSERT INTO canais (servidor_id, categoria_id, nome, tipo, topico, modo_lento, posicao)
		VALUES ($1, $2, $3, $4, $5, $6, (SELECT COALESCE(max(posicao) + 1, 0) FROM canais WHERE servidor_id = $1))
		RETURNING `+colunasCanal, servidorID, p.CategoriaID, *p.Nome, p.Tipo, topico, lento))
	if err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "canal_criado", canal.Nome, canal.Tipo)
	s.publicar(r.Context(), eventos.Novo(eventos.CanalCriado, servidorID, canal.ID, canal))
	api.Responder(w, http.StatusCreated, canal)
	return nil
}

// canalDaRota busca o canal da URL e confere que é do servidor da URL.
func (s *Servico) canalDaRota(r *http.Request, servidorID int64) (Canal, error) {
	canalID, err := id(r, "canal")
	if err != nil {
		return Canal{}, err
	}
	canal, err := lerCanal(s.db.QueryRowContext(r.Context(),
		`SELECT `+colunasCanal+` FROM canais WHERE id = $1 AND servidor_id = $2 AND tipo <> 'fio'`, canalID, servidorID))
	if errors.Is(err, sql.ErrNoRows) {
		return canal, api.NovoErro(http.StatusNotFound, "Canal não encontrado.")
	}
	return canal, err
}

func (s *Servico) AlterarCanal(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	canal, err := s.canalDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canal.ID, permissoes.GerenciarCanais); err != nil {
		return err
	}
	var p pedidoCanal
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	if err := validarCamposCanal(&p, canal.Tipo); err != nil {
		return err
	}
	if err := s.validarCategoria(r, servidorID, p.CategoriaID); err != nil {
		return err
	}
	if p.Nome != nil {
		canal.Nome = *p.Nome
	}
	if p.Topico != nil {
		canal.Topico = *p.Topico
	}
	if p.ModoLento != nil {
		canal.ModoLento = *p.ModoLento
	}
	if p.CategoriaID != nil {
		canal.CategoriaID = p.CategoriaID
	} else if p.SemCategoria {
		canal.CategoriaID = nil
	}
	canal, err = lerCanal(s.db.QueryRowContext(r.Context(), `
		UPDATE canais SET nome = $2, topico = $3, modo_lento = $4, categoria_id = $5 WHERE id = $1
		RETURNING `+colunasCanal, canal.ID, canal.Nome, canal.Topico, canal.ModoLento, canal.CategoriaID))
	if err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "canal_alterado", canal.Nome, "")
	s.publicar(r.Context(), eventos.Novo(eventos.CanalAlterado, servidorID, canal.ID, canal))
	api.Responder(w, http.StatusOK, canal)
	return nil
}

func (s *Servico) ApagarCanal(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	canal, err := s.canalDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canal.ID, permissoes.GerenciarCanais); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM canais WHERE id = $1`, canal.ID); err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "canal_apagado", canal.Nome, "")
	s.publicar(r.Context(), eventos.Novo(eventos.CanalApagado, servidorID, canal.ID, map[string]int64{"id": canal.ID}))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Ordenar recebe a lista de canais na ordem nova (com a categoria de cada um).
func (s *Servico) OrdenarCanais(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCanais); err != nil {
		return err
	}
	var p []struct {
		ID          int64  `json:"id"`
		CategoriaID *int64 `json:"categoriaId"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		for i, item := range p {
			if item.CategoriaID != nil {
				if err := s.validarCategoria(r, servidorID, item.CategoriaID); err != nil {
					return err
				}
			}
			res, err := tx.ExecContext(r.Context(),
				`UPDATE canais SET posicao = $3, categoria_id = $4 WHERE id = $1 AND servidor_id = $2`,
				item.ID, servidorID, i, item.CategoriaID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return api.NovoErro(http.StatusBadRequest, "Um dos canais não é deste servidor.")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	canais, err := s.canais(r.Context(), servidorID)
	if err != nil {
		return err
	}
	for _, canal := range canais {
		s.publicar(r.Context(), eventos.Novo(eventos.CanalAlterado, servidorID, canal.ID, canal))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- categorias ----

func (s *Servico) CriarCategoria(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCanais); err != nil {
		return err
	}
	var p struct {
		Nome string `json:"nome"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome, err := validarNome(p.Nome, 1, 40, "O nome da categoria")
	if err != nil {
		return err
	}
	var cat Categoria
	if err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO categorias (servidor_id, nome, posicao)
		VALUES ($1, $2, (SELECT COALESCE(max(posicao) + 1, 0) FROM categorias WHERE servidor_id = $1))
		RETURNING id, servidor_id, nome, posicao`, servidorID, nome).Scan(&cat.ID, &cat.ServidorID, &cat.Nome, &cat.Posicao); err != nil {
		return err
	}
	s.publicar(r.Context(), eventos.Novo(eventos.CategoriaCriada, servidorID, 0, cat))
	api.Responder(w, http.StatusCreated, cat)
	return nil
}

func (s *Servico) AlterarCategoria(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCanais); err != nil {
		return err
	}
	categoriaID, err := id(r, "categoria")
	if err != nil {
		return err
	}
	var p struct {
		Nome    *string `json:"nome"`
		Posicao *int    `json:"posicao"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	var cat Categoria
	err = s.db.QueryRowContext(r.Context(),
		`SELECT id, servidor_id, nome, posicao FROM categorias WHERE id = $1 AND servidor_id = $2`, categoriaID, servidorID).
		Scan(&cat.ID, &cat.ServidorID, &cat.Nome, &cat.Posicao)
	if errors.Is(err, sql.ErrNoRows) {
		return api.NovoErro(http.StatusNotFound, "Categoria não encontrada.")
	}
	if err != nil {
		return err
	}
	if p.Nome != nil {
		if cat.Nome, err = validarNome(*p.Nome, 1, 40, "O nome da categoria"); err != nil {
			return err
		}
	}
	if p.Posicao != nil {
		cat.Posicao = *p.Posicao
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE categorias SET nome = $2, posicao = $3 WHERE id = $1`, cat.ID, cat.Nome, cat.Posicao); err != nil {
		return err
	}
	s.publicar(r.Context(), eventos.Novo(eventos.CategoriaAlterada, servidorID, 0, cat))
	api.Responder(w, http.StatusOK, cat)
	return nil
}

// ApagarCategoria apaga a categoria; os canais dela ficam sem categoria.
func (s *Servico) ApagarCategoria(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCanais); err != nil {
		return err
	}
	categoriaID, err := id(r, "categoria")
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM categorias WHERE id = $1 AND servidor_id = $2`, categoriaID, servidorID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return api.NovoErro(http.StatusNotFound, "Categoria não encontrada.")
	}
	s.publicar(r.Context(), eventos.Novo(eventos.CategoriaApagada, servidorID, 0, map[string]int64{"id": categoriaID}))
	// os canais que estavam nela mudaram (categoria vazia)
	canais, err := s.canais(r.Context(), servidorID)
	if err == nil {
		for _, canal := range canais {
			if canal.CategoriaID == nil {
				s.publicar(r.Context(), eventos.Novo(eventos.CanalAlterado, servidorID, canal.ID, canal))
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- ajustes de permissão do canal ----

type Sobrescrita struct {
	Tipo     string `json:"tipo"`
	AlvoID   int64  `json:"alvoId"`
	Permitir uint64 `json:"permitir"`
	Negar    uint64 `json:"negar"`
}

func (s *Servico) Sobrescritas(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	canal, err := s.canalDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canal.ID, permissoes.GerenciarCanais); err != nil {
		return err
	}
	lista := []Sobrescrita{}
	for _, o := range c.Sobrescritas[canal.ID] {
		lista = append(lista, Sobrescrita{Tipo: o.Tipo, AlvoID: o.AlvoID, Permitir: uint64(o.Permitir), Negar: uint64(o.Negar)})
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

// DefinirSobrescrita cria ou troca o ajuste de um cargo ou membro no canal.
// Ninguém pode dar num canal uma permissão que não tem (exceto o dono e administradores).
func (s *Servico) DefinirSobrescrita(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	canal, err := s.canalDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if err := c.ExigirNoCanal(canal.ID, permissoes.GerenciarCanais); err != nil {
		return err
	}
	var p Sobrescrita
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	if err := s.validarAlvo(r, c.ServidorID, p.Tipo, p.AlvoID); err != nil {
		return err
	}
	permitir, negar := permissoes.Permissao(p.Permitir)&permissoes.Todas, permissoes.Permissao(p.Negar)&permissoes.Todas
	if permitir&negar != 0 {
		return api.NovoErro(http.StatusBadRequest, "Uma permissão não pode ser permitida e negada ao mesmo tempo.")
	}
	meu := c.NoCanal(canal.ID)
	if (permitir|negar)&^meu != 0 {
		return api.NovoErro(http.StatusForbidden, "Você só pode ajustar permissões que você mesmo tem neste canal.")
	}
	if p.Tipo == "cargo" && !c.PodeMexerNoCargo(p.AlvoID) && p.AlvoID != c.Servidor.CargoTodosID {
		return api.NovoErro(http.StatusForbidden, "Você só ajusta cargos abaixo do seu cargo mais alto.")
	}
	if permitir == 0 && negar == 0 {
		_, err = s.db.ExecContext(r.Context(), `DELETE FROM permissoes_canal WHERE canal_id = $1 AND tipo = $2 AND alvo_id = $3`, canal.ID, p.Tipo, p.AlvoID)
	} else {
		_, err = s.db.ExecContext(r.Context(), `
			INSERT INTO permissoes_canal (canal_id, tipo, alvo_id, permitir, negar) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (canal_id, tipo, alvo_id) DO UPDATE SET permitir = EXCLUDED.permitir, negar = EXCLUDED.negar`,
			canal.ID, p.Tipo, p.AlvoID, int64(permitir), int64(negar))
	}
	if err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "permissoes_canal", canal.Nome, p.Tipo+" "+itoa64(p.AlvoID))
	s.publicar(r.Context(), eventos.Novo(eventos.CanalAlterado, servidorID, canal.ID, canal))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Servico) validarAlvo(r *http.Request, servidorID int64, tipo string, alvoID int64) error {
	var consulta string
	switch tipo {
	case "cargo":
		consulta = `SELECT EXISTS (SELECT 1 FROM cargos WHERE id = $1 AND servidor_id = $2)`
	case "membro":
		consulta = `SELECT EXISTS (SELECT 1 FROM membros WHERE usuario_id = $1 AND servidor_id = $2)`
	default:
		return api.NovoErro(http.StatusBadRequest, "O ajuste é para um \"cargo\" ou um \"membro\".")
	}
	var existe bool
	if err := s.db.QueryRowContext(r.Context(), consulta, alvoID, servidorID).Scan(&existe); err != nil {
		return err
	}
	if !existe {
		return api.NovoErro(http.StatusBadRequest, "Cargo ou membro não encontrado neste servidor.")
	}
	return nil
}
