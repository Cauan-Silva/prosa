//! Prosa para Windows: uma janela com o site do Prosa e o que o navegador não faz:
//!
//! - **Pressionar para falar global**: uma thread consulta a tecla escolhida direto no
//!   Windows (GetAsyncKeyState) e avisa o site ("prosa-ptt"). Funciona com o Prosa em
//!   segundo plano, até em jogo em tela cheia, e não "rouba" a tecla dos outros programas.
//! - **Bandeja**: fechar a janela só esconde (a voz continua); sair pelo ícone.
//! - **Abrir junto com o Windows** (plugin autostart), começando escondido.
//! - **Uma instância só**: abrir de novo mostra a janela que já existe.

use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;

use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager, WindowEvent};

const JANELA: &str = "principal";
const MINIMIZADO: &str = "--minimizado";

/// Tecla do "pressionar para falar" (código virtual do Windows; 0 = desligado).
struct Ptt {
    tecla: AtomicU32,
}

/// O site manda o código da tecla (KeyboardEvent.code, ex.: "KeyV"); "" desliga.
#[tauri::command]
fn definir_ptt(codigo: String, ptt: tauri::State<'_, Arc<Ptt>>) -> bool {
    let vk = tecla_virtual(&codigo).unwrap_or(0);
    ptt.tecla.store(vk, Ordering::Relaxed);
    vk != 0
}

#[tauri::command]
fn versao_desktop() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Converte KeyboardEvent.code para o código virtual de tecla do Windows.
fn tecla_virtual(codigo: &str) -> Option<u32> {
    if let Some(letra) = codigo.strip_prefix("Key") {
        let b = letra.as_bytes();
        if b.len() == 1 && b[0].is_ascii_uppercase() {
            return Some(b[0] as u32); // 'A'..'Z' = 0x41..0x5A
        }
    }
    if let Some(d) = codigo.strip_prefix("Digit") {
        if let Ok(n) = d.parse::<u32>() {
            if n <= 9 {
                return Some(0x30 + n);
            }
        }
    }
    if let Some(d) = codigo.strip_prefix("Numpad") {
        if let Ok(n) = d.parse::<u32>() {
            if n <= 9 {
                return Some(0x60 + n);
            }
        }
    }
    if let Some(f) = codigo.strip_prefix('F') {
        if let Ok(n) = f.parse::<u32>() {
            if (1..=24).contains(&n) {
                return Some(0x70 + n - 1);
            }
        }
    }
    Some(match codigo {
        "Space" => 0x20,
        "Tab" => 0x09,
        "CapsLock" => 0x14,
        "ShiftLeft" => 0xA0,
        "ShiftRight" => 0xA1,
        "ControlLeft" => 0xA2,
        "ControlRight" => 0xA3,
        "AltLeft" => 0xA4,
        "AltRight" => 0xA5,
        "MetaLeft" => 0x5B,
        "MetaRight" => 0x5C,
        "ContextMenu" => 0x5D,
        "Backquote" => 0xC0,
        "Minus" => 0xBD,
        "Equal" => 0xBB,
        "BracketLeft" => 0xDB,
        "BracketRight" => 0xDD,
        "Backslash" => 0xDC,
        "IntlBackslash" => 0xE2,
        "Semicolon" => 0xBA,
        "Quote" => 0xDE,
        "Comma" => 0xBC,
        "Period" => 0xBE,
        "Slash" => 0xBF,
        "IntlRo" => 0xC1,
        "Insert" => 0x2D,
        "Delete" => 0x2E,
        "Home" => 0x24,
        "End" => 0x23,
        "PageUp" => 0x21,
        "PageDown" => 0x22,
        "ArrowLeft" => 0x25,
        "ArrowUp" => 0x26,
        "ArrowRight" => 0x27,
        "ArrowDown" => 0x28,
        "Pause" => 0x13,
        "ScrollLock" => 0x91,
        "NumLock" => 0x90,
        "NumpadAdd" => 0x6B,
        "NumpadSubtract" => 0x6D,
        "NumpadMultiply" => 0x6A,
        "NumpadDivide" => 0x6F,
        "NumpadDecimal" => 0x6E,
        _ => return None,
    })
}

