use libloading::Library;
use nsh_engine::{HostChannel, Policy, parse_capabilities};
use nsh_platform_windows::{App, AppOptions, AppView, Appearance, AssetBase, Bundle};
use serde_json::{Value, json};
use std::{
    cell::{Cell, RefCell},
    ffi::{CStr, CString, c_char},
    path::{Path, PathBuf},
    rc::Rc,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    thread,
    time::{Duration, Instant},
};
use windows::{
    Win32::{
        Foundation::{HWND, LPARAM, LRESULT, RECT, WPARAM},
        System::{
            Ole::{OleInitialize, OleUninitialize},
            Threading::{OpenProcess, PROCESS_SYNCHRONIZE, WaitForSingleObject},
        },
        UI::{
            HiDpi::{
                AdjustWindowRectExForDpi, DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2,
                GetDpiForWindow, SetProcessDpiAwarenessContext,
            },
            Shell::{
                DefSubclassProc, RemoveWindowSubclass, SetCurrentProcessExplicitAppUserModelID,
                SetWindowSubclass,
            },
            WindowsAndMessaging::*,
        },
    },
    core::{HSTRING, PCWSTR},
};

// Enabled only for a disposable acceptance session. Never record RPC payloads
// or credentials: lifecycle, page names and engine failures are sufficient.
fn trace(message: &str) {
    use std::io::Write;
    if let Ok(path) = std::env::var("GOPURR_NATIVE_SESSION_OUTPUT") {
        let path = PathBuf::from(path).with_extension("host.log");
        if let Ok(mut file) = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)
        {
            let _ = writeln!(
                file,
                "pid={} {:?} {message}",
                std::process::id(),
                std::time::SystemTime::now()
            );
        }
    }
}
struct DiagnosticLogger;
impl log::Log for DiagnosticLogger {
    fn enabled(&self, metadata: &log::Metadata<'_>) -> bool {
        metadata.level() <= log::Level::Warn
    }
    fn log(&self, record: &log::Record<'_>) {
        if self.enabled(record.metadata()) {
            trace(&format!(
                "{} {}: {}",
                record.level(),
                record.target(),
                record.args()
            ));
        }
    }
    fn flush(&self) {}
}
static LOGGER: DiagnosticLogger = DiagnosticLogger;

const EVENT: u32 = WM_APP + 0x47;
const TIMER: usize = 0x4750;
const CHANNELS: &[&str] = &[
    "desktop.dispatch",
    "desktop.file.open",
    "desktop.file.save",
    "desktop.clipboard.write",
    "desktop.openURL",
    "desktop.reveal",
    "desktop.window.close",
    "desktop.window.dirty",
];
type Open = unsafe extern "C" fn(*const c_char) -> *mut c_char;
type Call = unsafe extern "C" fn(u64, *const c_char) -> *mut c_char;
type Free = unsafe extern "C" fn(*mut c_char);

struct Bridge {
    call: Call,
    free: Free,
    handle: u64,
    pid: u32,
}
impl Bridge {
    unsafe fn open(root: &Path, home: &Path) -> Result<(Arc<Self>, Value), String> {
        // The Go runtime owns live threads until process exit. Keep its DLL
        // mapped for the entire process; unloading it is never safe.
        let library = Box::leak(Box::new(
            unsafe { Library::new(root.join("GopurrBridge.dll")) }.map_err(|e| e.to_string())?,
        ));
        let open: Open =
            *unsafe { library.get(b"GopurrDesktopOpen\0") }.map_err(|e| e.to_string())?;
        let call: Call =
            *unsafe { library.get(b"GopurrDesktopCall\0") }.map_err(|e| e.to_string())?;
        let free: Free = *unsafe { library.get(b"GopurrFree\0") }.map_err(|e| e.to_string())?;
        let options = CString::new(json!({"home":home,"executable":root.join("gopurr-desktop.exe"),"frontend":root.join("Gopurr.exe"),"productionHome":true}).to_string()).unwrap();
        let value = unsafe { take_json(open(options.as_ptr()), free) }?;
        if let Some(error) = value.get("error") {
            let code = if error["code"] == "already_running" && error["activate"] == true {
                "already_running_canonical"
            } else {
                error["code"].as_str().unwrap_or("open_failed")
            };
            return Err(code.to_owned()
                + ": "
                + error["message"]
                    .as_str()
                    .unwrap_or("Desktop backend unavailable"));
        }
        let handle = value["handle"]
            .as_u64()
            .ok_or("Missing desktop bridge handle")?;
        let pid = value["backendPID"]
            .as_u64()
            .and_then(|pid| u32::try_from(pid).ok())
            .ok_or("Missing desktop backend PID")?;
        Ok((
            Arc::new(Self {
                call,
                free,
                handle,
                pid,
            }),
            value,
        ))
    }
    fn request(&self, method: &str, params: Value) -> Result<Value, String> {
        let request = CString::new(json!({"method":method,"params":params}).to_string())
            .map_err(|e| e.to_string())?;
        unsafe { take_json((self.call)(self.handle, request.as_ptr()), self.free) }
    }
    fn result(&self, method: &str, params: Value) -> Result<Value, String> {
        let reply = self.request(method, params)?;
        if let Some(error) = reply.get("error") {
            return Err(error["message"]
                .as_str()
                .unwrap_or("Desktop operation failed")
                .into());
        }
        Ok(reply["result"].clone())
    }
    fn visible(self: &Arc<Self>, visible: bool) {
        let bridge = self.clone();
        thread::spawn(move || {
            let _ = bridge.result("host.visible", json!({"visible":visible}));
        });
    }
}
unsafe fn take_json(ptr: *mut c_char, free: Free) -> Result<Value, String> {
    if ptr.is_null() {
        return Err("Empty desktop bridge response".into());
    }
    let result = serde_json::from_slice(unsafe { CStr::from_ptr(ptr) }.to_bytes())
        .map_err(|e| e.to_string());
    unsafe { free(ptr) };
    result
}

