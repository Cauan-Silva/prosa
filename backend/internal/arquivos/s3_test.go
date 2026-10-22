package arquivos

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Exemplo oficial da documentação da AWS ("GET Object" com URL pré-assinada):
// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-query-string-auth.html
func TestAssinaturaBateComOExemploDaAWS(t *testing.T) {
	s := NovoS3("https://examplebucket.s3.amazonaws.com", "us-east-1", "", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	s.agora = func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) }
	canonico := strings.Join([]string{
		"GET",
		"/test.txt",
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host",
		"host:examplebucket.s3.amazonaws.com\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	got := s.assinar("20130524", "20130524T000000Z", "20130524/us-east-1/s3/aws4_request", canonico)
	want := "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Errorf("assinatura errada:\n got %s\nwant %s", got, want)
	}
}

func TestEnderecoAssinadoTemOsCampos(t *testing.T) {
	s := NovoS3("https://s3.us-east-005.backblazeb2.com", "us-east-005", "meu-bucket", "chave", "segredo")
	end, ok := s.Endereco("anexos/1/abc.png", "foto de férias.png", 10*time.Minute)
	if !ok {
		t.Fatal("S3 sempre gera endereço")
	}
	for _, parte := range []string{
		"https://s3.us-east-005.backblazeb2.com/meu-bucket/anexos/1/abc.png?",
		"X-Amz-Expires=600", "X-Amz-SignedHeaders=host", "X-Amz-Signature=",
		"response-content-disposition=inline%3B%20filename%2A%3DUTF-8%27%27foto%2520de%2520f%25C3%25A9rias.png",
	} {
		if !strings.Contains(end, parte) {
			t.Errorf("faltou %q em %s", parte, end)
		}
	}
}

func TestGravarMandaPUTAssinado(t *testing.T) {
	var recebido struct {
		metodo, caminho, auth, tipo, corpo string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		recebido.metodo, recebido.caminho, recebido.auth, recebido.tipo, recebido.corpo =
			r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)
	}))
	defer srv.Close()
	s := NovoS3(srv.URL, "auto", "b", "AKID", "segredo")
	if err := s.Gravar(context.Background(), "anexos/1/x y.txt", strings.NewReader("olá"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if recebido.metodo != "PUT" || recebido.caminho != "/b/anexos/1/x%20y.txt" || recebido.corpo != "olá" || recebido.tipo != "text/plain" {
		t.Errorf("pedido errado: %+v", recebido)
	}
	if !strings.HasPrefix(recebido.auth, "AWS4-HMAC-SHA256 Credential=AKID/") ||
		!strings.Contains(recebido.auth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("assinatura errada: %s", recebido.auth)
	}
}

func TestPastaLocal(t *testing.T) {
	p, err := NovaPasta(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := p.Gravar(ctx, "anexos/1/a.txt", strings.NewReader("conteúdo"), 0, "text/plain"); err != nil {
		t.Fatal(err)
	}
	f, err := p.Abrir(ctx, "anexos/1/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "conteúdo" {
		t.Errorf("leu %q", b)
	}
	if _, ok := p.Endereco("x", "", time.Minute); ok {
		t.Error("pasta local não tem endereço direto")
	}
	if err := p.Gravar(ctx, "../fora.txt", strings.NewReader("x"), 0, ""); err == nil {
		t.Error("não pode sair da pasta")
	}
	if err := p.Apagar(ctx, "anexos/1/a.txt"); err != nil {
		t.Error(err)
	}
}
