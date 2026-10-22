// Package arquivos guarda os anexos: num armazenamento compatível com S3 (Backblaze B2,
// Cloudflare R2, MinIO...) ou numa pasta local, quando não há S3 configurado.
package arquivos

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Armazem é onde os bytes ficam.
type Armazem interface {
	Gravar(ctx context.Context, chave string, conteudo io.Reader, tamanho int64, tipo string) error
	// Endereco devolve um link temporário para baixar direto do armazenamento (S3).
	// ok = false quando o próprio servidor precisa entregar o arquivo (pasta local).
	Endereco(chave, nome string, validade time.Duration) (endereco string, ok bool)
	Abrir(ctx context.Context, chave string) (io.ReadCloser, error)
	Apagar(ctx context.Context, chave string) error
}

// ---- pasta local ----

type Pasta struct{ Raiz string }

func NovaPasta(raiz string) (*Pasta, error) {
	if err := os.MkdirAll(raiz, 0o755); err != nil {
		return nil, err
	}
	return &Pasta{Raiz: raiz}, nil
}

var errChave = errors.New("chave de arquivo inválida")

func (p *Pasta) caminho(chave string) (string, error) {
	if chave == "" || strings.Contains(chave, "..") || strings.HasPrefix(chave, "/") {
		return "", errChave
	}
	return filepath.Join(p.Raiz, filepath.FromSlash(chave)), nil
}

func (p *Pasta) Gravar(_ context.Context, chave string, conteudo io.Reader, _ int64, _ string) error {
	destino, err := p.caminho(chave)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return err
	}
	tmp := destino + ".parcial"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, conteudo); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destino)
}

func (p *Pasta) Endereco(string, string, time.Duration) (string, bool) { return "", false }

func (p *Pasta) Abrir(_ context.Context, chave string) (io.ReadCloser, error) {
	caminho, err := p.caminho(chave)
	if err != nil {
		return nil, err
	}
	return os.Open(caminho)
}

func (p *Pasta) Apagar(_ context.Context, chave string) error {
	caminho, err := p.caminho(chave)
	if err != nil {
		return err
	}
	if err := os.Remove(caminho); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// escapar codifica cada parte do caminho como o S3 espera (mantém as barras).
func escapar(caminho string) string {
	partes := strings.Split(caminho, "/")
	for i, p := range partes {
		partes[i] = strings.ReplaceAll(url.PathEscape(p), "+", "%2B")
	}
	return strings.Join(partes, "/")
}
