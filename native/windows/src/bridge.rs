//! The Win32 frontend's native bridge to the Go desktop backend.
//!
//! The backend is still the existing Go application, running in its own
//! process. This module only starts it, owns its private session directory,
//! and speaks the same authenticated Unix-socket JSON protocol the other
//! native hosts use. Keeping the bridge here rather than in a c-shared Go
//! library means the UI process runs no second Go runtime.

use serde_json::{Value, json};
use std::{
    fs::OpenOptions,
    io::{Read, Write},
    os::windows::{
        io::{AsRawHandle, IntoRawHandle},
        process::CommandExt,
    },
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
    ptr,
    sync::{Arc, Mutex, OnceLock},
    thread,
    time::{Duration, Instant},
};
use windows::{
    Win32::{
        Foundation::{CloseHandle, ERROR_LOCK_VIOLATION, ERROR_SUCCESS, HANDLE, HLOCAL, LocalFree},
        Networking::WinSock::{
            AF_UNIX, SEND_RECV_FLAGS, SO_RCVTIMEO, SO_SNDTIMEO, SOCK_STREAM, SOCKADDR, SOCKADDR_UN,
            SOCKET, SOL_SOCKET, WSADATA, WSAGetLastError, WSASocketW, WSAStartup, closesocket,
            connect, recv, send, setsockopt,
        },
        Security::{
            Authorization::{
                ConvertSidToStringSidW, ConvertStringSecurityDescriptorToSecurityDescriptorW,
                GetNamedSecurityInfoW, SDDL_REVISION_1, SE_FILE_OBJECT, SetNamedSecurityInfoW,
            },
            Cryptography::{BCRYPT_USE_SYSTEM_PREFERRED_RNG, BCryptGenRandom},
            DACL_SECURITY_INFORMATION, GetSecurityDescriptorDacl, GetSecurityDescriptorOwner,
            OWNER_SECURITY_INFORMATION, PROTECTED_DACL_SECURITY_INFORMATION, PSECURITY_DESCRIPTOR,
            PSID,
        },
        Storage::FileSystem::{
            BY_HANDLE_FILE_INFORMATION, CreateFileW, FILE_FLAG_BACKUP_SEMANTICS,
            FILE_READ_ATTRIBUTES, FILE_SHARE_DELETE, FILE_SHARE_READ, FILE_SHARE_WRITE,
            GetFileInformationByHandle, LOCKFILE_EXCLUSIVE_LOCK, LOCKFILE_FAIL_IMMEDIATELY,
            LockFileEx, OPEN_EXISTING,
        },
        System::{
            IO::OVERLAPPED,
            Threading::{GetCurrentProcessId},
        },
    },
    core::{HSTRING, PWSTR},
};

const MAX_RESPONSE: usize = 32 << 20;
const STARTUP_TIMEOUT: Duration = Duration::from_secs(30);
const DEFAULT_CALL_TIMEOUT: Duration = Duration::from_secs(155);
const EVENTS_CALL_TIMEOUT: Duration = Duration::from_secs(2);
const MAX_SOCKET_PATH: usize = 107;

struct OwnedHandle(HANDLE);
impl Drop for OwnedHandle {
    fn drop(&mut self) {
        unsafe {
            let _ = CloseHandle(self.0);
        }
    }
}

struct Socket(SOCKET);
impl Drop for Socket {
    fn drop(&mut self) {
        unsafe {
            let _ = closesocket(self.0);
        }
    }
}

struct StartupLog {
    tail: Arc<Mutex<Vec<u8>>>,
    path: Option<PathBuf>,
}

impl StartupLog {
    fn new() -> Self {
        Self {
            tail: Arc::new(Mutex::new(Vec::new())),
            path: std::env::var_os("GOPURR_NATIVE_SESSION_OUTPUT")
                .map(|p| PathBuf::from(p).with_extension("backend.log")),
        }
    }

