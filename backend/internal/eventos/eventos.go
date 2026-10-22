// Package eventos leva o que acontece (mensagem nova, canal criado, cargo alterado...) até
// as conexões de tempo real.
//
// Os eventos são publicados em tópicos ("servidor:42", "usuario:7"). Cada instância do
// Prosa tem um Hub local com quem assinou cada tópico. Com uma instância só, publicar é
// entregar direto no Hub; com várias, a publicação passa pelo Redis (veja redis.go) e
// cada instância entrega no próprio Hub o que chegou.
package eventos

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
)

// Tipos de evento (o navegador recebe o mesmo nome).
const (
	Pronto            = "PRONTO"
	ServidorCriado    = "SERVIDOR_CRIADO"
	ServidorAlterado  = "SERVIDOR_ALTERADO"
	ServidorApagado   = "SERVIDOR_APAGADO"
	CanalCriado       = "CANAL_CRIADO"
	CanalAlterado     = "CANAL_ALTERADO"
	CanalApagado      = "CANAL_APAGADO"
	CategoriaCriada   = "CATEGORIA_CRIADA"
	CategoriaAlterada = "CATEGORIA_ALTERADA"
	CategoriaApagada  = "CATEGORIA_APAGADA"
	CargoCriado       = "CARGO_CRIADO"
	CargoAlterado     = "CARGO_ALTERADO"
	CargoApagado      = "CARGO_APAGADO"
	MembroEntrou      = "MEMBRO_ENTROU"
	MembroAlterado    = "MEMBRO_ALTERADO"
	MembroSaiu        = "MEMBRO_SAIU"
	MensagemCriada    = "MENSAGEM_CRIADA"
	MensagemEditada   = "MENSAGEM_EDITADA"
	MensagemApagada   = "MENSAGEM_APAGADA"
	ReacaoAdicionada  = "REACAO_ADICIONADA"
	ReacaoRemovida    = "REACAO_REMOVIDA"
	Digitando         = "DIGITANDO"
	Presenca          = "PRESENCA"
	VozEstado         = "VOZ_ESTADO"
	UsuarioAlterado   = "USUARIO_ALTERADO" // perfil ou status de alguém mudou
)

// MudaPermissoes diz se o evento pode mudar o que alguém vê no servidor
// (as conexões recalculam as permissões quando recebem um desses).
func MudaPermissoes(tipo string) bool {
	switch tipo {
	case ServidorAlterado, CanalCriado, CanalAlterado, CanalApagado, CargoCriado, CargoAlterado,
		CargoApagado, MembroAlterado, MembroSaiu:
		return true
	}
	return false
}

// Evento é o que circula entre as instâncias (formato compacto no Redis).
type Evento struct {
	Tipo       string          `json:"t"`
	ServidorID int64           `json:"s,omitempty"`
	CanalID    int64           `json:"c,omitempty"`
	UsuarioID  int64           `json:"u,omitempty"` // a quem o evento se refere (ex.: membro que saiu)
	Exige      uint64          `json:"p,omitempty"` // permissão necessária no canal para receber
	Dados      json.RawMessage `json:"d,omitempty"`

	// Pronto é o evento já em JSON no formato que vai para o navegador, calculado uma vez
	// só no Hub (e não uma vez por conexão).
	Pronto []byte `json:"-"`
}

// Codificar devolve o JSON que vai para o navegador (sem a permissão exigida).
func (e Evento) Codificar() []byte {
	e.Exige = 0
	b, _ := json.Marshal(e)
	return b
}

// Novo monta um evento já com os dados em JSON.
func Novo(tipo string, servidorID, canalID int64, dados any) Evento {
	bruto, _ := json.Marshal(dados)
	return Evento{Tipo: tipo, ServidorID: servidorID, CanalID: canalID, Dados: bruto}
}

func TopicoServidor(id int64) string { return "servidor:" + strconv.FormatInt(id, 10) }
func TopicoUsuario(id int64) string  { return "usuario:" + strconv.FormatInt(id, 10) }

// Publicador é quem os serviços usam para avisar que algo aconteceu.
type Publicador interface {
	Publicar(ctx context.Context, topico string, e Evento) error
}

// Assinante recebe os eventos de um tópico. Entregar não pode bloquear (cada conexão
// tem a própria fila).
type Assinante interface {
	Entregar(topico string, e Evento)
}

// Hub entrega os eventos às assinaturas desta instância.
type Hub struct {
	mu         sync.RWMutex
	assinantes map[string]map[Assinante]struct{}
	aoMudar    func(topico string, assinado bool) // o Redis usa para assinar só o necessário
	aguardar   func(ctx context.Context, topicos []string) error
}

// Aguardar só volta quando os tópicos já estão recebendo eventos de todas as instâncias
// (com o Redis, a assinatura é feita na hora em vez de na fila). Com uma instância só,
// volta direto.
func (h *Hub) Aguardar(ctx context.Context, topicos ...string) error {
	h.mu.RLock()
	f := h.aguardar
	h.mu.RUnlock()
	if f == nil || len(topicos) == 0 {
		return nil
	}
	return f(ctx, topicos)
}

func NovoHub() *Hub {
	return &Hub{assinantes: map[string]map[Assinante]struct{}{}}
}

// Assinar passa a entregar os eventos do tópico para a.
func (h *Hub) Assinar(topico string, a Assinante) {
	h.mu.Lock()
	lista := h.assinantes[topico]
	novo := lista == nil
	if novo {
		lista = map[Assinante]struct{}{}
		h.assinantes[topico] = lista
	}
	lista[a] = struct{}{}
	aviso := h.aoMudar
	h.mu.Unlock()
	if novo && aviso != nil {
		aviso(topico, true)
	}
}

// Cancelar para de entregar o tópico para a.
func (h *Hub) Cancelar(topico string, a Assinante) {
	h.mu.Lock()
	lista := h.assinantes[topico]
	vazio := false
	if lista != nil {
		delete(lista, a)
		if len(lista) == 0 {
			delete(h.assinantes, topico)
			vazio = true
		}
	}
	aviso := h.aoMudar
	h.mu.Unlock()
	if vazio && aviso != nil {
		aviso(topico, false)
	}
}

// Entregar passa o evento para todos que assinaram o tópico nesta instância.
func (h *Hub) Entregar(topico string, e Evento) {
	h.mu.RLock()
	lista := make([]Assinante, 0, len(h.assinantes[topico]))
	for a := range h.assinantes[topico] {
		lista = append(lista, a)
	}
	h.mu.RUnlock()
	if len(lista) == 0 {
		return
	}
	if e.Pronto == nil {
		e.Pronto = e.Codificar()
	}
	for _, a := range lista {
		a.Entregar(topico, e)
	}
}

// Topicos devolve os tópicos com alguém assinando (o Redis reassina ao reconectar).
func (h *Hub) Topicos() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	lista := make([]string, 0, len(h.assinantes))
	for t := range h.assinantes {
		lista = append(lista, t)
	}
	return lista
}

// Assinantes conta quantas assinaturas o tópico tem nesta instância.
func (h *Hub) Assinantes(topico string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.assinantes[topico])
}

// Local publica direto no Hub (uma instância só, sem Redis).
type Local struct{ Hub *Hub }

func (l Local) Publicar(_ context.Context, topico string, e Evento) error {
	l.Hub.Entregar(topico, e)
	return nil
}
