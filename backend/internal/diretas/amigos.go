package diretas

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Cauan-Silva/prosa/internal/api"
)

// ListaAmigos é tudo o que a aba "Amigos" mostra.
type ListaAmigos struct {
	Amigos     []Pessoa `json:"amigos"`
	Recebidos  []Pessoa `json:"recebidos"`
	Enviados   []Pessoa `json:"enviados"`
	Bloqueados []Pessoa `json:"bloqueados"`
}

// Listar: GET /api/amigos
func (s *Servico) Listar(w http.ResponseWriter, r *http.Request) error {
	eu := api.Quem(r).UsuarioID
	saida := ListaAmigos{Amigos: []Pessoa{}, Recebidos: []Pessoa{}, Enviados: []Pessoa{}, Bloqueados: []Pessoa{}}
	linhas, err := s.db.QueryContext(r.Context(), `
		SELECT `+colunasPessoa+`, a.estado, a.de_id = $1
		FROM amizades a JOIN usuarios u ON u.id = CASE WHEN a.de_id = $1 THEN a.para_id ELSE a.de_id END
		WHERE a.de_id = $1 OR a.para_id = $1
		ORDER BY lower(u.nome_exibicao)`, eu)
	if err != nil {
		return err
	}
	for linhas.Next() {
		var estado string
		var enviei bool
		p, err := lerPessoa(linhas, &estado, &enviei)
		if err != nil {
			linhas.Close()
			return err
		}
		switch {
		case estado == "aceita":
			saida.Amigos = append(saida.Amigos, p)
		case enviei:
			saida.Enviados = append(saida.Enviados, p)
		default:
			saida.Recebidos = append(saida.Recebidos, p)
		}
	}
	linhas.Close()
	linhas, err = s.db.QueryContext(r.Context(), `
		SELECT `+colunasPessoa+` FROM bloqueios b JOIN usuarios u ON u.id = b.bloqueado_id
		WHERE b.quem_id = $1 ORDER BY lower(u.nome_exibicao)`, eu)
	if err != nil {
		return err
	}
	defer linhas.Close()
	for linhas.Next() {
		p, err := lerPessoa(linhas)
		if err != nil {
			return err
		}
		saida.Bloqueados = append(saida.Bloqueados, p)
	}
	if err := linhas.Err(); err != nil {
		return err
	}
	s.marcarOnline(r.Context(), saida.Amigos)
	for _, lista := range [][]Pessoa{saida.Recebidos, saida.Enviados, saida.Bloqueados} {
		for i := range lista {
			if lista[i].Status == "invisivel" {
				lista[i].Status = "online"
			}
		}
	}
	api.Responder(w, http.StatusOK, saida)
	return nil
}

