// Package usuarios cuida do cadastro, do login e do perfil.
package usuarios

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/api"
	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/eventos"
)

type Usuario struct {
	ID           int64     `json:"id"`
	NomeUsuario  string    `json:"nomeUsuario"`
	NomeExibicao string    `json:"nomeExibicao"`
	Email        string    `json:"email,omitempty"`
	CriadoEm     time.Time `json:"criadoEm"`
	Perfil
	Preferencias json.RawMessage `json:"preferencias,omitempty"` // só para a própria pessoa
}

type Servico struct {
	db      *sql.DB
	emissor *auth.Emissor
	hash    func(string) (string, error)
	armazem arquivos.Armazem
	pub     eventos.Publicador
	// AoMudarStatus avisa o gateway quando a pessoa entra ou sai do modo invisível.
	AoMudarStatus func(ctx context.Context, usuarioID int64, invisivel bool)
}

func NovoServico(db *sql.DB, emissor *auth.Emissor, armazem arquivos.Armazem, pub eventos.Publicador) *Servico {
	return &Servico{db: db, emissor: emissor, hash: auth.HashSenha, armazem: armazem, pub: pub}
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
	return s.responderComToken(w, r, http.StatusCreated, u)
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
	return s.responderComToken(w, r, http.StatusOK, u)
}

func (s *Servico) responderComToken(w http.ResponseWriter, r *http.Request, status int, u Usuario) error {
	if completo, err := s.Buscar(r.Context(), u.ID); err == nil {
		u = completo
	}
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
	var avatar, banner string
	var prefs []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, nome_usuario, nome_exibicao, email, criado_em, avatar, banner, cor_perfil, bio, pronomes,
		       status, status_texto, status_emoji, preferencias, visual
		FROM usuarios WHERE id = $1`, id).
		Scan(&u.ID, &u.NomeUsuario, &u.NomeExibicao, &u.Email, &u.CriadoEm, &avatar, &banner, &u.CorPerfil, &u.Bio,
			&u.Pronomes, &u.Status, &u.StatusTexto, &u.StatusEmoji, &prefs, &u.Visual)
	if errors.Is(err, sql.ErrNoRows) {
		return u, api.NovoErro(http.StatusUnauthorized, "Conta não encontrada. Entre de novo.")
	}
	u.Avatar, u.Banner, u.Preferencias = URLImagem(avatar), URLImagem(banner), prefs
	return u, err
}

// Eu devolve a própria conta (com e-mail e preferências).
func (s *Servico) Eu(w http.ResponseWriter, r *http.Request) error {
	u, err := s.Buscar(r.Context(), api.Quem(r).UsuarioID)
	if err != nil {
		return err
	}
	api.Responder(w, http.StatusOK, u)
	return nil
}
