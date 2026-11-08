#![cfg_attr(windows, windows_subsystem = "windows")]

#[cfg(windows)]
mod desktop;

#[cfg(windows)]
fn main() {
    if let Err(error) = desktop::run() {
        nsh_platform_windows::fatal_error(&format!("Gopurr could not start: {error}"));
        std::process::exit(1);
    }
}

#[cfg(not(windows))]
fn main() {
    eprintln!("The Gopurr NSH Windows host requires Windows 10 1809 or later.");
    std::process::exit(1);
}
