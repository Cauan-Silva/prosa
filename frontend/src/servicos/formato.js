const hora = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' })
const dia = new Intl.DateTimeFormat('pt-BR', { day: 'numeric', month: 'long', year: 'numeric' })
const completo = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'full', timeStyle: 'short' })

export const formatarHora = (d) => hora.format(new Date(d))
export const formatarCompleto = (d) => completo.format(new Date(d))

export function formatarDia(d) {
  const data = new Date(d)
  const hoje = new Date()
  const ontem = new Date(hoje)
  ontem.setDate(hoje.getDate() - 1)
  if (data.toDateString() === hoje.toDateString()) return 'Hoje'
  if (data.toDateString() === ontem.toDateString()) return 'Ontem'
  return dia.format(data)
}

export function quando(d) {
  const data = new Date(d)
  const hoje = new Date()
  if (data.toDateString() === hoje.toDateString()) return `hoje às ${formatarHora(d)}`
  return `${formatarDia(d).replace(/^Ontem$/, 'ontem')} às ${formatarHora(d)}`
}

export function tamanho(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} MB`
}

export const corHex = (n) => (n ? `#${n.toString(16).padStart(6, '0')}` : null)
