fn main() {
    println!("cargo:rerun-if-env-changed=GOPURR_VERSION");
    println!("cargo:rerun-if-env-changed=GOPURR_WINDOWS_ICON");
    let version = std::env::var("GOPURR_VERSION").unwrap_or_else(|_| "0.6.26".into());
    println!("cargo:rustc-env=GOPURR_VERSION={version}");
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") {
        let mut resource = winresource::WindowsResource::new();
        resource
            .set("ProductName", "Gopurr")
            .set("FileDescription", "Gopurr NSH Desktop")
            .set("OriginalFilename", "Gopurr.exe")
            .set("ProductVersion", &version)
            .set("FileVersion", &version);
        resource.set_manifest(r#"<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0"><assemblyIdentity name="io.github.gopurr.app" version="1.0.0.0" type="win32"/><trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo><compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1"><application><supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/></application></compatibility><application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings><dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness><longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware></windowsSettings></application></assembly>"#);
        if let Ok(icon) = std::env::var("GOPURR_WINDOWS_ICON") {
            resource.set_icon(&icon);
        }
        resource.compile().expect("Windows executable resources");
    }
}
