/**
 * Recorta a imagem no meio (como "cobrir") e converte para WebP no tamanho certo.
 * GIFs pequenos vão como estão, para não perder a animação.
 */
export async function recortar(arquivo, largura, altura, { manterGif = false } = {}) {
  if (manterGif && arquivo.type === 'image/gif' && arquivo.size <= 4 << 20) return arquivo
  const img = await createImageBitmap(arquivo)
  const escala = Math.max(largura / img.width, altura / img.height)
  const w = img.width * escala
  const h = img.height * escala
  const canvas = document.createElement('canvas')
  canvas.width = largura
  canvas.height = altura
  canvas.getContext('2d').drawImage(img, (largura - w) / 2, (altura - h) / 2, w, h)
  return new Promise((ok) => canvas.toBlob(ok, 'image/webp', 0.88))
}
