import Foundation
import NativeShell
import UIKit

@main
enum GopurrNative {
    static func main() {
        if CommandLine.arguments.dropFirst().contains("--version") {
            print("Gopurr " + (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"))
            return
        }
        UIApplicationMain(CommandLine.argc, CommandLine.unsafeArgv, nil,
                          NSStringFromClass(AppDelegate.self))
    }
}

@MainActor
final class AppDelegate: UIResponder, UIApplicationDelegate {
    private(set) var engine: EngineBridge?
    private(set) var desktop: DesktopBridge?
    private(set) var home: URL?
    private var eventTimer: Timer?
    var onDesktopEvent: (([String: Any]) -> Void)?
    private var quitting = false
    weak var mainScene: UIWindowScene?
    private var windowVisible = true
    private var preferences: [String: Any] = [:]
    private var releaseTimer: Timer?
    private var appliedInitialPrefs = false
    private var hasUnsavedDrafts = false
    var lastPage = "overview"

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        let home: URL
        let args = CommandLine.arguments
        let homeIndex = args.firstIndex(of: "--gopurr-home")
        let argumentHome = homeIndex.flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil }
        if let override = argumentHome ?? ProcessInfo.processInfo.environment["GOPURR_NATIVE_HOME"] ?? ProcessInfo.processInfo.environment["GOPURR_HOME"],
           override.hasPrefix("/") {
            home = URL(fileURLWithPath: override, isDirectory: true)
        } else {
            home = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
                .appendingPathComponent(Bundle.main.object(forInfoDictionaryKey: "GopurrDataDirectory") as? String ?? "Gopurr Native", isDirectory: true)
        }
        #if targetEnvironment(macCatalyst)
        self.desktop = DesktopBridge(home: home)
        #else
        self.engine = EngineBridge(home: home)
        #endif
        self.home = home
        if let desktop {
            eventTimer = Timer.scheduledTimer(withTimeInterval: 0.4, repeats: true) { [weak self] _ in
                desktop.poll { result in
                    DispatchQueue.main.async {
                        guard let self else { return }
                        switch result {
                        case .success(let events): for event in events {
                            if event["kind"] as? String == "show" {
                                if let page = (event["value"] as? [String:Any])?["page"] as? String { self.lastPage = page }
                                self.showDesktop()
                            }
                            if event["kind"] as? String == "prefs", let prefs = event["value"] as? [String: Any] { self.applyPreferences(prefs) }
                            self.onDesktopEvent?(event)
                            if event["kind"] as? String == "quit" {
                                // The backend's updater owns relaunch after replacement.
                                exit(0)
                            }
                        }
                        case .failure(let error): self.onDesktopEvent?(["kind":"error", "value":error.localizedDescription])
                        }
                    }
                }
            }
        }
        return true
    }

    func applyPreferences(_ prefs: [String: Any]) {
        let releaseChanged = (preferences["lightMode"] as? Bool) != (prefs["lightMode"] as? Bool) || (preferences["lightDelaySeconds"] as? Int) != (prefs["lightDelaySeconds"] as? Int)
        preferences = prefs
        if releaseChanged { releaseTimer?.invalidate(); releaseTimer = nil }
        let source = prefs["appearance"] as? String ?? "system"
        mainScene?.windows.first?.overrideUserInterfaceStyle = source == "dark" ? .dark : source == "light" ? .light : .unspecified
        GopurrDesktopLayoutChrome()
        if !appliedInitialPrefs {
            appliedInitialPrefs = true
            if prefs["silentStart"] as? Bool == true { GopurrDesktopHideWindow(); setWindowVisible(false) }
        }
        applyDockPolicy()
        scheduleRelease()
    }

    func setWindowVisible(_ visible: Bool) {
        if visible, let window = mainScene?.windows.first, window.rootViewController == nil, let home {
            window.rootViewController = GopurrController(engine: engine, desktop: desktop, home: home)
        }
        windowVisible = visible
        desktop?.dispatch("host.visible", ["visible":visible]) { _ in }
        applyDockPolicy(); scheduleRelease()
    }

    func applicationDidBecomeActive(_ application: UIApplication) {
        if desktop != nil, !windowVisible { showDesktop() }
    }

