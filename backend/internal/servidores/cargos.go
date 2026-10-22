package servidores

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

const maxCargos = 100

func (s *Servico) ListarCargos(w http.ResponseWriter, r *http.Request) error {
	_, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	lista, err := s.cargos(r.Context(), servidorID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

type pedidoCargo struct {
	Nome        *string `json:"nome"`
	Cor         *int    `json:"cor"`
	Permissoes  *uint64 `json:"permissoes"`
	Mencionavel *bool   `json:"mencionavel"`
}

// conferirPermissoesDadas impede dar a um cargo permissões que a pessoa não tem.
func conferirPermissoesDadas(c *acesso.Contexto, p uint64) error {
	pedidas := permissoes.Permissao(p) & permissoes.Todas
	if pedidas&^c.Base() != 0 {
		return api.NovoErro(http.StatusForbidden, "Você só pode dar a um cargo permissões que você mesmo tem.")
	}
	return nil
}

// CriarCargo cria um cargo logo acima do @todos.
func (s *Servico) CriarCargo(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCargos); err != nil {
		return err
	}
	var p pedidoCargo
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := "novo cargo"
	if p.Nome != nil {
		if nome, err = validarNome(*p.Nome, 1, 40, "O nome do cargo"); err != nil {
			return err
		}
	}
	var perm uint64
	if p.Permissoes != nil {
		if err := conferirPermissoesDadas(c, *p.Permissoes); err != nil {
			return err
		}
		perm = *p.Permissoes & uint64(permissoes.Todas)
	}
	cor := 0
	if p.Cor != nil {
		cor = *p.Cor & 0xFFFFFF
	}
	if len(c.Posicoes) >= maxCargos {
		return api.NovoErro(http.StatusBadRequest, "O servidor já tem 100 cargos, o máximo.")
	}
	var cargo Cargo
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		// abre espaço na posição 1 (logo acima do @todos)
		if _, err := tx.ExecContext(r.Context(), `UPDATE cargos SET posicao = posicao + 1 WHERE servidor_id = $1 AND NOT padrao`, servidorID); err != nil {
			return err
		}
		var permDB int64
		return tx.QueryRowContext(r.Context(), `
			INSERT INTO cargos (servidor_id, nome, cor, permissoes, posicao, mencionavel)
			VALUES ($1, $2, $3, $4, 1, $5)
			RETURNING id, servidor_id, nome, cor, permissoes, posicao, padrao, mencionavel`,
			servidorID, nome, cor, int64(perm), p.Mencionavel != nil && *p.Mencionavel).
			Scan(&cargo.ID, &cargo.ServidorID, &cargo.Nome, &cargo.Cor, &permDB, &cargo.Posicao, &cargo.Padrao, &cargo.Mencionavel)
	})
	if err != nil {
		return err
	}
	cargo.Permissoes = perm
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "cargo_criado", cargo.Nome, "")
	s.publicarCargos(r, servidorID, eventos.CargoCriado, cargo)
	api.Responder(w, http.StatusCreated, cargo)
	return nil
}

func (s *Servico) cargoDaRota(r *http.Request, servidorID int64) (Cargo, error) {
	cargoID, err := id(r, "cargo")
	if err != nil {
		return Cargo{}, err
	}
	var c Cargo
	var perm int64
	err = s.db.QueryRowContext(r.Context(), `
		SELECT id, servidor_id, nome, cor, permissoes, posicao, padrao, mencionavel
		FROM cargos WHERE id = $1 AND servidor_id = $2`, cargoID, servidorID).
		Scan(&c.ID, &c.ServidorID, &c.Nome, &c.Cor, &perm, &c.Posicao, &c.Padrao, &c.Mencionavel)
	if errors.Is(err, sql.ErrNoRows) {
		return c, api.NovoErro(http.StatusNotFound, "Cargo não encontrado.")
	}
	c.Permissoes = uint64(perm)
	return c, err
}

