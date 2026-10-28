package usuarios

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cauan-Silva/prosa/internal/api"
)

// Visual é a personalização do perfil. Os nomes vêm de listas fixas (o desenho de cada
// moldura e efeito está nas telas), então o servidor só confere se o nome existe.
type Visual struct {
	Moldura string `json:"moldura,omitempty"` // enfeite em volta da foto
	Efeito  string `json:"efeito,omitempty"`  // animação por cima do cartão de perfil
	Tema    []int  `json:"tema,omitempty"`    // duas cores (0xRRGGBB) do fundo do cartão
	Nome    string `json:"nome,omitempty"`    // estilo do nome de exibição
}

var (
	Molduras    = conjunto("neon", "ouro", "azulejo", "arco-iris", "chamas", "folhas", "estrelas", "fones", "coroa", "pixel", "orbita", "gelo")
	Efeitos     = conjunto("confete", "neve", "estrelas", "bolhas", "petalas", "aurora", "fogos", "codigo", "coracoes", "vagalumes")
	EstilosNome = conjunto("gradiente", "neon", "arco-iris", "ouro", "glitch")
)

func conjunto(nomes ...string) map[string]bool {
	m := map[string]bool{"": true}
	for _, n := range nomes {
		m[n] = true
	}
	return m
}

// Validar confere cada campo; um nome que não existe é recusado (e não ignorado), para a
// tela mostrar o erro.
func (v Visual) Validar() error {
	if !Molduras[v.Moldura] {
		return api.NovoErro(http.StatusBadRequest, "Essa moldura não existe.")
	}
	if !Efeitos[v.Efeito] {
		return api.NovoErro(http.StatusBadRequest, "Esse efeito não existe.")
	}
	if !EstilosNome[v.Nome] {
		return api.NovoErro(http.StatusBadRequest, "Esse estilo de nome não existe.")
	}
	if len(v.Tema) != 0 && len(v.Tema) != 2 {
		return api.NovoErro(http.StatusBadRequest, "O tema do perfil tem duas cores.")
	}
	for _, c := range v.Tema {
		if c < 0 || c > 0xffffff {
			return api.NovoErro(http.StatusBadRequest, "Cor inválida no tema do perfil.")
		}
	}
	return nil
}

// Value e Scan guardam e leem o Visual como JSONB.
func (v Visual) Value() (driver.Value, error) { return json.Marshal(v) }

func (v *Visual) Scan(origem any) error {
	*v = Visual{}
	switch d := origem.(type) {
	case nil:
		return nil
	case []byte:
		return json.Unmarshal(d, v)
	case string:
		return json.Unmarshal([]byte(d), v)
	}
	return errors.New("visual: tipo inesperado")
}
