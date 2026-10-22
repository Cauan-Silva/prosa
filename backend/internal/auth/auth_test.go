package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSenhaConfereSoComASenhaCerta(t *testing.T) {
	hash, err := hashCom("minha senha", 1000) // poucas iterações para o teste ser rápido
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$1000$") {
		t.Fatalf("formato inesperado: %s", hash)
	}
	if ok, _ := ConferirSenha("minha senha", hash); !ok {
		t.Error("a senha certa deveria conferir")
	}
	if ok, _ := ConferirSenha("outra senha", hash); ok {
		t.Error("uma senha errada não pode conferir")
	}
}

func TestMesmaSenhaGeraHashesDiferentes(t *testing.T) {
	a, _ := hashCom("igual", 1000)
	b, _ := hashCom("igual", 1000)
	if a == b {
		t.Error("o sal aleatório deveria deixar os hashes diferentes")
	}
}

func TestSenhaComFormatoDesconhecido(t *testing.T) {
	if _, err := ConferirSenha("x", "bcrypt$abc"); err == nil {
		t.Error("deveria recusar formato desconhecido")
	}
}

func TestTokenIdaEVolta(t *testing.T) {
	e := NovoEmissor([]byte("um-segredo-de-teste-com-32-caracteres!"))
	token, err := e.Emitir(Identidade{UsuarioID: 42, Nome: "Cauan"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := e.Conferir(token)
	if err != nil {
		t.Fatal(err)
	}
	if id.UsuarioID != 42 || id.Nome != "Cauan" {
		t.Errorf("identidade errada: %+v", id)
	}
}

func TestTokenAdulteradoOuDeOutroSegredo(t *testing.T) {
	e := NovoEmissor([]byte("um-segredo-de-teste-com-32-caracteres!"))
	token, _ := e.Emitir(Identidade{UsuarioID: 1, Nome: "A"})

	outro := NovoEmissor([]byte("outro-segredo-de-teste-com-32-caract!"))
	if _, err := outro.Conferir(token); err != ErrTokenInvalido {
		t.Errorf("outro segredo: esperava ErrTokenInvalido, veio %v", err)
	}

	partes := strings.Split(token, ".")
	falso := b64url.EncodeToString([]byte(`{"sub":"999","nome":"X","iat":1,"exp":99999999999}`))
	if _, err := e.Conferir(partes[0] + "." + falso + "." + partes[2]); err != ErrTokenInvalido {
		t.Errorf("corpo trocado: esperava ErrTokenInvalido, veio %v", err)
	}

	semAlg := b64url.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	if _, err := e.Conferir(semAlg + "." + partes[1] + "."); err != ErrTokenInvalido {
		t.Errorf("alg none: esperava ErrTokenInvalido, veio %v", err)
	}
}

func TestTokenVencido(t *testing.T) {
	e := NovoEmissor([]byte("um-segredo-de-teste-com-32-caracteres!"))
	inicio := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e.agora = func() time.Time { return inicio }
	token, _ := e.Emitir(Identidade{UsuarioID: 1, Nome: "A"})

	e.agora = func() time.Time { return inicio.Add(DuracaoToken + time.Second) }
	if _, err := e.Conferir(token); err != ErrTokenVencido {
		t.Errorf("esperava ErrTokenVencido, veio %v", err)
	}
}
