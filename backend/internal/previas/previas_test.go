package previas

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestLerPagina(t *testing.T) {
	base, _ := url.Parse("https://exemplo.com.br/artigo/1")
	p := lerPagina(strings.NewReader(`<!doctype html><html><head>
		<title> Título da aba </title>
		<meta property="og:title" content="Título do cartão">
		<meta name="description" content="descrição comum">
		<meta property="og:image" content="/capa.png">
		<meta property="og:site_name" content="Exemplo">
		<meta name="theme-color" content="#123456">
		</head><body><meta property="og:title" content="ignorado"></body></html>`), base)
	if p.Titulo != "Título do cartão" || p.Descricao != "descrição comum" || p.Site != "Exemplo" || p.Cor != "#123456" {
		t.Fatalf("prévia errada: %+v", p)
	}
	if p.Imagem != "https://exemplo.com.br/capa.png" {
		t.Fatalf("imagem deveria virar endereço completo: %q", p.Imagem)
	}
	semOG := lerPagina(strings.NewReader(`<html><head><title>Só o título</title></head></html>`), base)
	if semOG.Titulo != "Só o título" {
		t.Fatalf("sem og:title deveria usar o <title>: %+v", semOG)
	}
	js := lerPagina(strings.NewReader(`<head><meta property="og:image" content="javascript:alert(1)"></head>`), base)
	if js.Imagem != "" {
		t.Fatalf("imagem javascript: deveria ser descartada: %q", js.Imagem)
	}
}

func TestIPPublico(t *testing.T) {
	internos := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.10", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fd00::1", "fe80::1"}
	for _, ip := range internos {
		if ipPublico(net.ParseIP(ip)) {
			t.Errorf("%s deveria ser recusado", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "142.250.79.46", "2606:4700::1111"} {
		if !ipPublico(net.ParseIP(ip)) {
			t.Errorf("%s deveria ser aceito", ip)
		}
	}
	if err := controle(false)("tcp", "127.0.0.1:443", nil); err == nil {
		t.Fatal("conexão para 127.0.0.1 deveria ser recusada")
	}
	if err := controle(false)("tcp", "8.8.8.8:22", nil); err == nil {
		t.Fatal("porta 22 deveria ser recusada")
	}
}