func (s *Servico) AlterarCargo(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCargos); err != nil {
		return err
	}
	cargo, err := s.cargoDaRota(r, servidorID)
	if err != nil {
		return err
	}
	// O @todos pode ser ajustado por quem gerencia cargos; os outros, só se estiverem abaixo.
	if !cargo.Padrao && !c.PodeMexerNoCargo(cargo.ID) {
		return acesso.ErrCargoAcima
	}
	var p pedidoCargo
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	if p.Nome != nil {
		if cargo.Padrao {
			return api.NovoErro(http.StatusBadRequest, "O nome do @todos não muda.")
		}
		if cargo.Nome, err = validarNome(*p.Nome, 1, 40, "O nome do cargo"); err != nil {
			return err
		}
	}
	if p.Cor != nil {
		cargo.Cor = *p.Cor & 0xFFFFFF
	}
	if p.Mencionavel != nil {
		cargo.Mencionavel = *p.Mencionavel
	}
	if p.Permissoes != nil {
		nova := *p.Permissoes & uint64(permissoes.Todas)
		// Dar ou tirar uma permissão do cargo exige tê-la (as que não mudam não importam).
		mudou := nova ^ cargo.Permissoes
		if err := conferirPermissoesDadas(c, mudou); err != nil {
			return err
		}
		cargo.Permissoes = nova
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE cargos SET nome = $2, cor = $3, permissoes = $4, mencionavel = $5 WHERE id = $1`,
		cargo.ID, cargo.Nome, cargo.Cor, int64(cargo.Permissoes), cargo.Mencionavel); err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "cargo_alterado", cargo.Nome, "")
	s.publicarCargos(r, servidorID, eventos.CargoAlterado, cargo)
	api.Responder(w, http.StatusOK, cargo)
	return nil
}

func (s *Servico) ApagarCargo(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCargos); err != nil {
		return err
	}
	cargo, err := s.cargoDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if cargo.Padrao {
		return api.NovoErro(http.StatusBadRequest, "O @todos não pode ser apagado.")
	}
	if !c.PodeMexerNoCargo(cargo.ID) {
		return acesso.ErrCargoAcima
	}
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM cargos WHERE id = $1`, cargo.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM permissoes_canal WHERE tipo = 'cargo' AND alvo_id = $1`, cargo.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(r.Context(), `UPDATE cargos SET posicao = posicao - 1 WHERE servidor_id = $1 AND posicao > $2`, servidorID, cargo.Posicao)
		return err
	})
	if err != nil {
		return err
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, "cargo_apagado", cargo.Nome, "")
	s.publicarCargos(r, servidorID, eventos.CargoApagado, map[string]int64{"id": cargo.ID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// OrdenarCargos recebe os ids dos cargos (sem o @todos) do mais alto para o mais baixo.
// Só os cargos abaixo do seu podem mudar de lugar, e nenhum pode subir acima do seu.
func (s *Servico) OrdenarCargos(w http.ResponseWriter, r *http.Request) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCargos); err != nil {
		return err
	}
	var ordem []int64
	if err := api.Ler(r, &ordem); err != nil {
		return err
	}
	if len(ordem) != len(c.Posicoes)-1 {
		return api.NovoErro(http.StatusBadRequest, "Mande todos os cargos (menos o @todos) na ordem nova.")
	}
	novas := map[int64]int{}
	for i, cargoID := range ordem {
		atual, ok := c.Posicoes[cargoID]
		if !ok || cargoID == c.Servidor.CargoTodosID {
			return api.NovoErro(http.StatusBadRequest, "Lista de cargos inválida.")
		}
		nova := len(ordem) - i
		if atual != nova && !c.Dono() {
			// cargos acima do seu não mudam, e nenhum pode passar a ficar acima do seu
			if atual >= c.PosicaoMaisAlta() || nova >= c.PosicaoMaisAlta() {
				return acesso.ErrCargoAcima
			}
		}
		novas[cargoID] = nova
	}
	err = s.emTransacao(r.Context(), func(tx *sql.Tx) error {
		for cargoID, pos := range novas {
			if _, err := tx.ExecContext(r.Context(), `UPDATE cargos SET posicao = $2 WHERE id = $1`, cargoID, pos); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	lista, err := s.cargos(r.Context(), servidorID)
	if err != nil {
		return err
	}
	for _, cargo := range lista {
		s.publicarCargos(r, servidorID, eventos.CargoAlterado, cargo)
	}
	api.Responder(w, http.StatusOK, lista)
	return nil
}

func (s *Servico) publicarCargos(r *http.Request, servidorID int64, tipo string, dados any) {
	s.publicar(r.Context(), eventos.Novo(tipo, servidorID, 0, dados))
}

// ---- cargos de um membro ----

// DarCargo e TirarCargo: PUT/DELETE /api/servidores/{servidor}/membros/{usuario}/cargos/{cargo}
func (s *Servico) DarCargo(w http.ResponseWriter, r *http.Request) error {
	return s.mudarCargoDoMembro(w, r, true)
}
func (s *Servico) TirarCargo(w http.ResponseWriter, r *http.Request) error {
	return s.mudarCargoDoMembro(w, r, false)
}

func (s *Servico) mudarCargoDoMembro(w http.ResponseWriter, r *http.Request, dar bool) error {
	c, servidorID, err := s.contexto(r)
	if err != nil {
		return err
	}
	if err := c.Exigir(permissoes.GerenciarCargos); err != nil {
		return err
	}
	cargo, err := s.cargoDaRota(r, servidorID)
	if err != nil {
		return err
	}
	if cargo.Padrao {
		return api.NovoErro(http.StatusBadRequest, "Todo mundo já tem o @todos.")
	}
	if !c.PodeMexerNoCargo(cargo.ID) {
		return acesso.ErrCargoAcima
	}
	usuarioID, err := id(r, "usuario")
	if err != nil {
		return err
	}
	alvo, err := acesso.CarregarMembro(r.Context(), s.db, servidorID, usuarioID)
	if err != nil {
		return api.NovoErro(http.StatusNotFound, "Membro não encontrado.")
	}
	// Mexer nos próprios cargos é permitido (abaixo do seu); nos dos outros, só se estiverem abaixo de você.
	if usuarioID != c.Membro.UsuarioID && !c.Dono() && !c.AcimaDe(alvo) {
		return acesso.ErrHierarquia
	}
	if dar {
		_, err = s.db.ExecContext(r.Context(),
			`INSERT INTO membros_cargos (servidor_id, usuario_id, cargo_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			servidorID, usuarioID, cargo.ID)
	} else {
		_, err = s.db.ExecContext(r.Context(),
			`DELETE FROM membros_cargos WHERE servidor_id = $1 AND usuario_id = $2 AND cargo_id = $3`, servidorID, usuarioID, cargo.ID)
	}
	if err != nil {
		return err
	}
	acao := "cargo_tirado"
	if dar {
		acao = "cargo_dado"
	}
	s.auditar(r.Context(), servidorID, c.Membro.UsuarioID, acao, cargo.Nome, "usuário "+itoa64(usuarioID))
	return s.publicarMembro(w, r, servidorID, usuarioID)
}
