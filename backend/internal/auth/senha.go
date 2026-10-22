// Package auth cuida das senhas (PBKDF2) e dos tokens de login (JWT HS256),
// só com a biblioteca padrão do Go.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Iterações do PBKDF2-SHA256 (recomendação da OWASP para 2023 em diante).
const iteracoesPadrao = 600_000

var b64 = base64.RawStdEncoding

// HashSenha devolve "pbkdf2-sha256$iterações$sal$hash", pronto para guardar no banco.
func HashSenha(senha string) (string, error) {
	return hashCom(senha, iteracoesPadrao)
}

func hashCom(senha string, iteracoes int) (string, error) {
	sal := make([]byte, 16)
	if _, err := rand.Read(sal); err != nil {
		return "", err
	}
	chave, err := pbkdf2.Key(sha256.New, senha, sal, iteracoes, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iteracoes, b64.EncodeToString(sal), b64.EncodeToString(chave)), nil
}

// ConferirSenha diz se a senha bate com o hash guardado (comparação em tempo constante).
func ConferirSenha(senha, guardado string) (bool, error) {
	partes := strings.Split(guardado, "$")
	if len(partes) != 4 || partes[0] != "pbkdf2-sha256" {
		return false, errors.New("formato de senha desconhecido")
	}
	iteracoes, err := strconv.Atoi(partes[1])
	if err != nil || iteracoes < 1 {
		return false, errors.New("número de iterações inválido")
	}
	sal, err := b64.DecodeString(partes[2])
	if err != nil {
		return false, err
	}
	esperado, err := b64.DecodeString(partes[3])
	if err != nil {
		return false, err
	}
	chave, err := pbkdf2.Key(sha256.New, senha, sal, iteracoes, len(esperado))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(chave, esperado) == 1, nil
}
