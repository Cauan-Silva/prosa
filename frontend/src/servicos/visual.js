// Personalização do perfil: as listas que a tela de configurações mostra. Os nomes (id)
// são os mesmos que o servidor aceita; o desenho de cada um está no estilos.css.

export const MOLDURAS = [
  ['', 'Nenhuma'],
  ['neon', 'Neon'],
  ['arco-iris', 'Arco-íris'],
  ['ouro', 'Ouro'],
  ['azulejo', 'Azulejo'],
  ['gelo', 'Gelo'],
  ['chamas', 'Chamas'],
  ['folhas', 'Folhas'],
  ['estrelas', 'Estrelas'],
  ['orbita', 'Órbita'],
  ['pixel', 'Pixel'],
  ['fones', 'Fones'],
  ['coroa', 'Coroa'],
]

export const EFEITOS = [
  ['', 'Nenhum'],
  ['confete', 'Confete'],
  ['neve', 'Neve'],
  ['estrelas', 'Estrelas'],
  ['bolhas', 'Bolhas'],
  ['petalas', 'Pétalas'],
  ['coracoes', 'Corações'],
  ['vagalumes', 'Vaga-lumes'],
  ['fogos', 'Fogos'],
  ['aurora', 'Aurora'],
  ['codigo', 'Código'],
]

export const ESTILOS_NOME = [
  ['', 'Normal'],
  ['gradiente', 'Gradiente'],
  ['neon', 'Neon'],
  ['arco-iris', 'Arco-íris'],
  ['ouro', 'Ouro'],
  ['glitch', 'Glitch'],
]

// pares de cores prontos para o tema do cartão
export const TEMAS = [
  [0x1f3fa6, 0x0b1430],
  [0x7c5cff, 0xd63a7a],
  [0x22b8cf, 0x1f3fa6],
  [0xf2994a, 0xd63a7a],
  [0x3fbf7f, 0x14635a],
  [0xe8b420, 0x9c2f4a],
  [0x2b2d42, 0x8d99ae],
  [0x111111, 0x5b6478],
]

export const hex = (n) => '#' + Math.max(0, Math.min(0xffffff, n | 0)).toString(16).padStart(6, '0')

/** O tema é escuro? (para escolher texto claro ou escuro por cima) */
export function temaEscuro([a, b]) {
  const lum = (n) => {
    const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
      v /= 255
      return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
  }
  return (lum(a) + lum(b)) / 2 < 0.35
}
