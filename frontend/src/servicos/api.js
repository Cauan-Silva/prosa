// Chamadas à API. O front-end e a API ficam no mesmo endereço (em desenvolvimento, o Vite
// repassa /api para o Go), então os caminhos são relativos.

const CHAVE_TOKEN = 'prosa.token'

export function lerToken() {
  try {
    return localStorage.getItem(CHAVE_TOKEN)
  } catch {
    return null
  }
}

export function guardarToken(token) {
  try {
    if (token) localStorage.setItem(CHAVE_TOKEN, token)
    else localStorage.removeItem(CHAVE_TOKEN)
  } catch {
    // navegação anônima sem armazenamento: o login vale só nesta aba
  }
}

export class ErroApi extends Error {
  constructor(mensagem, status) {
    super(mensagem)
    this.status = status
  }
}

async function pedir(metodo, caminho, corpo) {
  const token = lerToken()
  let resposta
  try {
    resposta = await fetch(caminho, {
      method: metodo,
      headers: {
        ...(corpo !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: corpo !== undefined ? JSON.stringify(corpo) : undefined,
    })
  } catch {
    throw new ErroApi('Sem conexão com o servidor. Confira sua internet.', 0)
  }
  if (resposta.status === 204) return null
  const dados = await resposta.json().catch(() => ({}))
  if (!resposta.ok) {
    if (resposta.status === 401 && token) window.dispatchEvent(new Event('sessao-expirada'))
    throw new ErroApi(dados.erro || 'Algo deu errado. Tente de novo.', resposta.status)
  }
  return dados
}

/** Envia um arquivo com barra de progresso (fetch ainda não informa o progresso do envio). */
export function enviarArquivo(canalId, arquivo, aoProgredir) {
  return new Promise((resolver, rejeitar) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', `/api/canais/${canalId}/anexos`)
    xhr.setRequestHeader('Authorization', `Bearer ${lerToken()}`)
    xhr.upload.onprogress = (e) => e.lengthComputable && aoProgredir?.(e.loaded / e.total)
    xhr.onload = () => {
      let dados = {}
      try {
        dados = JSON.parse(xhr.responseText)
      } catch {
        // resposta vazia
      }
      if (xhr.status >= 200 && xhr.status < 300) resolver(dados)
      else rejeitar(new ErroApi(dados.erro || 'Não foi possível enviar o arquivo.', xhr.status))
    }
    xhr.onerror = () => rejeitar(new ErroApi('Sem conexão com o servidor.', 0))
    const corpo = new FormData()
    corpo.append('arquivo', arquivo)
    xhr.send(corpo)
  })
}

/** Envia avatar ou banner (já recortado no navegador). */
async function enviarImagem(endereco, blob, nome) {
  const corpo = new FormData()
  corpo.append('arquivo', blob, blob.type === 'image/gif' ? `${nome}.gif` : `${nome}.webp`)
  const r = await fetch(endereco, { method: 'POST', headers: { Authorization: `Bearer ${lerToken()}` }, body: corpo })
  const dados = await r.json().catch(() => ({}))
  if (!r.ok) throw new ErroApi(dados.erro || 'Não foi possível enviar a imagem.', r.status)
  return dados
}

export const enviarImagemPerfil = (campo, blob) => enviarImagem(`/api/eu/${campo}`, blob, campo)
export const enviarIconeServidor = (id, blob) => enviarImagem(`/api/servidores/${id}/icone`, blob, 'icone')

/** Emoji novo do servidor (nome + imagem já reduzida). */
export async function enviarEmoji(servidorId, nome, blob) {
  const corpo = new FormData()
  corpo.append('nome', nome)
  corpo.append('arquivo', blob, blob.type === 'image/gif' ? 'emoji.gif' : 'emoji.webp')
  const r = await fetch(`/api/servidores/${servidorId}/emojis`, { method: 'POST', headers: { Authorization: `Bearer ${lerToken()}` }, body: corpo })
  const dados = await r.json().catch(() => ({}))
  if (!r.ok) throw new ErroApi(dados.erro || 'Não foi possível enviar o emoji.', r.status)
  return dados
}

/** Endereço de um anexo com o token (o <img> não manda o cabeçalho Authorization). */
export const enderecoAnexo = (endereco) => `${endereco}?t=${encodeURIComponent(lerToken() || '')}`

const sv = (id) => `/api/servidores/${id}`
const cn = (id) => `/api/canais/${id}`
const q = (obj) => {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(obj)) if (v !== undefined && v !== null && v !== '') p.set(k, v)
  const s = p.toString()
  return s ? `?${s}` : ''
}