    func requestWindowClose() {
        if preferences["closeToTrayAcknowledged"] as? Bool == true { closeDesktopWindow() }
        else { onDesktopEvent?(["kind":"closeRequested"]) }
    }

    func closeDesktopWindow() {
        GopurrDesktopHideWindow()
        setWindowVisible(false)
    }

    func setUnsavedDrafts(_ dirty: Bool) {
        hasUnsavedDrafts = dirty
        if dirty { releaseTimer?.invalidate(); releaseTimer = nil }
        else { scheduleRelease() }
    }

    private func applyDockPolicy() {
        let policy = preferences["dockVisibility"] as? String ?? "show"
        GopurrDesktopSetDock(policy == "hide" || (policy == "follow-window" && !windowVisible) ? 0 : 1)
    }

    private func scheduleRelease() {
        guard !windowVisible, !hasUnsavedDrafts, preferences["lightMode"] as? Bool == true else {
            releaseTimer?.invalidate(); releaseTimer = nil; return
        }
        guard releaseTimer == nil else { return }
        let seconds = max(1, (preferences["lightDelaySeconds"] as? NSNumber)?.doubleValue ?? 60)
        releaseTimer = Timer.scheduledTimer(withTimeInterval: seconds, repeats: false) { [weak self] _ in
            DispatchQueue.main.async {
                guard let self, !self.windowVisible, !self.hasUnsavedDrafts else { return }
                self.mainScene?.windows.first?.rootViewController = nil
                self.onDesktopEvent = nil
                self.releaseTimer = nil
            }
        }
    }

    private func showDesktop() {
        releaseTimer?.invalidate(); releaseTimer = nil
        if let window = mainScene?.windows.first, let home {
            if window.rootViewController == nil { window.rootViewController = GopurrController(engine: engine, desktop: desktop, home: home) }
            setWindowVisible(true); GopurrDesktopShowWindow()
        } else {
            UIApplication.shared.requestSceneSessionActivation(nil, userActivity: nil, options: nil)
        }
    }

    func restoreWindowFocus() { showDesktop() }

    func application(_ application: UIApplication,
                     configurationForConnecting connectingSceneSession: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let configuration = UISceneConfiguration(name: "Gopurr", sessionRole: connectingSceneSession.role)
        configuration.sceneClass = UIWindowScene.self
        configuration.delegateClass = SceneDelegate.self
        return configuration
    }

    override func buildMenu(with builder: UIMenuBuilder) {
        super.buildMenu(with: builder)
        guard desktop != nil, builder.system == .main else { return }
        let quit = UIKeyCommand(title: NSLocalizedString("Quit Gopurr", comment: ""), action: #selector(requestQuit), input: "q", modifierFlags: .command)
        builder.replaceChildren(ofMenu: .quit) { _ in [quit] }
    }

    @objc func requestQuit() {
        guard let desktop, !quitting else { return }
        quitting = true
        desktop.dispatch("host.shutdown", [:]) { [weak self] result in
            DispatchQueue.main.async {
                guard let self else { return }
                switch result {
                case .success(let value):
                    if let error = (value as? [String: Any])?["error"] as? [String: Any] {
                        self.quitting = false
                        self.onDesktopEvent?(["kind":"error", "value":error["message"] ?? "Could not restore system proxy"])
                    } else { exit(0) }
                case .failure(let error):
                    self.quitting = false
                    self.onDesktopEvent?(["kind":"error", "value":error.localizedDescription])
                }
            }
        }
    }

    func applicationWillTerminate(_ application: UIApplication) {
        engine?.close()
        desktop?.close()
    }
}

@MainActor
@objc(GopurrSceneDelegate)
final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession,
               options connectionOptions: UIScene.ConnectionOptions) {
        guard let scene = scene as? UIWindowScene,
              let application = UIApplication.shared.delegate as? AppDelegate,
              let home = application.home else { return }
        #if targetEnvironment(macCatalyst)
        scene.sizeRestrictions?.minimumSize = CGSize(width: 840, height: 560)
        scene.sizeRestrictions?.maximumSize = CGSize(width: 7680, height: 4320)
        scene.titlebar?.titleVisibility = .hidden
        #endif
        let window = UIWindow(windowScene: scene)
        application.mainScene = scene
        window.rootViewController = GopurrController(engine: application.engine, desktop: application.desktop, home: home)
        window.makeKeyAndVisible()
        self.window = window
        #if targetEnvironment(macCatalyst)
        if #available(macCatalyst 16.0, *), !UserDefaults.standard.bool(forKey: "desktopWindowSizeInitialized") {
            DispatchQueue.main.async {
                scene.requestGeometryUpdate(.Mac(systemFrame: CGRect(x: 80, y: 80, width: 1060, height: 720))) { _ in
                    UserDefaults.standard.set(false, forKey: "desktopWindowSizeInitialized")
                }
                UserDefaults.standard.set(true, forKey: "desktopWindowSizeInitialized")
            }
        }
        #endif
        for context in connectionOptions.urlContexts {
            application.desktop?.dispatch("host.importURL", ["url":context.url.absoluteString]) { _ in }
        }
    }
    func sceneWillEnterForeground(_ scene: UIScene) {
        (UIApplication.shared.delegate as? AppDelegate)?.setWindowVisible(true)
    }
    func sceneDidEnterBackground(_ scene: UIScene) {
        (UIApplication.shared.delegate as? AppDelegate)?.setWindowVisible(false)
    }
    func scene(_ scene: UIScene, openURLContexts URLContexts: Set<UIOpenURLContext>) {
        for context in URLContexts {
            (UIApplication.shared.delegate as? AppDelegate)?.desktop?.dispatch("host.importURL", ["url":context.url.absoluteString]) { _ in }
        }
    }

}