    fn read(&self, mut reader: impl Read) {
        let mut buffer = [0u8; 8192];
        loop {
            match reader.read(&mut buffer) {
                Ok(0) => break,
                Ok(n) => {
                    if let Ok(mut tail) = self.tail.lock() {
                        tail.extend_from_slice(&buffer[..n]);
                        if tail.len() > 8192 {
                            let excess = tail.len() - 8192;
                            tail.drain(..excess);
                        }
                    }
                    if let Some(path) = &self.path {
                        if let Ok(mut file) =
                            OpenOptions::new().create(true).append(true).open(path)
                        {
                            let _ = file.write_all(&buffer[..n]);
                        }
                    }
                }
                Err(_) => break,
            }
        }
    }

    fn detail(&self) -> String {
        self.tail
            .lock()
            .map(|tail| String::from_utf8_lossy(&tail).trim().to_owned())
            .unwrap_or_default()
    }
}

/// One frontend session with the Go backend.
pub struct NativeBridge {
    token: String,
    socket: PathBuf,
    pub pid: u32,
}

impl NativeBridge {
    /// Starts the backend and waits until its authenticated socket answers.
    ///
    /// The backend owns the established desktop model. This bridge starts it
    /// with the same private session and lifecycle arguments, then talks to
    /// it through the authenticated socket. No Go runtime enters this process.
    pub fn open(
        root: &Path,
        home: &Path,
        production: bool,
        frontend_tray: bool,
    ) -> Result<(Arc<Self>, Value), String> {
        if !home.is_absolute() {
            return Err("open_failed: An absolute desktop data directory is required".into());
        }
        let executable = root.join("gopurr-desktop.exe");
        let frontend = root.join("Gopurr.exe");
        if !executable.is_file() || !frontend.is_file() {
            return Err("open_failed: The desktop helper is missing".into());
        }
        match desktop_home_ready(home) {
            // The backend takes the authoritative home lock after startup.
            // This preflight only waits out a previous version's cleanup.
            Ok(lock) => drop(lock),
            Err(HomeError::InUse) => return Err(already_running(home)),
            Err(HomeError::Other(error)) => {
                return Err(format!("open_failed: {error}"));
            }
        }

        let session =
            SessionDir::new(session_directory().map_err(|e| format!("open_failed: {e}"))?);
        start_backend(
            &executable,
            &frontend,
            home,
            session,
            production,
            frontend_tray,
        )
    }

    /// Sends one authenticated request to the backend.
    pub fn request(&self, method: &str, params: Value) -> Result<Value, String> {
        let timeout = if method == "host.events" {
            EVENTS_CALL_TIMEOUT
        } else {
            DEFAULT_CALL_TIMEOUT
        };
        rpc(&self.socket, &self.token, method, params, timeout)
    }

    /// Sends a request and unwraps the backend's result/error envelope.
    pub fn result(&self, method: &str, params: Value) -> Result<Value, String> {
        let reply = self.request(method, params)?;
        if let Some(error) = reply.get("error") {
            return Err(error["message"]
                .as_str()
                .unwrap_or("Desktop operation failed")
                .to_owned());
        }
        Ok(reply["result"].clone())
    }

    /// Reports the window's visibility without delaying the UI thread.
    pub fn visible(self: &Arc<Self>, visible: bool) {
        let bridge = self.clone();
        thread::spawn(move || {
            let _ = bridge.result("host.visible", json!({"visible": visible}));
        });
    }
}