export const api = {
  cadastrar: (dados) => pedir('POST', '/api/cadastro', dados),
  entrar: (login, senha) => pedir('POST', '/api/entrar', { login, senha }),
  eu: () => pedir('GET', '/api/eu'),
  alterarPerfil: (dados) => pedir('PATCH', '/api/eu', dados),
  salvarPreferencias: (prefs) => pedir('PUT', '/api/eu/preferencias', prefs),
  apagarImagemPerfil: (campo) => pedir('DELETE', `/api/eu/${campo}`),
  perfil: (id) => pedir('GET', `/api/usuarios/${id}`),
  mencoes: () => pedir('GET', '/api/eu/mencoes'),

  criarServidor: (nome) => pedir('POST', '/api/servidores', { nome }),
  servidor: (id) => pedir('GET', sv(id)),
  alterarServidor: (id, dados) => pedir('PATCH', sv(id), dados),
  apagarServidor: (id) => pedir('DELETE', sv(id)),
  apagarIconeServidor: (id) => pedir('DELETE', `${sv(id)}/icone`),
  sair: (id) => pedir('DELETE', `${sv(id)}/eu`),
  buscar: (id, filtros) => pedir('GET', `${sv(id)}/busca${q(filtros)}`),
  auditoria: (id) => pedir('GET', `${sv(id)}/auditoria`),

  criarCanal: (id, dados) => pedir('POST', `${sv(id)}/canais`, dados),
  alterarCanal: (id, canal, dados) => pedir('PATCH', `${sv(id)}/canais/${canal}`, dados),
  apagarCanal: (id, canal) => pedir('DELETE', `${sv(id)}/canais/${canal}`),
  criarCategoria: (id, nome) => pedir('POST', `${sv(id)}/categorias`, { nome }),
  alterarCategoria: (id, cat, dados) => pedir('PATCH', `${sv(id)}/categorias/${cat}`, dados),
  apagarCategoria: (id, cat) => pedir('DELETE', `${sv(id)}/categorias/${cat}`),
  sobrescritas: (id, canal) => pedir('GET', `${sv(id)}/canais/${canal}/permissoes`),
  definirSobrescrita: (id, canal, dados) => pedir('PUT', `${sv(id)}/canais/${canal}/permissoes`, dados),

  cargos: (id) => pedir('GET', `${sv(id)}/cargos`),
  criarCargo: (id, dados) => pedir('POST', `${sv(id)}/cargos`, dados),
  alterarCargo: (id, cargo, dados) => pedir('PATCH', `${sv(id)}/cargos/${cargo}`, dados),
  apagarCargo: (id, cargo) => pedir('DELETE', `${sv(id)}/cargos/${cargo}`),
  ordenarCargos: (id, ids) => pedir('PUT', `${sv(id)}/ordem-cargos`, ids),

  membros: (id) => pedir('GET', `${sv(id)}/membros`),
  apelido: (id, usuario, apelido) => pedir('PATCH', `${sv(id)}/membros/${usuario}/apelido`, { apelido }),
  darCargo: (id, usuario, cargo) => pedir('PUT', `${sv(id)}/membros/${usuario}/cargos/${cargo}`),
  tirarCargo: (id, usuario, cargo) => pedir('DELETE', `${sv(id)}/membros/${usuario}/cargos/${cargo}`),
  expulsar: (id, usuario, motivo) => pedir('DELETE', `${sv(id)}/membros/${usuario}`, { motivo }),
  castigar: (id, usuario, minutos, motivo) => pedir('PUT', `${sv(id)}/membros/${usuario}/castigo`, { minutos, motivo }),
  banidos: (id) => pedir('GET', `${sv(id)}/banimentos`),
  banir: (id, usuario, motivo) => pedir('PUT', `${sv(id)}/banimentos/${usuario}`, { motivo }),
  desbanir: (id, usuario) => pedir('DELETE', `${sv(id)}/banimentos/${usuario}`),

  convites: (id) => pedir('GET', `${sv(id)}/convites`),
  criarConvite: (id, canal, dados) => pedir('POST', `${sv(id)}/canais/${canal}/convites`, dados),
  apagarConvite: (id, codigo) => pedir('DELETE', `${sv(id)}/convites/${codigo}`),
  verConvite: (codigo) => pedir('GET', `/api/convites/${encodeURIComponent(codigo)}`),
  aceitarConvite: (codigo) => pedir('POST', `/api/convites/${encodeURIComponent(codigo)}`),

  mensagens: (canal, filtros = {}) => pedir('GET', `${cn(canal)}/mensagens${q(filtros)}`),
  enviar: (canal, dados) => pedir('POST', `${cn(canal)}/mensagens`, dados),
  editar: (canal, msg, conteudo) => pedir('PATCH', `${cn(canal)}/mensagens/${msg}`, { conteudo }),
  apagar: (canal, msg) => pedir('DELETE', `${cn(canal)}/mensagens/${msg}`),
  reagir: (canal, msg, emoji) => pedir('PUT', `${cn(canal)}/mensagens/${msg}/reacoes/${encodeURIComponent(emoji)}`),
  desreagir: (canal, msg, emoji) => pedir('DELETE', `${cn(canal)}/mensagens/${msg}/reacoes/${encodeURIComponent(emoji)}`),
  fixar: (canal, msg) => pedir('PUT', `${cn(canal)}/mensagens/${msg}/fixar`),

  amigos: () => pedir('GET', '/api/amigos'),
  pedirAmizadePorNome: (nomeUsuario) => pedir('POST', '/api/amigos', { nomeUsuario }),
  pedirAmizade: (id) => pedir('PUT', `/api/amigos/${id}`),
  removerAmigo: (id) => pedir('DELETE', `/api/amigos/${id}`),
  bloquear: (id) => pedir('PUT', `/api/bloqueios/${id}`),
  desbloquear: (id) => pedir('DELETE', `/api/bloqueios/${id}`),
  conversas: () => pedir('GET', '/api/conversas'),
  criarConversa: (usuarios, nome = '') => pedir('POST', '/api/conversas', { usuarios, nome }),
  alterarConversa: (id, dados) => pedir('PATCH', `/api/conversas/${id}`, dados),
  fecharConversa: (id) => pedir('DELETE', `/api/conversas/${id}`),
  adicionarAoGrupo: (id, usuarioId) => pedir('POST', `/api/conversas/${id}/membros`, { usuarioId }),
  tirarDoGrupo: (id, usuarioId) => pedir('DELETE', `/api/conversas/${id}/membros/${usuarioId}`),
  desfixar: (canal, msg) => pedir('DELETE', `${cn(canal)}/mensagens/${msg}/fixar`),
  fixadas: (canal) => pedir('GET', `${cn(canal)}/fixadas`),
  previa: (url) => pedir('GET', `/api/previa?url=${encodeURIComponent(url)}`),
  criarFio: (canal, msg, nome = '') => pedir('POST', `${cn(canal)}/mensagens/${msg}/fio`, { nome }),
  fios: (canal) => pedir('GET', `${cn(canal)}/fios`),
  alterarFio: (fio, nome) => pedir('PATCH', `${cn(fio)}/fio`, { nome }),
  apagarFio: (fio) => pedir('DELETE', `${cn(fio)}/fio`),
  votar: (canal, msg, opcoes) => pedir('PUT', `${cn(canal)}/mensagens/${msg}/votos`, { opcoes }),
  encerrarEnquete: (canal, msg) => pedir('POST', `${cn(canal)}/mensagens/${msg}/encerrar`),
  renomearEmoji: (id, emoji, nome) => pedir('PATCH', `${sv(id)}/emojis/${emoji}`, { nome }),
  apagarEmoji: (id, emoji) => pedir('DELETE', `${sv(id)}/emojis/${emoji}`),
  marcarLido: (canal, mensagemId) => pedir('POST', `${cn(canal)}/lido`, { mensagemId }),
  vozInfo: (canal) => pedir('GET', `${cn(canal)}/voz`),
}

export function enderecoGateway() {
  const protocolo = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocolo}//${location.host}/api/gateway?t=${encodeURIComponent(lerToken() || '')}`
}
