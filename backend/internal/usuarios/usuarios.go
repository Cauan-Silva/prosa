// Package usuarios cuida do cadastro, do login e do perfil.
package usuarios

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/auth"
)

type Usuario struct {
	ID           int64     `json:"id"`
	NomeUsuario  string    `json:"nomeUsuario"`
	NomeExibicao string    `json:"nomeExibicao"`
	Email        string    `json:"email,omitempty"`
	CriadoEm     time.Time `json:"criadoEm"`
}

type Servico struct {
	db      *sql.DB
	emissor *auth.Emissor
	hash    func(string) (string, error)
}

func NovoServico(db *sql.DB, emissor *auth.Emissor) *Servico {
	return &Servico{db: db, emissor: emissor, hash: auth.HashSenha}
}

var nomeUsuarioValido = regexp.MustCompile(`^[a-z0-9_.]{3,32}$`)

// Validar confere os campos do cadastro e devolve os valores limpos.
func Validar(nomeUsuario, nomeExibicao, email, senha string) (string, string, string, error) {
	nomeUsuario = strings.ToLower(strings.TrimSpace(nomeUsuario))
	nomeExibicao = strings.Join(strings.Fields(nomeExibicao), " ")
	email = strings.ToLower(strings.TrimSpace(email))
	switch {
	case !nomeUsuarioValido.MatchString(nomeUsuario) || strings.Contains(nomeUsuario, ".."):
		return "", "", "", api.NovoErro(http.StatusBadRequest, "O nome de usuário precisa ter de 3 a 32 caracteres: letras minúsculas, números, _ ou ponto.")
	case nomeExibicao != "" && utf8.RuneCountInString(nomeExibicao) > 40:
		return "", "", "", api.NovoErro(http.StatusBadRequest, "O nome de exibição pode ter até 40 caracteres.")
	case len(email) > 160 || !emailValido(email):
		return "", "", "", api.NovoErro(http.StatusBadRequest, "Informe um e-mail válido.")
	case utf8.RuneCountInString(senha) < 8:
		return "", "", "", api.NovoErro(http.StatusBadRequest, "A senha precisa ter pelo menos 8 caracteres.")
	case len(senha) > 200:
		return "", "", "", api.NovoErro(http.StatusBadRequest, "A senha é longa demais.")
	}
	if nomeExibicao == "" {
		nomeExibicao = nomeUsuario
	}
	return nomeUsuario, nomeExibicao, email, nil
}

func emailValido(email string) bool {
	e, err := mail.ParseAddress(email)
	return err == nil && e.Address == email && strings.Contains(email[strings.Index(email, "@"):], ".")
}

type pedidoCadastro struct {
	NomeUsuario  string `json:"nomeUsuario"`
	NomeExibicao string `json:"nomeExibicao"`
	Email        string `json:"email"`
	Senha        string `json:"senha"`
}

type pedidoLogin struct {
	Login string `json:"login"` // e-mail ou nome de usuário
	Senha string `json:"senha"`
}

func (s *Servico) Cadastrar(w http.ResponseWriter, r *http.Request) error {
	var p pedidoCadastro
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nomeUsuario, nomeExibicao, email, err := Validar(p.NomeUsuario, p.NomeExibicao, p.Email, p.Senha)
	if err != nil {
		return err
	}
	hash, err := s.hash(p.Senha)
	if err != nil {
		return err
	}
	var u Usuario
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO usuarios (nome_usuario, nome_exibicao, email, senha_hash) VALUES ($1, $2, $3, $4)
		RETURNING id, nome_usuario, nome_exibicao, email, criado_em`,
		nomeUsuario, nomeExibicao, email, hash).Scan(&u.ID, &u.NomeUsuario, &u.NomeExibicao, &u.Email, &u.CriadoEm)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		if strings.Contains(pqErr.Constraint, "nome") {
			return api.NovoErro(http.StatusConflict, "Esse nome de usuário já está em uso.")
		}
		return api.NovoErro(http.StatusConflict, "Já existe uma conta com esse e-mail.")
	}
	if err != nil {
		return err
	}
	return s.responderComToken(w, http.StatusCreated, u)
}

func (s *Servico) Entrar(w http.ResponseWriter, r *http.Request) error {
	var p pedidoLogin
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	var u Usuario
	var hash string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, nome_usuario, nome_exibicao, email, criado_em, senha_hash FROM usuarios
		WHERE lower(email) = lower($1) OR lower(nome_usuario) = lower($1)`,
		strings.TrimSpace(p.Login)).Scan(&u.ID, &u.NomeUsuario, &u.NomeExibicao, &u.Email, &u.CriadoEm, &hash)
	recusa := api.NovoErro(http.StatusUnauthorized, "Login ou senha incorretos.")
	if errors.Is(err, sql.ErrNoRows) {
		return recusa
	}
	if err != nil {
		return err
	}
	if ok, err := auth.ConferirSenha(p.Senha, hash); err != nil || !ok {
		return recusa
	}
	return s.responderComToken(w, http.StatusOK, u)
}

func (s *Servico) responderComToken(w http.ResponseWriter, status int, u Usuario) error {
	token, err := s.emissor.Emitir(auth.Identidade{UsuarioID: u.ID, Nome: u.NomeExibicao})
	if err != nil {
		return err
	}
	api.Responder(w, status, map[string]any{"token": token, "usuario": u})
	return nil
}

// Buscar devolve a pessoa logada (com e-mail).
func (s *Servico) Buscar(ctx context.Context, id int64) (Usuario, error) {
	var u Usuario
	err := s.db.QueryRowContext(ctx,
		`SELECT id, nome_usuario, nome_exibicao, email, criado_em FROM usuarios WHERE id = $1`, id).
		Scan(&u.ID, &u.NomeUsuario, &u.NomeExibicao, &u.Email, &u.CriadoEm)
	if errors.Is(err, sql.ErrNoRows) {
		return u, api.NovoErro(http.StatusUnauthorized, "Conta não encontrada. Entre de novo.")
	}
	return u, err
}

func (s *Servico) Eu(w http.ResponseWriter, r *http.Request) error {
	u, err := s.Buscar(r.Context(), api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, u)
	return nil
}

// AlterarPerfil muda o nome de exibição.
func (s *Servico) AlterarPerfil(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		NomeExibicao string `json:"nomeExibicao"`
	}
	if err := api.Ler(r, &p); err != nil {
		return err
	}
	nome := strings.Join(strings.Fields(p.NomeExibicao), " ")
	if n := utf8.RuneCountInString(nome); n < 1 || n > 40 {
		return api.NovoErro(http.StatusBadRequest, "O nome de exibição precisa ter de 1 a 40 caracteres.")
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE usuarios SET nome_exibicao = $2 WHERE id = $1`,
		api.Quem(r).UsuarioID, nome); err != nil {
		return err
	}
	return s.Eu(w, r)
}