fn start_backend(
    executable: &Path,
    frontend: &Path,
    home: &Path,
    session: SessionDir,
    production: bool,
    frontend_tray: bool,
) -> Result<(Arc<NativeBridge>, Value), String> {
    let token = random_token().map_err(|e| format!("open_failed: {e}"))?;
    let token_file = session.path().join("auth");
    let socket = session.path().join("rpc");
    if let Err(error) = write_private_file(&token_file, token.as_bytes()) {
        return Err(format!("open_failed: {error}"));
    }

    let startup_log = StartupLog::new();
    let stderr_log = startup_log.tail.clone();
    let mut command = Command::new(executable);
    command
        .arg("--native-backend")
        .arg("--home")
        .arg(home)
        .arg("--socket")
        .arg(&socket)
        .arg("--token-file")
        .arg(&token_file)
        .arg("--frontend")
        .arg(frontend)
        .arg("--parent")
        .arg(unsafe { GetCurrentProcessId() }.to_string())
        .arg("--wait-for-frontend-exit")
        .stdout(Stdio::null())
        .stderr(Stdio::piped())
        .creation_flags(0x08000000); // CREATE_NO_WINDOW
    if frontend_tray {
        command.arg("--frontend-tray");
    }
    if production {
        command.arg("--production-home");
    }
    let mut child = command.spawn().map_err(|e| format!("open_failed: {e}"))?;
    let stderr = match child.stderr.take() {
        Some(stderr) => stderr,
        None => {
            spawn_backend_task(BackendOwnership {
                child,
                session,
                socket: socket.clone(),
                token: token.clone(),
            });
            return Err("open_failed: Desktop helper stderr is unavailable".into());
        }
    };
    let log_reader = StartupLog {
        tail: stderr_log,
        path: startup_log.path.clone(),
    };
    if let Err(error) = thread::Builder::new()
        .name("gopurr-backend-log".into())
        .spawn(move || log_reader.read(stderr))
    {
        spawn_backend_task(BackendOwnership {
            child,
            session,
            socket: socket.clone(),
            token: token.clone(),
        });
        return Err(format!("open_failed: {error}"));
    }

    let backend_pid = child.id();
    let ownership = BackendOwnership {
        child,
        session,
        socket: socket.clone(),
        token: token.clone(),
    };
    let mut ownership = ownership;
    match wait_ready(&mut ownership.child, &socket, &token) {
        Ready::Ready => {
            // The backend owns session cleanup after a normal exit. Keeping a
            // Rust `Child` here would retain its process handle unnecessarily.
            ownership.session.keep();
            drop(ownership);
            let bridge = Arc::new(NativeBridge {
                token,
                socket: socket.clone(),
                pid: backend_pid,
            });
            let value = json!({
                "socket": socket,
                "tokenFile": token_file,
                "backendPID": backend_pid,
                "frontendPID": unsafe { GetCurrentProcessId() },
            });
            Ok((bridge, value))
        }
        Ready::Exited(status) => {
            let _ = ownership.child.wait();
            if status == Some(23) {
                return Err(already_running(home));
            }
            let detail = startup_log.detail();
            if detail.is_empty() {
                Err("open_failed: Desktop backend exited during startup".into())
            } else {
                Err(format!(
                    "open_failed: Desktop backend exited during startup: {detail}"
                ))
            }
        }
        Ready::Timeout => {
            // The helper may have started restoring an OS proxy before the
            // socket became ready. Never SIGKILL it here; keep retrying the
            // graceful shutdown while the user sees the startup error.
            spawn_backend_task(ownership);
            Err("open_failed: Desktop backend did not become ready".into())
        }
    }
}

enum Ready {
    Ready,
    Exited(Option<i32>),
    Timeout,
}

fn wait_ready(child: &mut Child, socket: &Path, token: &str) -> Ready {
    let deadline = Instant::now() + STARTUP_TIMEOUT;
    while Instant::now() < deadline {
        match child.try_wait() {
            Ok(Some(status)) => return Ready::Exited(status.code()),
            Err(_) => return Ready::Exited(None),
            Ok(None) => {}
        }
        match rpc(
            socket,
            token,
            "host.ping",
            json!({}),
            Duration::from_secs(1),
        ) {
            Ok(reply) => {
                if reply["error"].is_null() && reply["result"]["ready"].as_bool() == Some(true) {
                    return Ready::Ready;
                }
            }
            Err(_) => {}
        }
        thread::sleep(Duration::from_millis(50));
    }
    Ready::Timeout
}

struct SessionDir {
    path: PathBuf,
    cleanup: bool,
}

impl SessionDir {
    fn new(path: PathBuf) -> Self {
        Self {
            path,
            cleanup: true,
        }
    }

    fn path(&self) -> &Path {
        &self.path
    }

