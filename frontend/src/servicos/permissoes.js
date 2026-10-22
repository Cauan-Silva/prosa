// As mesmas permissões do servidor (internal/permissoes), na mesma ordem de bits.
// Os valores passam de 2^31, então as contas usam BigInt para não perder bits.

const nomes = [
  ['VerCanal', 'Ver canais', 'Ver o canal e as mensagens que chegam'],
  ['LerHistorico', 'Ler mensagens antigas', 'Rolar para cima e ver o que foi dito antes'],
  ['EnviarMensagens', 'Enviar mensagens', ''],
  ['GerenciarMensagens', 'Gerenciar mensagens', 'Apagar mensagens dos outros e ignorar o modo lento'],
  ['AnexarArquivos', 'Anexar arquivos', ''],
  ['AdicionarReacoes', 'Reagir', 'Adicionar reações novas (somar a uma existente é sempre possível)'],
  ['MencionarTodos', 'Mencionar @todos', 'Avisar todo mundo e mencionar cargos não mencionáveis'],
  ['ConectarVoz', 'Entrar na voz', ''],
  ['Falar', 'Falar', ''],
  ['Video', 'Câmera e tela', 'Ligar a câmera e compartilhar a tela'],
  ['SilenciarNaVoz', 'Silenciar na voz', 'Silenciar e tirar outras pessoas da sala de voz'],
  ['CriarConvite', 'Criar convites', ''],
  ['AlterarApelido', 'Mudar o próprio apelido', ''],
  ['GerenciarApelidos', 'Mudar apelidos', 'Mudar o apelido dos outros'],
  ['GerenciarCanais', 'Gerenciar canais', 'Criar, editar e apagar canais e categorias'],
  ['GerenciarCargos', 'Gerenciar cargos', 'Criar e editar cargos abaixo do seu'],
  ['Expulsar', 'Expulsar', ''],
  ['Banir', 'Banir', ''],
  ['Castigar', 'Castigar', 'Deixar alguém só lendo por um tempo'],
  ['VerAuditoria', 'Ver registro de auditoria', ''],
  ['GerenciarServidor', 'Gerenciar servidor', 'Mudar o nome do servidor e gerenciar convites'],
  ['Administrador', 'Administrador', 'Pode tudo, em todos os canais. Dê com cuidado'],
]

export const P = {}
export const LISTA = nomes.map(([chave, nome, dica], i) => {
  P[chave] = 1n << BigInt(i)
  return { chave, nome, dica, bit: 1n << BigInt(i) }
})

export const big = (v) => BigInt(v || 0)
export const tem = (valor, p) => (big(valor) & p) === p

/** Grupos para a tela de cargos e de permissões do canal. */
export const GRUPOS = [
  { nome: 'Texto', chaves: ['VerCanal', 'LerHistorico', 'EnviarMensagens', 'AnexarArquivos', 'AdicionarReacoes', 'MencionarTodos', 'GerenciarMensagens'] },
  { nome: 'Voz', chaves: ['ConectarVoz', 'Falar', 'Video', 'SilenciarNaVoz'] },
  { nome: 'Membros', chaves: ['CriarConvite', 'AlterarApelido', 'GerenciarApelidos', 'Expulsar', 'Banir', 'Castigar'] },
  { nome: 'Servidor', chaves: ['GerenciarCanais', 'GerenciarCargos', 'VerAuditoria', 'GerenciarServidor', 'Administrador'] },
]
export const porChave = Object.fromEntries(LISTA.map((p) => [p.chave, p]))

// O JSON chega como número; os bits cabem com folga em 2^53, então dá para voltar.
export const numero = (b) => Number(b)