@MainActor
final class GopurrController: UIViewController {
    private let engine: EngineBridge?
    private let desktop: DesktopBridge?
    private let home: URL
    private let app = NshAppView()

    init(engine: EngineBridge?, desktop: DesktopBridge?, home: URL) {
        self.engine = engine
        self.desktop = desktop
        self.home = home
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        app.translatesAutoresizingMaskIntoConstraints = false
        app.storageDirectory = home.appendingPathComponent("ui", isDirectory: true)
        app.params = [
            "nativeHost": true,
            "desktop": desktop != nil,
            "nativeTitlebarInset": desktop != nil ? GopurrDesktopTitlebarInset() : 0,
            "engine": ["transport": "native", "vpnAvailable": false],
        ]
        if desktop != nil { app.params["page"] = (UIApplication.shared.delegate as? AppDelegate)?.lastPage ?? "overview" }
        if let page = ProcessInfo.processInfo.environment["GOPURR_NATIVE_PAGE"],
           ["overview", "profiles", "subs", "proxies", "conns", "mesh", "rules", "logs", "settings"].contains(page) {
            app.params["page"] = page
        }
        app.limits = NshLimits(memoryMB: 128, callBudget: 2, maxNodes: 50_000)
        app.onError = { [weak self] message in self?.showFailure(message) }
        var channels = ["engine.dispatch"]
        if let desktop {
            channels = ["desktop.dispatch", "desktop.file.open", "desktop.file.save", "desktop.clipboard.write", "desktop.openURL", "desktop.reveal", "desktop.window.close", "desktop.window.dirty"]
            for channel in channels {
                app.handle(channel) { request, reply in
                    if channel == "desktop.window.dirty" {
                        guard let dirty = (request as? [String:Any])?["dirty"] as? Bool else { reply.reject("A draft state is required"); return }
                        (UIApplication.shared.delegate as? AppDelegate)?.setUnsavedDrafts(dirty)
                        reply.resolve(["ok":true]); return
                    }
                    if channel == "desktop.window.close" {
                        (UIApplication.shared.delegate as? AppDelegate)?.closeDesktopWindow()
                        reply.resolve(["ok":true]); return
                    }
                    var method = "host." + channel.dropFirst("desktop.".count)
                    var params = request
                    if channel == "desktop.dispatch", let object = request as? [String: Any], let business = object["method"] as? String {
                        method = business; params = object["params"]
                    }
                    desktop.dispatch(method, params) { result in
                        switch result {
                        case .success(let value):
                            if channel == "desktop.file.open" || channel == "desktop.file.save" {
                                DispatchQueue.main.async {
                                    (UIApplication.shared.delegate as? AppDelegate)?.restoreWindowFocus()
                                }
                            }
                            if channel == "desktop.dispatch", method == "desktop.snapshot", let snapshot = (value as? [String: Any])?["result"] as? [String: Any] {
                                DispatchQueue.main.async {
                                    let delegate = UIApplication.shared.delegate as? AppDelegate
                                    if let page = snapshot["page"] as? String { delegate?.lastPage = page }
                                    if let prefs = snapshot["prefs"] as? [String: Any] { delegate?.applyPreferences(prefs) }
                                }
                            }
                            if channel == "desktop.dispatch" { reply.resolve(value) }
                            else if let envelope = value as? [String: Any], let failure = envelope["error"] as? [String: Any] { reply.reject(failure["message"] as? String ?? "Desktop operation failed") }
                            else { reply.resolve((value as? [String: Any])?["result"]) }
                        case .failure(let error): reply.reject(error.localizedDescription)
                        }
                    }
                }
            }
            (UIApplication.shared.delegate as? AppDelegate)?.onDesktopEvent = { [weak self] event in
                self?.app.send("desktop.event", event)
            }
        } else if let engine {
          app.handle("engine.dispatch") { request, reply in
            engine.dispatch(request) { result in
                switch result {
                case .success(let value): reply.resolve(value)
                case .failure(let error): reply.reject(error.localizedDescription)
                }
            }
          }
        }
        view.addSubview(app)
        NSLayoutConstraint.activate([
            app.topAnchor.constraint(equalTo: view.topAnchor),
            app.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            app.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            app.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
        do {
            // Native work is only reachable through the explicit engine handler.
            let capabilities = try JSONSerialization.data(withJSONObject: ["host":channels, "statusBar":true])
            try app.grantCapabilities(String(decoding: capabilities, as: UTF8.self))
            guard let bundle = Bundle.main.url(forResource: "bundle", withExtension: "nshc", subdirectory: "nsh") else {
                throw BridgeError("The Gopurr interface bundle is missing")
            }
            try app.loadBundle(at: bundle)
        } catch {
            showFailure(error.localizedDescription)
        }
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        guard desktop != nil else { return }
        installWindowHooks()
    }

    private func installWindowHooks(attempt: Int = 0) {
        if attempt == 0, ProcessInfo.processInfo.environment["GOPURR_NATIVE_GEOMETRY_LOG"] == "1" {
            NSLog("Gopurr desktop geometry: idiom=%ld view=%@ scale=%.2f", UIDevice.current.userInterfaceIdiom.rawValue, String(describing: view.bounds), view.traitCollection.displayScale)
        }
        let installed = GopurrDesktopInstallWindowHooks { visible in
            let delegate = UIApplication.shared.delegate as? AppDelegate
            if visible < 0 { delegate?.requestQuit() }
            else if visible == 2 { delegate?.requestWindowClose() }
            else if visible == 3 { delegate?.onDesktopEvent?(["kind":"chrome", "value":["left":GopurrDesktopTitlebarInset(), "height":38]]) }
            else { delegate?.setWindowVisible(visible != 0) }
        }
        if installed == 0, attempt < 40 {
            DispatchQueue.main.asyncAfter(deadline: .now()+0.05) { [weak self] in self?.installWindowHooks(attempt: attempt+1) }
            return
        }
        (UIApplication.shared.delegate as? AppDelegate)?.onDesktopEvent?(["kind":"chrome", "value":["left":GopurrDesktopTitlebarInset(), "height":38]])
    }

    override var preferredStatusBarStyle: UIStatusBarStyle { NshAppView.preferredStatusBarStyle }
    override var prefersStatusBarHidden: Bool { NshAppView.prefersStatusBarHidden }
    override var preferredStatusBarUpdateAnimation: UIStatusBarAnimation {
        NshAppView.preferredStatusBarUpdateAnimation
    }

    private func showFailure(_ message: String) {
        NSLog("Gopurr Native: %@", message)
        let label = UILabel()
        label.text = "Gopurr\n\n" + message
        label.numberOfLines = 0
        label.textAlignment = .center
        label.textColor = .label
        label.backgroundColor = .systemBackground
        label.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(label)
        NSLayoutConstraint.activate([
            label.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            label.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),
            label.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor),
        ])
    }
}