    fn keep(&mut self) {
        self.cleanup = false;
    }
}

impl Drop for SessionDir {
    fn drop(&mut self) {
        if self.cleanup {
            remove_session(&self.path);
        }
    }
}

struct BackendOwnership {
    child: Child,
    session: SessionDir,
    socket: PathBuf,
    token: String,
}

/// Runs the graceful-shutdown loop off the UI thread.
///
/// The ownership slot survives a failed thread creation, so the caller can
/// still hand the backend to the fallback shutdown below instead of removing
/// its live authentication directory or orphaning it with no cleanup.
fn spawn_backend_task(ownership: BackendOwnership) -> bool {
    let slot = Arc::new(Mutex::new(Some(ownership)));
    let worker = slot.clone();
    let result = thread::Builder::new()
        .name("gopurr-backend-retire".into())
        .spawn(move || {
            let Some(mut ownership) = worker.lock().ok().and_then(|mut slot| slot.take()) else {
                return;
            };
            retire_backend(&mut ownership);
        });
    if result.is_ok() {
        return true;
    }
    if let Some(mut ownership) = slot.lock().ok().and_then(|mut slot| slot.take()) {
        retire_backend_with_deadline(&mut ownership, Duration::from_secs(3));
        if ownership.child.try_wait().ok().flatten().is_none() {
            // The backend's parent monitor retries its own graceful cleanup
            // after this frontend exits, including the session token.
            ownership.session.keep();
            return false;
        }
    }
    false
}

fn retire_backend(ownership: &mut BackendOwnership) {
    loop {
        match ownership.child.try_wait() {
            Ok(Some(_)) | Err(_) => return,
            Ok(None) => {}
        }
        let _ = rpc(
            &ownership.socket,
            &ownership.token,
            "host.shutdown",
            json!({}),
            Duration::from_secs(25),
        );
        thread::sleep(Duration::from_secs(2));
    }
}

fn retire_backend_with_deadline(ownership: &mut BackendOwnership, wait: Duration) {
    let deadline = Instant::now() + wait;
    while Instant::now() < deadline {
        match ownership.child.try_wait() {
            Ok(Some(_)) | Err(_) => return,
            Ok(None) => {}
        }
        let _ = rpc(
            &ownership.socket,
            &ownership.token,
            "host.shutdown",
            json!({}),
            Duration::from_secs(1),
        );
        thread::sleep(Duration::from_millis(250));
    }
}

enum HomeError {
    InUse,
    Other(String),
}

fn desktop_home_ready(home: &Path) -> Result<OwnedHandle, HomeError> {
    let deadline = Instant::now() + Duration::from_millis(1500);
    loop {
        match lock_home(home) {
            Ok(lock) => return Ok(lock),
            Err(HomeError::InUse) if Instant::now() < deadline => {
                thread::sleep(Duration::from_millis(50));
            }
            Err(error) => return Err(error),
        }
    }
}

fn lock_home(home: &Path) -> Result<OwnedHandle, HomeError> {
    if !home.is_absolute() {
        return Err(HomeError::Other(
            "An absolute desktop data directory is required".into(),
        ));
    }
    std::fs::create_dir_all(home).map_err(|e| HomeError::Other(e.to_string()))?;
    let lock = home.join("desktop.lock");
    let file = OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .open(&lock)
        .map_err(|e| HomeError::Other(e.to_string()))?;
    set_private_dacl(&lock, false).map_err(HomeError::Other)?;
    let mut overlapped = OVERLAPPED::default();
    unsafe {
        LockFileEx(
            HANDLE(file.as_raw_handle() as *mut _),
            LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY,
            None,
            1,
            0,
            &mut overlapped,
        )
    }
    .map_err(|error| {
        if error.code() == windows::core::HRESULT::from_win32(ERROR_LOCK_VIOLATION.0) {
            HomeError::InUse
        } else {
            HomeError::Other(error.to_string())
        }
    })?;
    Ok(OwnedHandle(HANDLE(file.into_raw_handle() as *mut _)))
}