struct Window {
    view: Rc<AppView>,
    channel: HostChannel,
    bridge: Arc<Bridge>,
    bundle: Bundle,
    prefs: RefCell<Value>,
    page: RefCell<String>,
    visible: Cell<bool>,
    loaded: Cell<bool>,
    dirty: Cell<bool>,
    hidden_at: Cell<Instant>,
    running: Arc<AtomicBool>,
}
impl Window {
    fn hide(&self) {
        unsafe {
            let _ = ShowWindow(self.view.hwnd(), SW_HIDE);
        }
        self.visible.set(false);
        self.hidden_at.set(Instant::now());
        self.bridge.visible(false);
    }
    fn show(&self) {
        if !self.loaded.get() {
            params(&self.channel, &self.page.borrow());
            self.view.load(App::Js(self.bundle.clone()));
            self.loaded.set(true);
        }
        self.visible.set(true);
        self.bridge.visible(true);
        unsafe {
            let _ = ShowWindow(self.view.hwnd(), SW_RESTORE);
            let _ = SetForegroundWindow(self.view.hwnd());
        }
    }
    fn event(&self, event: Value) {
        if event["kind"] != "snapshot" {
            trace(&format!(
                "event {} page={}",
                event["kind"], event["value"]["page"]
            ));
        }
        match event["kind"].as_str().unwrap_or("") {
            "quit" => {
                self.running.store(false, Ordering::Release);
                // Only the backend initiates process exit, after restoring the
                // proxy/core state. It waits for us before restarting updates.
                unsafe {
                    let _ = AllowSetForegroundWindow(self.bridge.pid);
                    trace("destroying window");
                    let _ = DestroyWindow(self.view.hwnd());
                    trace("window destroyed");
                }
                return;
            }
            "show" => {
                if let Some(page) = event["value"]["page"].as_str() {
                    *self.page.borrow_mut() = page.into();
                }
                self.show();
            }
            "prefs" => *self.prefs.borrow_mut() = event["value"].clone(),
            "snapshot" => {
                *self.prefs.borrow_mut() = event["value"]["prefs"].clone();
                if let Some(page) = event["value"]["page"].as_str() {
                    *self.page.borrow_mut() = page.into();
                }
                return;
            }
            "focus" => {
                self.show();
                return;
            }
            "hide" => {
                self.hide();
                return;
            }
            "dirty" => {
                self.dirty.set(event["value"].as_bool().unwrap_or(true));
                return;
            }
            _ => (),
        }
        let _ = self.channel.send("desktop.event", event);
    }
}
fn params(channel: &HostChannel, page: &str) {
    channel.set_params(json!({"desktop":true,"nativeHost":true,"platform":"windows","nativeTitlebarInset":0,"nativeTitlebarHeight":38,"page":page}).as_object().unwrap().clone());
}
fn post(hwnd: usize, value: Value) {
    let ptr = Box::into_raw(Box::new(value));
    if unsafe {
        PostMessageW(
            Some(HWND(hwnd as _)),
            EVENT,
            WPARAM(0),
            LPARAM(ptr as isize),
        )
    }
    .is_err()
    {
        unsafe {
            drop(Box::from_raw(ptr));
        }
    }
}
unsafe extern "system" fn subclass(
    hwnd: HWND,
    message: u32,
    w: WPARAM,
    l: LPARAM,
    _: usize,
    data: usize,
) -> LRESULT {
    let state = unsafe { &*(data as *const Window) };
    match message {
        EVENT => {
            let event = unsafe { Box::from_raw(l.0 as *mut Value) };
            state.event(*event);
            return LRESULT(0);
        }
        WM_CLOSE => {
            if state.prefs.borrow()["closeToTrayAcknowledged"].as_bool() == Some(true) {
                state.hide();
            } else {
                let _ = state
                    .channel
                    .send("desktop.event", json!({"kind":"closeRequested"}));
            }
            return LRESULT(0);
        }
        WM_GETMINMAXINFO => {
            let dpi = unsafe { GetDpiForWindow(hwnd) }.max(96);
            let mut rect = RECT {
                left: 0,
                top: 0,
                right: 840 * dpi as i32 / 96,
                bottom: 560 * dpi as i32 / 96,
            };
            unsafe {
                let _ = AdjustWindowRectExForDpi(
                    &mut rect,
                    WINDOW_STYLE(GetWindowLongW(hwnd, GWL_STYLE) as u32),
                    false,
                    WINDOW_EX_STYLE(GetWindowLongW(hwnd, GWL_EXSTYLE) as u32),
                    dpi,
                );
            }
            let info = unsafe { &mut *(l.0 as *mut MINMAXINFO) };
            info.ptMinTrackSize.x = rect.right - rect.left;
            info.ptMinTrackSize.y = rect.bottom - rect.top;
            return LRESULT(0);
        }
        WM_TIMER if w.0 == TIMER => {
            if !state.visible.get()
                && state.loaded.get()
                && !state.dirty.get()
                && state.prefs.borrow()["lightMode"].as_bool() == Some(true)
                && state.hidden_at.get().elapsed().as_secs()
                    >= state.prefs.borrow()["lightDelaySeconds"]
                        .as_u64()
                        .unwrap_or(60)
                        .max(1)
            {
                trace("light-mode view release begin");
                state.view.stop();
                state.loaded.set(false);
                trace("light-mode view release complete");
            }
            return LRESULT(0);
        }
        WM_NCDESTROY => unsafe {
            let _ = RemoveWindowSubclass(hwnd, Some(subclass), 1);
        },
        _ => (),
    }
    unsafe { DefSubclassProc(hwnd, message, w, l) }
}
fn argument(args: &[String], name: &str) -> Option<String> {
    args.iter()
        .position(|arg| arg == name)
        .and_then(|i| args.get(i + 1))
        .cloned()
}
fn wait_for_process(pid: u32) -> Result<(), String> {
    unsafe {
        match OpenProcess(PROCESS_SYNCHRONIZE, false, pid) {
            Ok(handle) => {
                let result = WaitForSingleObject(handle, 30000);
                let _ = windows::Win32::Foundation::CloseHandle(handle);
                if result.0 != 0 {
                    return Err("Previous desktop backend did not exit".into());
                }
            }
            Err(error) if error.code().0 as u32 == 0x80070057 => (), // already exited
            Err(error) => return Err(error.to_string()),
        }
    }
    Ok(())
}

