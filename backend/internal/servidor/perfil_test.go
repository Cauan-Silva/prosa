package servidor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func pngDeTeste() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{30, 60, 200, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func enviarImagem(t *testing.T, c *cliente, caminho string, dados []byte, nome string) (int, map[string]any) {
	t.Helper()
	corpo := &bytes.Buffer{}
	mp := multipart.NewWriter(corpo)
	parte, _ := mp.CreateFormFile("arquivo", nome)
	parte.Write(dados)
	mp.Close()
	req, _ := http.NewRequest("POST", c.base+caminho, corpo)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var saida map[string]any
	json.NewDecoder(resp.Body).Decode(&saida)
	return resp.StatusCode, saida
}

func TestPerfil(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	estranho := cadastrar(t, srv.URL, "carlos")

	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Perfis"}, &v)
	sid := v.Servidor.ID
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}
	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("/api/servidores/%d/canais/%d/convites", sid, geral), map[string]int{}, &conv)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)

	wa := conectar(t, srv.URL, ana.token)
	wa.esperar("PRONTO", nil)

	// Bia muda o perfil: a Ana fica sabendo pelo tempo real
	var eu map[string]any
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{
		"bio": "Gosto de café e de Go.", "pronomes": "ela/dela", "corPerfil": 0x8b5cf6,
		"status": "ocupado", "statusTexto": "estudando", "statusEmoji": "📚",
	}, &eu)
	if eu["bio"] != "Gosto de café e de Go." || eu["status"] != "ocupado" {
		t.Fatalf("perfil não salvo: %v", eu)
	}
	alt := wa.esperar("USUARIO_ALTERADO", nil)
	if !strings.Contains(string(alt.Dados), `"statusTexto":"estudando"`) || strings.Contains(string(alt.Dados), "email") {
		t.Fatalf("evento de perfil: %s", alt.Dados)
	}
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"status": "dormindo"}, nil)
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"bio": strings.Repeat("a", 401)}, nil)

	// avatar: imagem de verdade passa, texto disfarçado não
	status, conta := enviarImagem(t, bia, "/api/eu/avatar", pngDeTeste(), "eu.png")
	avatar, _ := conta["avatar"].(string)
	if status != 200 || !strings.HasPrefix(avatar, "/api/perfis/") {
		t.Fatalf("avatar: %d %v", status, conta)
	}
	if s, _ := enviarImagem(t, bia, "/api/eu/banner", []byte("<html>oi</html>"), "x.png"); s != 400 {
		t.Errorf("HTML disfarçado de imagem foi aceito: %d", s)
	}
	resp, err := http.Get(srv.URL + avatar) // pública, sem login
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(b, pngDeTeste()) {
		t.Errorf("imagem do perfil: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if r, _ := http.Get(srv.URL + "/api/perfis/1/../../anexos"); r.StatusCode != 404 {
		t.Errorf("caminho estranho deveria dar 404: %d", r.StatusCode)
	}

	// lista de membros traz avatar e status
	var membros []map[string]any
	ana.esperar(200, "GET", fmt.Sprintf("/api/servidores/%d/membros", sid), nil, &membros)
	achou := false
	for _, m := range membros {
		if m["nomeUsuario"] == "bia" {
			achou = m["avatar"] == avatar && m["status"] == "ocupado" && m["statusEmoji"] == "📚"
		}
	}
	if !achou {
		t.Errorf("membros sem o perfil da Bia: %v", membros)
	}

	// perfil completo: quem divide servidor vê, estranho não
	var perfil map[string]any
	ana.esperar(200, "GET", fmt.Sprintf("/api/usuarios/%d", bia.id), nil, &perfil)
	if perfil["pronomes"] != "ela/dela" || perfil["email"] != nil {
		t.Errorf("perfil público: %v", perfil)
	}
	estranho.esperar(404, "GET", fmt.Sprintf("/api/usuarios/%d", bia.id), nil, nil)

	// preferências ficam na conta
	bia.esperar(204, "PUT", "/api/eu/preferencias", map[string]any{"tema": "meia-noite", "destaque": "#8b5cf6"}, nil)
	bia.esperar(200, "GET", "/api/eu", nil, &eu)
	if p, _ := eu["preferencias"].(map[string]any); p["tema"] != "meia-noite" {
		t.Errorf("preferências: %v", eu["preferencias"])
	}

	// invisível: a Bia conectada some para a Ana, e reaparece ao voltar
	wb := conectar(t, srv.URL, bia.token)
	wb.esperar("PRONTO", nil)
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":true`) })
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{"status": "invisivel"}, nil)
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":false`) })
	wa2 := conectar(t, srv.URL, ana.token)
	p := wa2.esperar("PRONTO", nil)
	if strings.Contains(string(p.Dados), fmt.Sprintf(`[%d,%d]`, ana.id, bia.id)) || strings.Contains(string(p.Dados), fmt.Sprintf(`,%d]`, bia.id)) {
		t.Errorf("invisível apareceu online no PRONTO: %s", p.Dados)
	}
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{"status": "online"}, nil)
	wa.esperar("PRESENCA", func(e evento) bool { return strings.Contains(string(e.Dados), `"online":true`) })

	// tirar o avatar
	bia.esperar(200, "DELETE", "/api/eu/avatar", nil, &eu)
	if eu["avatar"] != "" {
		t.Errorf("avatar não saiu: %v", eu["avatar"])
	}
}