fn already_running(home: &Path) -> String {
    let activate = canonical_home(home);
    if activate {
        "already_running_canonical: Gopurr is already running for this data directory".into()
    } else {
        "already_running: Gopurr is already running for this data directory".into()
    }
}

fn canonical_home(home: &Path) -> bool {
    let Some(config) = std::env::var_os("APPDATA").filter(|v| !v.is_empty()) else {
        return false;
    };
    same_directory(home, &PathBuf::from(config).join("Gopurr"))
}

fn same_directory(first: &Path, second: &Path) -> bool {
    match (file_identity(first), file_identity(second)) {
        (Some(a), Some(b)) => a == b,
        _ => match (std::fs::canonicalize(first), std::fs::canonicalize(second)) {
            (Ok(a), Ok(b)) => a
                .to_string_lossy()
                .eq_ignore_ascii_case(&b.to_string_lossy()),
            _ => false,
        },
    }
}

fn file_identity(path: &Path) -> Option<(u32, u64, u64)> {
    let handle = unsafe {
        CreateFileW(
            &HSTRING::from(path.as_os_str()),
            FILE_READ_ATTRIBUTES.0,
            FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
            None,
            OPEN_EXISTING,
            FILE_FLAG_BACKUP_SEMANTICS,
            None,
        )
    }
    .ok()?;
    let owned = OwnedHandle(handle);
    let mut info = BY_HANDLE_FILE_INFORMATION::default();
    unsafe {
        GetFileInformationByHandle(owned.0, &mut info).ok()?;
    }
    Some((
        info.dwVolumeSerialNumber,
        u64::from(info.nFileIndexHigh),
        u64::from(info.nFileIndexLow),
    ))
}

fn session_directory() -> Result<PathBuf, String> {
    let root = std::env::temp_dir();
    for _ in 0..64 {
        let suffix = random_hex(8)?;
        let path = root.join(format!("gp-nsh-{suffix}"));
        match std::fs::create_dir(&path) {
            Ok(()) => {
                if let Err(error) = set_private_dacl(&path, true) {
                    let _ = std::fs::remove_dir_all(&path);
                    return Err(error);
                }
                if path.join("rpc").to_string_lossy().len() > MAX_SOCKET_PATH {
                    let _ = std::fs::remove_dir_all(&path);
                    return Err("The native session path is too long".into());
                }
                return Ok(path);
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                std::fs::create_dir_all(&root).map_err(|e| e.to_string())?;
                continue;
            }
            Err(error) => return Err(error.to_string()),
        }
    }
    Err("Could not allocate a private native session".into())
}

fn remove_session(path: &Path) {
    let _ = std::fs::remove_dir_all(path);
}

fn write_private_file(path: &Path, body: &[u8]) -> Result<(), String> {
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|e| e.to_string())?;
    file.write_all(body).map_err(|e| e.to_string())?;
    file.sync_all().map_err(|e| e.to_string())?;
    set_private_dacl(path, false)?;
    Ok(())
}