pub fn run() -> Result<(), String> {
    thread::Builder::new()
        .name("gopurr-nsh-ui".into())
        .stack_size(8 << 20)
        .spawn(run_on_ui_thread)
        .map_err(|error| error.to_string())?
        .join()
        .map_err(|_| "The native desktop UI thread failed".to_owned())?
}

fn run_on_ui_thread() -> Result<(), String> {
    if std::env::var_os("GOPURR_NATIVE_SESSION_OUTPUT").is_some() {
        let _ = log::set_logger(&LOGGER);
        log::set_max_level(log::LevelFilter::Warn);
    }
    trace("starting frontend");
    let args: Vec<String> = std::env::args().collect();
    // Update relaunches carry the explicit data directory, while login
    // launches use --autostart. This also covers the first restart from a
    // version that predates the one-shot relaunch marker.
    let explicit_relaunch = args.iter().any(|arg| arg == "--gopurr-home")
        && !args.iter().any(|arg| arg == "--autostart");
    if args.len() == 2 && args[1] == "--version" {
        println!("Gopurr {}", env!("GOPURR_VERSION"));
        return Ok(());
    }
    if let Some(pid) = argument(&args, "--wait-for-process") {
        wait_for_process(pid.parse().map_err(|_| "Invalid previous process")?)?;
    }
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let root = exe.parent().ok_or("Missing installation directory")?;
    // Old shortcuts, registered service commands and core child commands all
    // continue to enter through Gopurr.exe. Forward only actual helper modes.
    if args.get(1).is_some_and(|arg| {
        arg == "--core"
            || arg.starts_with("--service-")
            || arg.starts_with("--sysproxy-")
            || arg == "--windows-service"
    }) {
        use std::os::windows::process::CommandExt;
        let status = std::process::Command::new(root.join("gopurr-desktop.exe"))
            .args(&args[1..])
            .creation_flags(0x08000000)
            .status()
            .map_err(|e| e.to_string())?;
        std::process::exit(status.code().unwrap_or(1));
    }
    if args.iter().any(|arg| arg == "--native-quit") {
        use std::os::windows::process::CommandExt;
        let status = std::process::Command::new(root.join("gopurr-desktop.exe"))
            .args(["--native-activate", "--native-quit"])
            .env_remove("GOPURR_HOME")
            .creation_flags(0x08000000)
            .status()
            .map_err(|e| e.to_string())?;
        if !status.success() {
            return Err("Could not request desktop shutdown".into());
        }
        return Ok(());
    }
    let home = argument(&args, "--gopurr-home")
        .or_else(|| std::env::var("GOPURR_NATIVE_HOME").ok())
        .or_else(|| std::env::var("GOPURR_HOME").ok())
        .map(PathBuf::from)
        .unwrap_or_else(|| {
            PathBuf::from(std::env::var_os("APPDATA").unwrap_or_default()).join("Gopurr")
        });
    if !home.is_absolute() {
        return Err("An absolute desktop data directory is required".into());
    }
    let (bridge, session) = match unsafe { Bridge::open(root, &home) } {
        Ok(value) => value,
        Err(error) if error.starts_with("already_running_canonical:") => {
            use std::os::windows::process::CommandExt;
            // The existing mygo singleton routes activation and protocol URLs
            // to the running backend. It never constructs a legacy window.
            let _ = std::process::Command::new(root.join("gopurr-desktop.exe"))
                .arg("--native-activate")
                .args(&args[1..])
                .env_remove("GOPURR_HOME")
                .creation_flags(0x08000000)
                .status();
            return Ok(());
        }
        Err(error) => return Err(error),
    };
    if let Ok(path) = std::env::var("GOPURR_NATIVE_SESSION_OUTPUT") {
        std::fs::write(path, session.to_string()).map_err(|e| e.to_string())?;
    }
    let page = argument(&args, "--gopurr-page").unwrap_or_else(|| "overview".into());
    trace("backend ready");
    let snapshot = bridge.result("desktop.snapshot", json!({"page":page}))?;
    let prefs = snapshot["prefs"].clone();
    let show_window_on_launch = snapshot["startup"]["showWindow"].as_bool().unwrap_or(false);
    let channel = HostChannel::new();
    params(&channel, &page);
    unsafe {
        let _ = SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        OleInitialize(None).map_err(|e| e.to_string())?;
        SetCurrentProcessExplicitAppUserModelID(&HSTRING::from("io.github.gopurr.app"))
            .map_err(|e| e.to_string())?;
    }
    let bundle_path = root.join("nsh/bundle.nshc");
    let mut bundle = unsafe { Bundle::read_trusted(&bundle_path) }.map_err(|e| e.to_string())?;
    let granted = parse_capabilities(&json!({"host":CHANNELS}).to_string())?;
    let declared = bundle.policy.declared.clone();
    bundle = bundle.with_policy(Policy {
        declared,
        granted: Some(granted),
        ..Default::default()
    });
    let width = argument(&args, "--gopurr-width")
        .and_then(|s| s.parse().ok())
        .unwrap_or(1060)
        .max(840);
    let height = argument(&args, "--gopurr-height")
        .and_then(|s| s.parse().ok())
        .unwrap_or(720)
        .max(560);
    let view = AppView::new(
        None,
        AppOptions {
            app_id: "io.github.gopurr.app".into(),
            title: "Gopurr".into(),
            size: (width, height),
            host: channel.clone(),
            asset_base: Some(AssetBase::Directory(root.join("nsh/assets"))),
            data_dir: Some(home.join("nsh")),
            appearance: match prefs["appearance"].as_str() {
                Some("dark") => Appearance::Dark,
                Some("light") => Appearance::Light,
                _ => Appearance::System,
            },
            ..Default::default()
        },
    )
    .map_err(|e| e.to_string())?;
    let hwnd = view.hwnd().0 as usize;
    for &name in CHANNELS {
        let bridge = bridge.clone();
        channel.handle(name, move |call| {
            let data = call.data().clone();
            let bridge = bridge.clone();
            if name == "desktop.window.close" {
                post(hwnd, json!({"kind":"hide"}));
                call.resolve(json!({"ok":true}));
                return;
            }
            if name == "desktop.window.dirty" {
                if let Some(dirty) = data["dirty"].as_bool() {
                    post(hwnd, json!({"kind":"dirty","value":dirty}));
                    call.resolve(json!({"ok":true}));
                } else {
                    call.reject("A draft state is required");
                }
                return;
            }
            if matches!(
                name,
                "desktop.file.open" | "desktop.file.save" | "desktop.openURL" | "desktop.reveal"
            ) {
                unsafe {
                    let _ = AllowSetForegroundWindow(bridge.pid);
                }
            }
            thread::spawn(move || {
                let result = if name == "desktop.dispatch" {
                    let method = data["method"].as_str().unwrap_or("");
                    bridge
                        .request(method, data["params"].clone())
                        .inspect(|reply| {
                            if method == "desktop.snapshot" && reply.get("error").is_none() {
                                post(hwnd, json!({"kind":"snapshot","value":reply["result"]}));
                            }
                        })
                } else {
                    bridge.result(&name.replacen("desktop.", "host.", 1), data)
                };
                if matches!(name, "desktop.file.open" | "desktop.file.save") {
                    post(hwnd, json!({"kind":"focus"}));
                }
                call.reply(result);
            });
        });
    }
    let running = Arc::new(AtomicBool::new(true));
    let state = Box::new(Window {
        view: view.clone(),
        channel: channel.clone(),
        bridge: bridge.clone(),
        bundle: bundle.clone(),
        prefs: RefCell::new(prefs),
        page: RefCell::new(page),
        visible: Cell::new(true),
        loaded: Cell::new(true),
        dirty: Cell::new(false),
        hidden_at: Cell::new(Instant::now()),
        running: running.clone(),
    });
    unsafe {
        if !SetWindowSubclass(
            view.hwnd(),
            Some(subclass),
            1,
            (&*state as *const Window) as usize,
        )
        .as_bool()
        {
            return Err("Cannot install desktop window lifecycle".into());
        }
        SetTimer(Some(view.hwnd()), TIMER, 500, None);
        let icon = LoadImageW(
            None,
            PCWSTR(HSTRING::from(root.join("Gopurr.ico").to_string_lossy().as_ref()).as_ptr()),
            IMAGE_ICON,
            0,
            0,
            LR_LOADFROMFILE | LR_DEFAULTSIZE,
        );
        if let Ok(icon) = icon {
            SendMessageW(
                view.hwnd(),
                WM_SETICON,
                Some(WPARAM(ICON_BIG as usize)),
                Some(LPARAM(icon.0 as isize)),
            );
        }
    }
    view.load(App::Js(bundle));
    if state.prefs.borrow()["silentStart"].as_bool() == Some(true)
        && !show_window_on_launch
        && !explicit_relaunch
    {
        state.hide();
    } else {
        view.show();
        bridge.visible(true);
    }
    let polling = bridge.clone();
    let alive = running.clone();
    thread::spawn(move || {
        let mut after = 0;
        let mut failures = 0;
        while alive.load(Ordering::Acquire) {
            match polling.result("host.events", json!({"after":after})) {
                Ok(events) => {
                    failures = 0;
                    for event in events.as_array().into_iter().flatten() {
                        after = after.max(event["id"].as_u64().unwrap_or(0));
                        trace(&format!(
                            "poll event {} kind={}",
                            event["id"], event["kind"]
                        ));
                        post(hwnd, event.clone());
                    }
                }
                Err(error) => {
                    trace(&format!("event poll failed: {error}"));
                    failures += 1;
                    post(hwnd, json!({"kind":"error","value":error}));
                    if failures >= 3 {
                        post(hwnd, json!({"kind":"quit"}));
                        break;
                    }
                }
            }
            thread::sleep(Duration::from_millis(400));
        }
    });
    for url in args
        .iter()
        .skip(1)
        .filter(|arg| arg.starts_with("gopurr://") || arg.starts_with("clash://"))
    {
        let _ = bridge.result("host.importURL", json!({"url":url}));
    }
    unsafe {
        let mut message = MSG::default();
        while GetMessageW(&mut message, None, 0, 0).0 > 0 {
            let _ = TranslateMessage(&message);
            DispatchMessageW(&message);
        }
    }
    trace("message loop exited");
    running.store(false, Ordering::Release);
    view.stop();
    trace("view stopped");
    drop(state);
    drop(view);
    unsafe {
        OleUninitialize();
    }
    Ok(())
}