func TestVisualDoPerfil(t *testing.T) {
	srv, _ := ambiente(t)
	ana := cadastrar(t, srv.URL, "ana")
	bia := cadastrar(t, srv.URL, "bia")
	var v visao
	ana.esperar(201, "POST", "/api/servidores", map[string]string{"nome": "Visual"}, &v)
	var geral int64
	for _, c := range v.Canais {
		if c.Tipo == "texto" {
			geral = c.ID
		}
	}
	var conv struct {
		Codigo string `json:"codigo"`
	}
	ana.esperar(201, "POST", fmt.Sprintf("/api/servidores/%d/canais/%d/convites", v.Servidor.ID, geral), map[string]int{}, &conv)
	bia.esperar(200, "POST", "/api/convites/"+conv.Codigo, nil, nil)
	wa := conectar(t, srv.URL, ana.token)
	wa.esperar("PRONTO", nil)

	visual := map[string]any{"moldura": "neon", "efeito": "neve", "tema": []int{0x1f3fa6, 0xd63a7a}, "nome": "gradiente"}
	var eu map[string]any
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{"visual": visual}, &eu)
	vis, _ := eu["visual"].(map[string]any)
	if vis["moldura"] != "neon" || vis["efeito"] != "neve" || vis["nome"] != "gradiente" || len(vis["tema"].([]any)) != 2 {
		t.Fatalf("visual não salvo: %v", eu["visual"])
	}
	// mudar outro campo não apaga o visual
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{"bio": "oi"}, &eu)
	if eu["visual"].(map[string]any)["moldura"] != "neon" {
		t.Fatalf("visual sumiu ao salvar a bio: %v", eu["visual"])
	}
	alt := wa.esperar("USUARIO_ALTERADO", nil)
	if !strings.Contains(string(alt.Dados), `"moldura":"neon"`) {
		t.Fatalf("evento sem o visual: %s", alt.Dados)
	}
	// aparece para os outros: lista de membros e perfil público
	var membros []map[string]any
	ana.esperar(200, "GET", fmt.Sprintf("/api/servidores/%d/membros", v.Servidor.ID), nil, &membros)
	achou := false
	for _, m := range membros {
		if m["nomeUsuario"] == "bia" {
			achou = m["visual"].(map[string]any)["moldura"] == "neon"
		}
	}
	if !achou {
		t.Fatal("lista de membros sem a moldura")
	}
	var perfil map[string]any
	ana.esperar(200, "GET", fmt.Sprintf("/api/usuarios/%v", eu["id"]), nil, &perfil)
	if perfil["visual"].(map[string]any)["efeito"] != "neve" {
		t.Fatalf("perfil público sem o efeito: %v", perfil["visual"])
	}
	// nomes que não existem e cores fora da faixa são recusados
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"visual": map[string]any{"moldura": "<script>"}}, nil)
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"visual": map[string]any{"efeito": "tremor"}}, nil)
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"visual": map[string]any{"tema": []int{1, 2, 3}}}, nil)
	bia.esperar(400, "PATCH", "/api/eu", map[string]any{"visual": map[string]any{"tema": []int{0x1000000, 0}}}, nil)
	// tirar tudo volta ao padrão
	bia.esperar(200, "PATCH", "/api/eu", map[string]any{"visual": map[string]any{}}, &eu)
	if len(eu["visual"].(map[string]any)) != 0 {
		t.Fatalf("visual não foi limpo: %v", eu["visual"])
	}
}
