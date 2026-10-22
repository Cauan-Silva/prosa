// Package mensagens cuida das mensagens dos canais de texto: enviar, editar, apagar,
// responder, mencionar, reagir, buscar, anexar arquivos e marcar como lido.
package mensagens

import (
	"regexp"
	"strconv"
)

// Formato das menções no texto (a tela mostra o nome no lugar):
//
//	<@123>   uma pessoa
//	<@&45>   um cargo
//	@todos   todo mundo que vê o canal (precisa da permissão "Mencionar @todos")
var (
	padraoUsuario = regexp.MustCompile(`<@(\d{1,19})>`)
	padraoCargo   = regexp.MustCompile(`<@&(\d{1,19})>`)
	padraoTodos   = regexp.MustCompile(`(^|[^\w@])@todos\b`)
)

// Mencoes é o que foi encontrado no texto.
type Mencoes struct {
	Usuarios []int64
	Cargos   []int64
	Todos    bool
}

const maxMencoes = 50

// LerMencoes encontra as menções (sem repetir, no máximo 50 de cada tipo).
func LerMencoes(texto string) Mencoes {
	return Mencoes{
		Usuarios: ids(padraoUsuario, texto),
		Cargos:   ids(padraoCargo, texto),
		Todos:    padraoTodos.MatchString(texto),
	}
}

func ids(padrao *regexp.Regexp, texto string) []int64 {
	vistos := map[int64]bool{}
	var lista []int64
	for _, m := range padrao.FindAllStringSubmatch(texto, -1) {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || id <= 0 || vistos[id] {
			continue
		}
		vistos[id] = true
		lista = append(lista, id)
		if len(lista) == maxMencoes {
			break
		}
	}
	return lista
}
