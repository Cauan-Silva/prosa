package arquivos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// S3 fala com qualquer armazenamento compatível com S3 assinando os pedidos com AWS
// Signature V4 (escrito com a biblioteca padrão, sem o SDK da AWS).
type S3 struct {
	Endpoint string // ex.: https://s3.us-east-005.backblazeb2.com
	Regiao   string // ex.: us-east-005 (R2: auto)
	Bucket   string
	Chave    string
	Segredo  string
	Cliente  *http.Client
	agora    func() time.Time
}

func NovoS3(endpoint, regiao, bucket, chave, segredo string) *S3 {
	return &S3{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Regiao:   regiao,
		Bucket:   bucket,
		Chave:    chave,
		Segredo:  segredo,
		Cliente:  &http.Client{Timeout: 2 * time.Minute},
		agora:    time.Now,
	}
}

// caminhoObjeto usa o formato endpoint/bucket/chave (path-style), aceito por B2, R2 e MinIO.
func (s *S3) caminhoObjeto(chave string) string {
	return "/" + escapar(s.Bucket) + "/" + escapar(chave)
}

const semCorpo = "UNSIGNED-PAYLOAD"

func (s *S3) Gravar(ctx context.Context, chave string, conteudo io.Reader, tamanho int64, tipo string) error {
	u, err := url.Parse(s.Endpoint + s.caminhoObjeto(chave))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), conteudo)
	if err != nil {
		return err
	}
	req.ContentLength = tamanho
	req.Header.Set("Content-Type", tipo)
	req.Header.Set("Cache-Control", "private, max-age=31536000, immutable")
	s.assinarCabecalho(req, semCorpo)
	resp, err := s.Cliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("S3 respondeu %d: %s", resp.StatusCode, strings.TrimSpace(string(corpo)))
	}
	return nil
}

func (s *S3) Abrir(ctx context.Context, chave string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Endpoint+s.caminhoObjeto(chave), nil)
	if err != nil {
		return nil, err
	}
	s.assinarCabecalho(req, hex.EncodeToString(sha256Vazio))
	resp, err := s.Cliente.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("S3 respondeu %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *S3) Apagar(ctx context.Context, chave string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.Endpoint+s.caminhoObjeto(chave), nil)
	if err != nil {
		return err
	}
	s.assinarCabecalho(req, hex.EncodeToString(sha256Vazio))
	resp, err := s.Cliente.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("S3 respondeu %d", resp.StatusCode)
	}
	return nil
}

// Endereco gera um link assinado (válido pela validade pedida) que o navegador baixa direto
// do armazenamento, sem passar pelo servidor.
func (s *S3) Endereco(chave, nome string, validade time.Duration) (string, bool) {
	agora := s.agora().UTC()
	data := agora.Format("20060102")
	quando := agora.Format("20060102T150405Z")
	escopo := data + "/" + s.Regiao + "/s3/aws4_request"
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return "", false
	}
	consulta := url.Values{}
	consulta.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	consulta.Set("X-Amz-Credential", s.Chave+"/"+escopo)
	consulta.Set("X-Amz-Date", quando)
	consulta.Set("X-Amz-Expires", strconv.Itoa(int(validade.Seconds())))
	consulta.Set("X-Amz-SignedHeaders", "host")
	if nome != "" {
		consulta.Set("response-content-disposition", `inline; filename*=UTF-8''`+url.PathEscape(nome))
	}
	caminho := s.caminhoObjeto(chave)
	canonico := strings.Join([]string{
		http.MethodGet,
		caminho,
		consultaCanonica(consulta),
		"host:" + u.Host + "\n",
		"host",
		semCorpo,
	}, "\n")
	assinatura := s.assinar(data, quando, escopo, canonico)
	return s.Endpoint + caminho + "?" + consultaCanonica(consulta) + "&X-Amz-Signature=" + assinatura, true
}

var sha256Vazio = func() []byte { h := sha256.Sum256(nil); return h[:] }()

// assinarCabecalho põe a assinatura V4 no cabeçalho Authorization.
func (s *S3) assinarCabecalho(req *http.Request, hashCorpo string) {
	agora := s.agora().UTC()
	data := agora.Format("20060102")
	quando := agora.Format("20060102T150405Z")
	escopo := data + "/" + s.Regiao + "/s3/aws4_request"

	req.Header.Set("X-Amz-Date", quando)
	req.Header.Set("X-Amz-Content-Sha256", hashCorpo)
	req.Header.Set("Host", req.URL.Host)

	nomes := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("Content-Type") != "" {
		nomes = append(nomes, "content-type")
	}
	sort.Strings(nomes)
	var cab strings.Builder
	for _, n := range nomes {
		valor := req.Header.Get(n)
		if n == "host" {
			valor = req.URL.Host
		}
		cab.WriteString(n + ":" + strings.TrimSpace(valor) + "\n")
	}
	assinados := strings.Join(nomes, ";")
	canonico := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		consultaCanonica(req.URL.Query()),
		cab.String(),
		assinados,
		hashCorpo,
	}, "\n")
	assinatura := s.assinar(data, quando, escopo, canonico)
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.Chave, escopo, assinados, assinatura))
}

func (s *S3) assinar(data, quando, escopo, canonico string) string {
	h := sha256.Sum256([]byte(canonico))
	texto := "AWS4-HMAC-SHA256\n" + quando + "\n" + escopo + "\n" + hex.EncodeToString(h[:])
	chave := hmacSHA256([]byte("AWS4"+s.Segredo), data)
	chave = hmacSHA256(chave, s.Regiao)
	chave = hmacSHA256(chave, "s3")
	chave = hmacSHA256(chave, "aws4_request")
	return hex.EncodeToString(hmacSHA256(chave, texto))
}

func hmacSHA256(chave []byte, texto string) []byte {
	m := hmac.New(sha256.New, chave)
	m.Write([]byte(texto))
	return m.Sum(nil)
}

// consultaCanonica ordena os parâmetros e codifica como a especificação da AWS pede.
func consultaCanonica(v url.Values) string {
	chaves := make([]string, 0, len(v))
	for k := range v {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	partes := make([]string, 0, len(chaves))
	for _, k := range chaves {
		for _, valor := range v[k] {
			partes = append(partes, codificarAWS(k)+"="+codificarAWS(valor))
		}
	}
	return strings.Join(partes, "&")
}

// codificarAWS escapa tudo menos letras, números e -_.~ (espaço vira %20, não +).
func codificarAWS(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
