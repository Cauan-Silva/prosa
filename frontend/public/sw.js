// Service worker mínimo: só existe para o navegador oferecer "Instalar o Prosa".
// Não guarda nada em cache, então cada atualização do site chega na hora.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()))
self.addEventListener('fetch', () => {})