#[cfg(windows)]
fn apertada(vk: u32) -> bool {
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::GetAsyncKeyState;
    // bit mais alto = a tecla está apertada agora
    unsafe { (GetAsyncKeyState(vk as i32) as u16 & 0x8000) != 0 }
}

#[cfg(not(windows))]
fn apertada(_vk: u32) -> bool {
    false
}

/// Consulta a tecla ~80 vezes por segundo e avisa o site só quando o estado muda.
fn vigiar_ptt(app: AppHandle, ptt: Arc<Ptt>) {
    std::thread::spawn(move || {
        let mut antes = false;
        loop {
            let vk = ptt.tecla.load(Ordering::Relaxed);
            let agora = vk != 0 && apertada(vk);
            if agora != antes {
                antes = agora;
                let _ = app.emit("prosa-ptt", agora);
            }
            std::thread::sleep(Duration::from_millis(if vk == 0 { 200 } else { 12 }));
        }
    });
}

fn mostrar(app: &AppHandle) {
    if let Some(janela) = app.get_webview_window(JANELA) {
        let _ = janela.show();
        let _ = janela.unminimize();
        let _ = janela.set_focus();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let ptt = Arc::new(Ptt {
        tecla: AtomicU32::new(0),
    });
    let ptt_vigia = ptt.clone();

    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _pasta| {
            mostrar(app)
        }))
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            Some(vec![MINIMIZADO]),
        ))
        .manage(ptt)
        .invoke_handler(tauri::generate_handler![definir_ptt, versao_desktop])
        .setup(move |app| {
            let abrir = MenuItem::with_id(app, "abrir", "Abrir o Prosa", true, None::<&str>)?;
            let sair_voz =
                MenuItem::with_id(app, "sair-voz", "Sair da sala de voz", true, None::<&str>)?;
            let sair = MenuItem::with_id(app, "sair", "Sair do Prosa", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&abrir, &sair_voz, &sair])?;
            let mut bandeja = TrayIconBuilder::with_id("prosa")
                .tooltip("Prosa")
                .menu(&menu)
                .on_menu_event(|app, evento| match evento.id.as_ref() {
                    "abrir" => mostrar(app),
                    // a janela pode estar escondida: o site sai da sala mesmo assim
                    "sair-voz" => {
                        let _ = app.emit("prosa-sair-voz", ());
                    }
                    "sair" => app.exit(0),
                    _ => {}
                })
                .on_tray_icon_event(|icone, evento| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = evento
                    {
                        mostrar(icone.app_handle());
                    }
                });
            if let Some(imagem) = app.default_window_icon() {
                bandeja = bandeja.icon(imagem.clone());
            }
            bandeja.build(app)?;

            // aberto junto com o Windows: começa escondido na bandeja
            if std::env::args().any(|a| a == MINIMIZADO) {
                if let Some(janela) = app.get_webview_window(JANELA) {
                    let _ = janela.hide();
                }
            }
            vigiar_ptt(app.handle().clone(), ptt_vigia);
            Ok(())
        })
        .on_window_event(|janela, evento| {
            // fechar só esconde: a voz continua e o Prosa fica na bandeja
            if let WindowEvent::CloseRequested { api, .. } = evento {
                api.prevent_close();
                let _ = janela.hide();
            }
        })
        .run(tauri::generate_context!())
        .expect("não foi possível abrir o Prosa");
}

#[cfg(test)]
mod testes {
    use super::tecla_virtual;

    #[test]
    fn converte_teclas() {
        assert_eq!(tecla_virtual("KeyV"), Some(0x56));
        assert_eq!(tecla_virtual("Digit0"), Some(0x30));
        assert_eq!(tecla_virtual("F13"), Some(0x7C));
        assert_eq!(tecla_virtual("Space"), Some(0x20));
        assert_eq!(tecla_virtual("CapsLock"), Some(0x14));
        assert_eq!(tecla_virtual(""), None);
        assert_eq!(tecla_virtual("Keyv"), None);
    }
}
