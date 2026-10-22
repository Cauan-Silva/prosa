// Package api tem o que todas as rotas HTTP usam: JSON, erros e o login.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Cauan-Silva/prosa/internal/auth"
)

// Erro é uma falha que pode ser mostrada para quem chamou a API.
type Erro struct {
	Status   int
	Mensagem string
}

func (e *Erro) Error() string { return e.Mensagem }

func NovoErro(status int, mensagem string) *Erro { return &Erro{Status: status, Mensagem: mensagem} }

// Responder escreve o valor como JSON.
func Responder(w http.ResponseWriter, status int, valor any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if valor != nil {
		_ = json.NewEncoder(w).Encode(valor)
	}
}

// Falhar responde {"erro": "..."}; erros que não são *Erro viram 500 e vão para o log.
func Falhar(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		Responder(w, e.Status, map[string]string{"erro": e.Mensagem})
		return
	}
	slog.Error("erro inesperado", "rota", r.Method+" "+r.URL.Path, "erro", err)
	Responder(w, http.StatusInternalServerError, map[string]string{"erro": "Algo deu errado. Tente de novo."})
}

// Ler decodifica o corpo JSON (até 64 KB) recusando campos desconhecidos.
func Ler(r *http.Request, destino any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		return NovoErro(http.StatusBadRequest, "Dados inválidos.")
	}
	return nil
}

// Manipulador é uma rota que pode devolver erro (o Falhar cuida da resposta).
type Manipulador func(w http.ResponseWriter, r *http.Request) error

func (m Manipulador) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := m(w, r); err != nil {
		Falhar(w, r, err)
	}
}

type chaveIdentidade struct{}

// ExigirLogin só deixa passar quem mandou um token válido em "Authorization: Bearer ...".
func ExigirLogin(emissor *auth.Emissor, proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			Responder(w, http.StatusUnauthorized, map[string]string{"erro": "Entre na sua conta."})
			return
		}
		id, err := emissor.Conferir(strings.TrimSpace(token))
		if err != nil {
			Responder(w, http.StatusUnauthorized, map[string]string{"erro": "Sua sessão expirou. Entre de novo."})
			return
		}
		proximo.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chaveIdentidade{}, id)))
	})
}

// Quem devolve o usuário logado (só dentro de rotas com ExigirLogin).
func Quem(r *http.Request) auth.Identidade {
	id, _ := r.Context().Value(chaveIdentidade{}).(auth.Identidade)
	return id
}
