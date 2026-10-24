fn main() {
    // Os comandos do programa que o site (carregado do servidor) pode chamar. A permissão
    // de cada um ("allow-...") é dada em capabilities/padrao.json.
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&["definir_ptt", "versao_desktop"]),
    ))
    .expect("falha ao preparar o build do Tauri");
}
