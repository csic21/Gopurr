#![cfg_attr(windows, windows_subsystem = "windows")]

#[cfg(windows)]
mod desktop;

#[cfg(windows)]
fn main() {
    if let Err(error) = desktop::run() {
        let message = format!("Gopurr could not start: {error}");
        if let Ok(path) = std::env::var("GOPURR_NATIVE_SESSION_OUTPUT") {
            let _ = std::fs::write(
                std::path::PathBuf::from(path).with_extension("error.log"),
                &message,
            );
        } else {
            nsh_platform_windows::fatal_error(&message);
        }
        std::process::exit(1);
    }
}

#[cfg(not(windows))]
fn main() {
    eprintln!("The Gopurr NSH Windows host requires Windows 10 1809 or later.");
    std::process::exit(1);
}
