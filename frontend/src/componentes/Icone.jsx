// Ícones de traço simples (24×24), herdam a cor do texto.
const CAMINHOS = {
  microfone: 'M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3Z M5 11a7 7 0 0 0 14 0 M12 18v3',
  'microfone-off': 'M9 9v3a3 3 0 0 0 5.1 2.1 M15 9.3V6a3 3 0 0 0-5.7-1.3 M5 11a7 7 0 0 0 11.6 5.3 M19 11a7 7 0 0 1-.6 2.8 M12 18v3 M3 3l18 18',
  camera: 'M3 7h11a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1Z M15 10l6-3v10l-6-3',
  'camera-off': 'M8 7h6a1 1 0 0 1 1 1v6 M15 10l6-3v10l-6-3 M14.5 17H3a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1h1 M3 3l18 18',
  tela: 'M3 4h18v12H3z M8 20h8 M12 16v4 M9 10l3-3 3 3 M12 7v6',
  sair: 'M10 14l-2 2m0 0 2 2m-2-2h6 M3 10c5-4 13-4 18 0l-2 3-3-1v-2c-2.6-.6-5.4-.6-8 0v2l-3 1z',
  copiar: 'M9 9h10v10H9z M5 15V5h10',
  config: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z M19.4 13a7.6 7.6 0 0 0 0-2l2-1.5-2-3.4-2.4 1a7.6 7.6 0 0 0-1.7-1L15 3.5h-4l-.4 2.6a7.6 7.6 0 0 0-1.7 1l-2.4-1-2 3.4L6.6 11a7.6 7.6 0 0 0 0 2l-2 1.5 2 3.4 2.4-1a7.6 7.6 0 0 0 1.7 1l.4 2.6h4l.4-2.6a7.6 7.6 0 0 0 1.7-1l2.4 1 2-3.4z',
  voltar: 'M15 18l-6-6 6-6',
  hashtag: 'M5 9h14 M5 15h14 M10 4 8 20 M16 4l-2 16',
  voz: 'M4 10v4h3l5 4V6L7 10z M16 9a4 4 0 0 1 0 6 M18.5 6.5a8 8 0 0 1 0 11',
  mais: 'M12 5v14 M5 12h14',
  fechar: 'M6 6l12 12 M18 6 6 18',
  buscar: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14Z M20 20l-4-4',
  pessoas: 'M9 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z M2.5 20a6.5 6.5 0 0 1 13 0 M16 4.3a3.5 3.5 0 0 1 0 6.4 M18 14a6.5 6.5 0 0 1 3.5 6',
  anexo: 'M20 11.5 12.4 19a5 5 0 0 1-7.1-7.1l7.6-7.6a3.3 3.3 0 0 1 4.7 4.7l-7.6 7.6a1.7 1.7 0 0 1-2.4-2.4L14.7 7',
  enviar: 'M4 12 20 4l-4 16-4-6z M12 14l8-10',
  responder: 'M10 8 4 13l6 5 M4 13h10a6 6 0 0 1 6 6',
  editar: 'M4 20h4L19 9l-4-4L4 16z M13.5 6.5l4 4',
  lixo: 'M4 7h16 M10 11v6 M14 11v6 M6 7l1 13h10l1-13 M9 7V4h6v3',
  emoji: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z M8.5 14a4 4 0 0 0 7 0 M9 9.5h.01 M15 9.5h.01',
  menu: 'M4 7h16 M4 12h16 M4 17h16',
  arroba: 'M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z M16 12v1.5a2.5 2.5 0 0 0 5 0V12a9 9 0 1 0-3.5 7.1',
  cadeado: 'M6 11h12v9H6z M8.5 11V8a3.5 3.5 0 0 1 7 0v3',
  convite: 'M15 8a3 3 0 1 0 0-.01 M3 20a6 6 0 0 1 9-5.2 M19 14v6 M16 17h6 M9 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z',
  seta: 'M6 9l6 6 6-6',
  sol: 'M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z M12 2v2 M12 20v2 M4.9 4.9l1.4 1.4 M17.7 17.7l1.4 1.4 M2 12h2 M20 12h2 M4.9 19.1l1.4-1.4 M17.7 6.3l1.4-1.4',
  lua: 'M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5Z',
  bussola: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z M15.5 8.5l-2 5-5 2 2-5z',
  sinal: 'M5 19v-3 M10 19v-7 M15 19V9 M20 19V5',
  desligar: 'M3 14c4-4 14-4 18 0l-2.5 2.5-3-1.2V13a12 12 0 0 0-7 0v2.3l-3 1.2z',
  fone: 'M4 15v-3a8 8 0 0 1 16 0v3 M4 15h3v5H5a1 1 0 0 1-1-1z M20 15h-3v5h2a1 1 0 0 0 1-1z',
  'fone-off': 'M4 15v-3a8 8 0 0 1 12.5-6.6 M20 12v3 M4 15h3v5H5a1 1 0 0 1-1-1z M20 15h-3v5h2a1 1 0 0 0 1-1z M3 3l18 18',
}

export default function Icone({ nome, tamanho = 20 }) {
  return (
    <svg width={tamanho} height={tamanho} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={CAMINHOS[nome]} />
    </svg>
  )
}