fn random_token() -> Result<String, String> {
    let mut bytes = [0u8; 32];
    random_bytes(&mut bytes)?;
    Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

fn random_hex(count: usize) -> Result<String, String> {
    let mut bytes = vec![0u8; count];
    random_bytes(&mut bytes)?;
    Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

fn random_bytes(bytes: &mut [u8]) -> Result<(), String> {
    let status = unsafe { BCryptGenRandom(None, bytes, BCRYPT_USE_SYSTEM_PREFERRED_RNG) };
    if status.0 < 0 {
        return Err("Could not generate native session credentials".into());
    }
    Ok(())
}

fn path_owner_sid(path: &Path) -> Result<String, String> {
    let mut descriptor = PSECURITY_DESCRIPTOR::default();
    let status = unsafe {
        GetNamedSecurityInfoW(
            &HSTRING::from(path.as_os_str()),
            SE_FILE_OBJECT,
            OWNER_SECURITY_INFORMATION,
            None,
            None,
            None,
            None,
            &mut descriptor,
        )
    };
    if status != ERROR_SUCCESS {
        return Err(format!(
            "Could not read the native session owner: {}",
            status.0
        ));
    }
    let mut owner = PSID::default();
    let mut defaulted = windows::core::BOOL::default();
    unsafe {
        GetSecurityDescriptorOwner(descriptor, &mut owner, &mut defaulted)
            .map_err(|e| e.to_string())?;
    }
    let result = sid_string(owner);
    unsafe {
        let _ = LocalFree(Some(HLOCAL(descriptor.0)));
    }
    result
}

fn sid_string(sid: PSID) -> Result<String, String> {
    if sid.0.is_null() {
        return Err("The native session has no owner".into());
    }
    unsafe {
        let mut text = PWSTR::null();
        ConvertSidToStringSidW(sid, &mut text).map_err(|e| e.to_string())?;
        let mut wide = Vec::new();
        let mut at = text.0;
        loop {
            let value = std::ptr::read(at);
            if value == 0 {
                break;
            }
            wide.push(value);
            at = at.add(1);
        }
        let _ = LocalFree(Some(HLOCAL(text.0.cast())));
        String::from_utf16(&wide).map_err(|e| e.to_string())
    }
}

fn set_private_dacl(path: &Path, directory: bool) -> Result<(), String> {
    let owner = path_owner_sid(path)?;
    let inherit = if directory { "OICI" } else { "" };
    let sddl = format!("D:P(A;{inherit};FA;;;SY)(A;{inherit};FA;;;BA)(A;{inherit};FA;;;{owner})");
    let mut descriptor = windows::Win32::Security::PSECURITY_DESCRIPTOR::default();
    unsafe {
        ConvertStringSecurityDescriptorToSecurityDescriptorW(
            &HSTRING::from(sddl),
            SDDL_REVISION_1,
            &mut descriptor,
            None,
        )
        .map_err(|e| e.to_string())?;
    }
    let mut present = windows::core::BOOL::default();
    let mut dacl: *mut windows::Win32::Security::ACL = ptr::null_mut();
    let mut defaulted = windows::core::BOOL::default();
    unsafe {
        GetSecurityDescriptorDacl(descriptor, &mut present, &mut dacl, &mut defaulted)
            .map_err(|e| e.to_string())?;
    }
    let result = unsafe {
        SetNamedSecurityInfoW(
            &HSTRING::from(path.as_os_str()),
            SE_FILE_OBJECT,
            DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION,
            None,
            None,
            Some(dacl),
            None,
        )
    };
    unsafe {
        let _ = LocalFree(Some(HLOCAL(descriptor.0)));
    }
    if result != ERROR_SUCCESS {
        return Err(format!(
            "Could not protect the native session: {}",
            result.0
        ));
    }
    Ok(())
}

static WINSOCK: OnceLock<Result<(), String>> = OnceLock::new();

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_line_delimited_response() {
        assert_eq!(
            frame_response(b"{\"result\":{\"ready\":true}}\n").unwrap(),
            Some(b"{\"result\":{\"ready\":true}}".to_vec())
        );
        assert_eq!(frame_response(b"").unwrap(), None);
        assert!(frame_response(&[b'x'; MAX_RESPONSE + 1]).is_err());
    }

    #[test]
    fn identity_compares_directory_identity() {
        let first = std::env::temp_dir();
        assert!(same_directory(&first, &first));
    }
}

fn rpc(
    socket_path: &Path,
    token: &str,
    method: &str,
    params: Value,
    timeout: Duration,
) -> Result<Value, String> {
    ensure_winsock()?;
    let socket = wait_socket(socket_path).map_err(|error| format!("transport: {error}"))?;
    set_timeout(socket.0, SO_RCVTIMEO, timeout)?;
    set_timeout(socket.0, SO_SNDTIMEO, timeout)?;
    let request = serde_json::to_vec(&json!({
        "token": token,
        "method": method,
        "params": params,
    }))
    .map_err(|e| format!("transport: {e}"))?;
    socket_send(socket.0, &request)?;
    let response = socket_read(socket.0)?;
    let value = serde_json::from_slice(&response)
        .map_err(|e| format!("transport: invalid backend response: {e}"))?;
    Ok(value)
}

fn ensure_winsock() -> Result<(), String> {
    WINSOCK
        .get_or_init(|| unsafe {
            let mut data = WSADATA::default();
            let status = WSAStartup(0x0202, &mut data);
            if status != 0 {
                Err(format!("transport: WSAStartup failed ({status})"))
            } else {
                Ok(())
            }
        })
        .clone()
}

fn wait_socket(path: &Path) -> Result<Socket, String> {
    let bytes = path
        .to_str()
        .ok_or_else(|| "native socket path is not valid UTF-8".to_owned())?
        .as_bytes();
    if bytes.is_empty() || bytes.len() > MAX_SOCKET_PATH {
        return Err("native socket path is too long".into());
    }
    let mut address = SOCKADDR_UN {
        sun_family: windows::Win32::Networking::WinSock::ADDRESS_FAMILY(AF_UNIX),
        ..Default::default()
    };
    for (slot, byte) in address.sun_path.iter_mut().zip(bytes) {
        *slot = *byte as i8;
    }
    let socket = unsafe {
        WSASocketW(
            AF_UNIX as i32,
            SOCK_STREAM.0,
            0,
            None,
            0,
            1, // WSA_FLAG_OVERLAPPED
        )
    }
    .map_err(|error| error.to_string())?;
    let socket = Socket(socket);
    let length = (std::mem::size_of::<u16>() + bytes.len() + 1) as i32;
    let result = unsafe {
        connect(
            socket.0,
            &address as *const SOCKADDR_UN as *const SOCKADDR,
            length,
        )
    };
    if result != 0 {
        return Err(format!(
            "native socket connect failed ({})",
            unsafe { WSAGetLastError() }.0
        ));
    }
    Ok(socket)
}

fn set_timeout(socket: SOCKET, option: i32, timeout: Duration) -> Result<(), String> {
    let value = timeout.as_millis().min(u128::from(u32::MAX)) as u32;
    let result = unsafe { setsockopt(socket, SOL_SOCKET, option, Some(&value.to_ne_bytes())) };
    if result != 0 {
        return Err(format!(
            "transport: could not set the socket timeout ({})",
            unsafe { WSAGetLastError() }.0
        ));
    }
    Ok(())
}

fn socket_send(socket: SOCKET, mut body: &[u8]) -> Result<(), String> {
    while !body.is_empty() {
        let sent = unsafe { send(socket, body, SEND_RECV_FLAGS(0)) };
        if sent <= 0 {
            return Err(format!(
                "transport: send failed ({})",
                unsafe { WSAGetLastError() }.0
            ));
        }
        body = &body[sent as usize..];
    }
    Ok(())
}

fn socket_read(socket: SOCKET) -> Result<Vec<u8>, String> {
    let mut response = Vec::new();
    let mut buffer = [0u8; 16 << 10];
    loop {
        let read = unsafe { recv(socket, &mut buffer, SEND_RECV_FLAGS(0)) };
        if read < 0 {
            return Err(format!(
                "transport: receive failed ({})",
                unsafe { WSAGetLastError() }.0
            ));
        }
        if read == 0 {
            break;
        }
        response.extend_from_slice(&buffer[..read as usize]);
        if let Some(body) = frame_response(&response)? {
            return Ok(body);
        }
    }
    frame_response(&response)?.ok_or_else(|| "transport: empty native response".into())
}

/// Extracts one complete JSON line from a native response stream.
fn frame_response(bytes: &[u8]) -> Result<Option<Vec<u8>>, String> {
    if let Some(at) = bytes.iter().position(|byte| *byte == b'\n') {
        return Ok(Some(bytes[..at].to_vec()));
    }
    if bytes.len() > MAX_RESPONSE {
        return Err(format!(
            "transport: native response exceeds {MAX_RESPONSE} bytes"
        ));
    }
    Ok(None)
}
