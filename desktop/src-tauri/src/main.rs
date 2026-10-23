// Sem janela de terminal no Windows (versão final).
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    prosa_desktop_lib::run()
}
