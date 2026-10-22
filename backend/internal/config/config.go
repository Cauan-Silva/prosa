// Package config lê a configuração das variáveis de ambiente.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Porta           string
	BancoURL        string   // DATABASE_URL
	RedisURL        string   // REDIS_URL (vazio = uma instância só, eventos em memória)
	SegredoJWT      []byte   // JWT_SEGREDO
	OrigensWS       []string // ORIGENS_WS
	Desenvolvimento bool     // AMBIENTE=desenvolvimento
	Instancia       string   // INSTANCIA: nome desta cópia do servidor (gerado se vazio)
	LoginPorMinuto  int      // LOGIN_POR_MINUTO: tentativas de login/cadastro por IP (padrão 10)
	EnderecoPublico string   // ENDERECO_PUBLICO: URL desta instância (usado pela voz em várias instâncias)

	// Arquivos: S3 compatível (Backblaze B2, Cloudflare R2...) ou pasta local.
	S3Endpoint string // S3_ENDPOINT
	S3Regiao   string // S3_REGIAO
	S3Bucket   string // S3_BUCKET
	S3Chave    string // S3_CHAVE
	S3Segredo  string // S3_SEGREDO
	PastaLocal string // PASTA_ARQUIVOS (sem S3)
	MaxAnexoMB int    // MAX_ANEXO_MB

	// Voz
	TurnURLs    []string // TURN_URLS
	TurnUsuario string   // TURN_USUARIO
	TurnSenha   string   // TURN_SENHA
	PortaUDP    int      // VOZ_PORTA_UDP: porta única para a mídia (0 = portas aleatórias)
	IPPublico   string   // VOZ_IP_PUBLICO: IP anunciado para a mídia (atrás de NAT/Docker)
}

const segredoDev = "segredo-de-desenvolvimento-do-prosa-troque-em-producao"

func Ler() (Config, error) {
	c := Config{
		Porta:           valor("PORT", "8080"),
		BancoURL:        valor("DATABASE_URL", "postgres://prosa:prosa@localhost:5432/prosa?sslmode=disable"),
		RedisURL:        valor("REDIS_URL", ""),
		Desenvolvimento: valor("AMBIENTE", "producao") == "desenvolvimento",
		Instancia:       valor("INSTANCIA", ""),
		EnderecoPublico: strings.TrimRight(valor("ENDERECO_PUBLICO", ""), "/"),
		S3Endpoint:      valor("S3_ENDPOINT", ""),
		S3Regiao:        valor("S3_REGIAO", "auto"),
		S3Bucket:        valor("S3_BUCKET", ""),
		S3Chave:         valor("S3_CHAVE", ""),
		S3Segredo:       valor("S3_SEGREDO", ""),
		PastaLocal:      valor("PASTA_ARQUIVOS", "arquivos"),
		TurnUsuario:     valor("TURN_USUARIO", ""),
		TurnSenha:       valor("TURN_SENHA", ""),
		IPPublico:       valor("VOZ_IP_PUBLICO", ""),
	}
	var err error
	if c.MaxAnexoMB, err = strconv.Atoi(valor("MAX_ANEXO_MB", "8")); err != nil || c.MaxAnexoMB < 1 {
		return c, errors.New("MAX_ANEXO_MB precisa ser um número positivo")
	}
	if c.LoginPorMinuto, err = strconv.Atoi(valor("LOGIN_POR_MINUTO", "10")); err != nil || c.LoginPorMinuto < 1 {
		return c, errors.New("LOGIN_POR_MINUTO precisa ser um número positivo")
	}
	if c.PortaUDP, err = strconv.Atoi(valor("VOZ_PORTA_UDP", "0")); err != nil {
		return c, errors.New("VOZ_PORTA_UDP precisa ser um número")
	}

	segredo := os.Getenv("JWT_SEGREDO")
	if segredo == "" && c.Desenvolvimento {
		segredo = segredoDev
	}
	if len(segredo) < 32 {
		return c, errors.New("JWT_SEGREDO precisa ter pelo menos 32 caracteres (em desenvolvimento, use AMBIENTE=desenvolvimento)")
	}
	c.SegredoJWT = []byte(segredo)

	c.OrigensWS = lista(valor("ORIGENS_WS", ""))
	if c.Desenvolvimento && len(c.OrigensWS) == 0 {
		c.OrigensWS = []string{"localhost:5173", "127.0.0.1:5173"}
	}
	// Aceita o endpoint do S3 sem "https://" (é assim que o Backblaze mostra na tela).
	if c.S3Endpoint != "" && !strings.Contains(c.S3Endpoint, "://") {
		c.S3Endpoint = "https://" + c.S3Endpoint
	}
	c.TurnURLs = lista(valor("TURN_URLS", ""))
	if c.Instancia == "" {
		b := make([]byte, 3)
		_, _ = rand.Read(b)
		c.Instancia = "prosa-" + hex.EncodeToString(b)
	}
	return c, nil
}

// TemS3 diz se os arquivos vão para um armazenamento S3.
func (c Config) TemS3() bool {
	return c.S3Endpoint != "" && c.S3Bucket != "" && c.S3Chave != "" && c.S3Segredo != ""
}

func lista(texto string) []string {
	var saida []string
	for _, p := range strings.Split(texto, ",") {
		if p = strings.TrimSpace(p); p != "" {
			saida = append(saida, p)
		}
	}
	return saida
}

func valor(nome, padrao string) string {
	if v := strings.TrimSpace(os.Getenv(nome)); v != "" {
		return v
	}
	return padrao
}