// PedirPorNome: POST /api/amigos {"nomeUsuario": "bia"}
func (s *Servico) PedirPorNome(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		NomeUsuario string `json:"nomeUsuario"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := strings.TrimPrefix(strings.TrimSpace(p.NomeUsuario), "@")
	if nome == "" {
		return api.NovoErro(http.StatusBadRequest, "Digite o nome de usuário da pessoa.")
	}
	var alvo int64
	err := s.db.QueryRowContext(r.Context(), `SELECT id FROM usuarios WHERE lower(nome_usuario) = lower($1)`, nome).Scan(&alvo)
	if errors.Is(err, sql.ErrNoRows) {
		return api.NovoErro(http.StatusNotFound, "Ninguém com esse nome de usuário. Confira se digitou certinho (é o @nome, não o nome de exibição).")
	}
	if err != nil {
		return err
	}
	return s.pedir(w, r, alvo)
}

// Pedir: PUT /api/amigos/{usuario} — manda o pedido, ou aceita se a pessoa já tinha pedido.
func (s *Servico) Pedir(w http.ResponseWriter, r *http.Request) error {
	alvo, err := idDaRota(r, "usuario")
	if err != nil {
		return err
	}
	return s.pedir(w, r, alvo)
}

func (s *Servico) pedir(w http.ResponseWriter, r *http.Request, alvo int64) error {
	ctx := r.Context()
	eu := api.Quem(r).UsuarioID
	if alvo == eu {
		return api.NovoErro(http.StatusBadRequest, "Você não pode adicionar a si mesmo.")
	}
	if bloq, err := s.bloqueio(ctx, eu, alvo); err != nil {
		return err
	} else if bloq {
		// não revela quem bloqueou quem
		return api.NovoErro(http.StatusForbidden, "Não foi possível mandar o pedido para essa pessoa.")
	}
	var de, estado string
	err := s.db.QueryRowContext(ctx, `
		SELECT CASE WHEN de_id = $1 THEN 'eu' ELSE 'outro' END, estado FROM amizades
		WHERE (de_id = $1 AND para_id = $2) OR (de_id = $2 AND para_id = $1)`, eu, alvo).Scan(&de, &estado)
	resultado := ""
	switch {
	case err == nil && estado == "aceita":
		resultado = "amigos"
	case err == nil && de == "eu":
		resultado = "enviado"
	case err == nil: // a outra pessoa já tinha pedido: aceitar
		if _, err := s.db.ExecContext(ctx, `UPDATE amizades SET estado = 'aceita' WHERE de_id = $1 AND para_id = $2`, alvo, eu); err != nil {
			return err
		}
		resultado = "amigos"
	case errors.Is(err, sql.ErrNoRows):
		var abertos int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM amizades WHERE de_id = $1 AND estado = 'pendente'`, eu).Scan(&abertos); err != nil {
			return err
		}
		if abertos >= maxPedidosAbertos {
			return api.NovoErro(http.StatusBadRequest, "Você tem 100 pedidos esperando resposta. Cancele alguns antes.")
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO amizades (de_id, para_id, estado) VALUES ($1, $2, 'pendente') ON CONFLICT DO NOTHING`, eu, alvo); err != nil {
			return err
		}
		resultado = "enviado"
	default:
		return err
	}
	s.avisar(ctx, AmigoAlterado, 0, map[string]any{"de": eu, "para": alvo}, eu, alvo)
	api.Responder(w, http.StatusOK, map[string]string{"resultado": resultado})
	return nil
}

// Remover: DELETE /api/amigos/{usuario} — desfaz a amizade, recusa ou cancela o pedido.
func (s *Servico) Remover(w http.ResponseWriter, r *http.Request) error {
	alvo, err := idDaRota(r, "usuario")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	if err := s.desfazer(r.Context(), eu, alvo); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Servico) desfazer(ctx context.Context, eu, alvo int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM amizades WHERE (de_id = $1 AND para_id = $2) OR (de_id = $2 AND para_id = $1)`, eu, alvo)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.avisar(ctx, AmigoAlterado, 0, map[string]any{"de": eu, "para": alvo}, eu, alvo)
	}
	return nil
}

// Bloquear: PUT /api/bloqueios/{usuario} — desfaz a amizade e impede pedidos e mensagens diretas.
func (s *Servico) Bloquear(w http.ResponseWriter, r *http.Request) error {
	alvo, err := idDaRota(r, "usuario")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	if alvo == eu {
		return api.NovoErro(http.StatusBadRequest, "Você não pode bloquear a si mesmo.")
	}
	var existe bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS (SELECT 1 FROM usuarios WHERE id = $1)`, alvo).Scan(&existe); err != nil {
		return err
	}
	if !existe {
		return api.NovoErro(http.StatusNotFound, "Pessoa não encontrada.")
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO bloqueios (quem_id, bloqueado_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, eu, alvo); err != nil {
		return err
	}
	if err := s.desfazer(r.Context(), eu, alvo); err != nil {
		return err
	}
	// a DM fica só para ler: quem bloqueou e quem foi bloqueado recarregam as conversas
	s.avisar(r.Context(), ConversaAlterada, 0, nil, eu, alvo)
	s.avisar(r.Context(), AmigoAlterado, 0, nil, eu)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Desbloquear: DELETE /api/bloqueios/{usuario}
func (s *Servico) Desbloquear(w http.ResponseWriter, r *http.Request) error {
	alvo, err := idDaRota(r, "usuario")
	if err != nil {
		return err
	}
	eu := api.Quem(r).UsuarioID
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM bloqueios WHERE quem_id = $1 AND bloqueado_id = $2`, eu, alvo); err != nil {
		return err
	}
	s.avisar(r.Context(), ConversaAlterada, 0, nil, eu, alvo)
	s.avisar(r.Context(), AmigoAlterado, 0, nil, eu)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
