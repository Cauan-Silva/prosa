package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Duração do login: depois disso a pessoa entra de novo.
const DuracaoToken = 30 * 24 * time.Hour

var (
	ErrTokenInvalido = errors.New("token inválido")
	ErrTokenVencido  = errors.New("token vencido")

	b64url = base64.RawURLEncoding
	// Cabeçalho fixo: só aceitamos HS256 (nada de "alg": "none").
	cabecalho = b64url.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
)

type dadosToken struct {
	Sub  string `json:"sub"`
	Nome string `json:"nome"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// Identidade é quem está logado.
type Identidade struct {
	UsuarioID int64
	Nome      string
}

// Emissor cria e confere tokens com um segredo.
type Emissor struct {
	segredo []byte
	agora   func() time.Time
}

func NovoEmissor(segredo []byte) *Emissor {
	return &Emissor{segredo: segredo, agora: time.Now}
}

// Emitir gera um JWT HS256 para o usuário.
func (e *Emissor) Emitir(id Identidade) (string, error) {
	agora := e.agora()
	corpo, err := json.Marshal(dadosToken{
		Sub:  strconv.FormatInt(id.UsuarioID, 10),
		Nome: id.Nome,
		Iat:  agora.Unix(),
		Exp:  agora.Add(DuracaoToken).Unix(),
	})
	if err != nil {
		return "", err
	}
	semAssinatura := cabecalho + "." + b64url.EncodeToString(corpo)
	return semAssinatura + "." + e.assinar(semAssinatura), nil
}

// Conferir valida a assinatura e a validade e devolve quem é o dono do token.
func (e *Emissor) Conferir(token string) (Identidade, error) {
	partes := strings.Split(token, ".")
	if len(partes) != 3 || partes[0] != cabecalho {
		return Identidade{}, ErrTokenInvalido
	}
	esperada := e.assinar(partes[0] + "." + partes[1])
	if !hmac.Equal([]byte(esperada), []byte(partes[2])) {
		return Identidade{}, ErrTokenInvalido
	}
	corpo, err := b64url.DecodeString(partes[1])
	if err != nil {
		return Identidade{}, ErrTokenInvalido
	}
	var d dadosToken
	if err := json.Unmarshal(corpo, &d); err != nil {
		return Identidade{}, ErrTokenInvalido
	}
	if e.agora().Unix() >= d.Exp {
		return Identidade{}, ErrTokenVencido
	}
	id, err := strconv.ParseInt(d.Sub, 10, 64)
	if err != nil || id <= 0 {
		return Identidade{}, ErrTokenInvalido
	}
	return Identidade{UsuarioID: id, Nome: d.Nome}, nil
}

func (e *Emissor) assinar(texto string) string {
	mac := hmac.New(sha256.New, e.segredo)
	mac.Write([]byte(texto))
	return b64url.EncodeToString(mac.Sum(nil))
}
