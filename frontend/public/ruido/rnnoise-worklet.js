// Supressão de ruído com IA: o RNNoise (rede neural da Xiph.org) roda aqui, dentro do
// AudioWorklet, numa thread de áudio separada. O binário (rnnoise.wasm, compilado pela
// Jitsi) chega pronto do navegador em processorOptions e é instanciado aqui mesmo, sem o
// código de apoio do Emscripten: ele só pede duas funções (copiar e aumentar a memória).
//
// O RNNoise trabalha em quadros de 480 amostras (10 ms a 48 kHz) e o navegador entrega
// blocos de 128, então há uma fila: a saída atrasa no máximo 10 ms.

const QUADRO = 480
const ESCALA = 32768 // o RNNoise espera amostras na escala de 16 bits
const MAX_HEAP = 2147483648

class Rnnoise extends AudioWorkletProcessor {
  constructor(opcoes) {
    super()
    let memoria
    const importacoes = {
      a: {
        // emscripten_resize_heap
        a: (pedido) => {
          pedido >>>= 0
          const atual = memoria.buffer.byteLength
          if (pedido > MAX_HEAP) return 0
          const alvo = Math.min(MAX_HEAP, Math.ceil(Math.max(pedido, atual * 1.2) / 65536) * 65536)
          try {
            memoria.grow((alvo - atual) / 65536)
            return 1
          } catch {
            return 0
          }
        },
        // emscripten_memcpy_big
        b: (destino, origem, n) => {
          new Uint8Array(memoria.buffer).copyWithin(destino, origem, origem + n)
        },
      },
    }
    const instancia = new WebAssembly.Instance(new WebAssembly.Module(opcoes.processorOptions.wasm), importacoes)
    const f = instancia.exports
    memoria = f.c
    f.d() // construtores
    this.f = f
    this.memoria = memoria
    this.estado = f.f(0) // rnnoise_create(modelo padrão)
    this.ptr = f.g(QUADRO * 4) // malloc
    this.quadro = new Float32Array(QUADRO)
    this.cheio = 0
    // fila de saída (começa com um quadro de silêncio para nunca faltar amostra)
    this.fila = new Float32Array(4096)
    this.ler = 0
    this.escrever = QUADRO
    this.vivo = true
    this.port.onmessage = (e) => {
      if (e.data === 'parar') this.parar()
    }
  }

  parar() {
    if (!this.vivo) return
    this.vivo = false
    this.f.i(this.ptr) // free
    this.f.h(this.estado) // rnnoise_destroy
  }

  processarQuadro() {
    const heap = new Float32Array(this.memoria.buffer, this.ptr, QUADRO)
    for (let i = 0; i < QUADRO; i++) heap[i] = this.quadro[i] * ESCALA
    this.f.j(this.estado, this.ptr, this.ptr) // rnnoise_process_frame (devolve a chance de ter voz)
    const saida = new Float32Array(this.memoria.buffer, this.ptr, QUADRO)
    const n = this.fila.length
    for (let i = 0; i < QUADRO; i++) {
      this.fila[this.escrever] = saida[i] / ESCALA
      this.escrever = (this.escrever + 1) % n
    }
  }

  process(entradas, saidas) {
    if (!this.vivo) return false
    const entrada = entradas[0]?.[0]
    const saida = saidas[0]
    const tamanho = saida[0].length
    const n = this.fila.length
    for (let i = 0; i < tamanho; i++) {
      this.quadro[this.cheio++] = entrada ? entrada[i] : 0
      if (this.cheio === QUADRO) {
        this.processarQuadro()
        this.cheio = 0
      }
      let v = 0
      if (this.ler !== this.escrever) {
        v = this.fila[this.ler]
        this.ler = (this.ler + 1) % n
      }
      for (const canal of saida) canal[i] = v
    }
    return true
  }
}

registerProcessor('rnnoise', Rnnoise)
