# Prosa para Windows

Programa feito com [Tauri 2](https://tauri.app): uma janela com o site do Prosa (o mesmo do servidor) mais o que o navegador não consegue fazer:

- **Pressionar para falar global**: a tecla escolhida funciona com o Prosa em segundo plano, até em jogo em tela cheia. Uma thread consulta a tecla direto no Windows (`GetAsyncKeyState`), então a tecla continua funcionando normalmente nos outros programas.
- **Ícone na bandeja** (ao lado do relógio): fechar a janela só esconde, e a voz continua. Para sair, clique com o botão direito no ícone.
- **Abrir junto com o Windows** (em Configurações → App), começando escondido.
- **Uma instância só**: abrir de novo mostra a janela que já está aberta.

O instalador tem uns 3 MB, porque usa o WebView2, o motor do Edge que já vem no Windows 10 e 11.

## Como gerar o instalador

O GitHub Actions compila numa máquina Windows (`.github/workflows/desktop.yml`):

- **Todo push que mexe em `desktop/`** confere se compila. O instalador fica em *Actions → Programa para Windows → Artifacts*.
- **Para publicar uma versão** (é ela que o botão "Baixar para Windows" do site baixa):

```powershell
git tag desktop-v0.1.0
git push origin desktop-v0.1.0
```

A versão aparece em *Releases* com o arquivo `Prosa_0.1.0_x64-setup.exe`.

Como o instalador não tem assinatura digital paga, o Windows pode mostrar "O Windows protegeu o computador". É só clicar em **Mais informações → Executar assim mesmo**.

## Endereço do servidor

O programa abre `https://prosa-s4u4.onrender.com`. Se o endereço mudar, troque-o em dois lugares: `src-tauri/tauri.conf.json` (`app.windows[0].url`) e `src-tauri/capabilities/padrao.json` (`remote.urls`), que dá ao site permissão para falar com o programa.

## Rodar no seu computador (opcional)

Precisa do [Rust](https://rustup.rs) e do Node:

```powershell
cd desktop
npm install
npm run dev
```
