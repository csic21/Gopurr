import Foundation

/// The desktop helper retains the original model, kernel and OS integration.
/// Only JSON crosses the private socket; the helper never creates an old UI.
final class DesktopBridge: @unchecked Sendable {
    private let queue = DispatchQueue(label: "io.github.gopurr.native.desktop", qos: .userInitiated)
    private let eventsQueue = DispatchQueue(label: "io.github.gopurr.native.events", qos: .userInitiated)
    private let lock = NSLock()
    private let home: URL
    private var handle: UInt64 = 0
    private var closed = false
    private var lastEvent: UInt64 = 0 // eventsQueue only
    private var pollInFlight = false // lock

    private func session() -> (UInt64, Bool) {
        lock.lock(); defer { lock.unlock() }
        return (handle, closed)
    }

    init(home: URL) { self.home = home }

    func dispatch(_ method: String, _ params: Any?, completion: @escaping (Result<Any, Error>) -> Void) {
        queue.async { [self] in
            do {
                guard !session().1 else { throw BridgeError("The desktop service is closed") }
                if session().0 == 0 { try open() }
                let current = session()
                guard !current.1 else { throw BridgeError("The desktop service is closed") }
                completion(.success(try call(current.0, method, params)))
            } catch { completion(.failure(error)) }
        }
    }

    func poll(completion: @escaping (Result<[[String: Any]], Error>) -> Void) {
        lock.lock()
        guard !closed, handle != 0, !pollInFlight else { lock.unlock(); return }
        let currentHandle = handle
        pollInFlight = true
        lock.unlock()
        eventsQueue.async { [self] in
            defer { lock.lock(); pollInFlight = false; lock.unlock() }
            do {
                let reply = try call(currentHandle, "host.events", ["after": lastEvent])
                if let error = reply["error"] as? [String: Any] { throw BridgeError(error["message"] as? String ?? "Desktop events unavailable") }
                let events = reply["result"] as? [[String: Any]] ?? []
                for event in events { if let id = event["id"] as? NSNumber { lastEvent = max(lastEvent, id.uint64Value) } }
                completion(.success(events))
            } catch { if !session().1 { completion(.failure(error)) } }
        }
    }

    func close() {
        lock.lock()
        if closed { lock.unlock(); return }
        closed = true
        let currentHandle = handle
        handle = 0
        lock.unlock()
        // Go cancels outstanding reads first. Do not wait on a serial queue
        // that may still be downloading a subscription while the app quits.
        if currentHandle != 0 { GopurrDesktopClose(currentHandle) }
    }

    private func open() throws {
        guard let helper = Bundle.main.url(forResource: "gopurr-desktop", withExtension: nil),
              let frontend = Bundle.main.executableURL else { throw BridgeError("The desktop helper is missing") }
        let options: [String: Any] = ["home": home.path, "executable": helper.path, "frontend": frontend.path,
            "productionHome": Bundle.main.object(forInfoDictionaryKey: "GopurrDataDirectory") as? String == "Gopurr"]
        let json = String(decoding: try JSONSerialization.data(withJSONObject: options), as: UTF8.self)
        let result = try json.withCString { try decode(GopurrDesktopOpen(UnsafeMutablePointer(mutating: $0))) }
        if let failure = result["error"] as? [String: Any] { throw BridgeError(failure["message"] as? String ?? "Desktop startup failed") }
        guard let id = result["handle"] as? NSNumber, id.uint64Value != 0 else { throw BridgeError("Invalid desktop handle") }
        lock.lock()
        let wasClosed = closed
        if !wasClosed { handle = id.uint64Value }
        lock.unlock()
        if wasClosed { GopurrDesktopClose(id.uint64Value); throw BridgeError("The desktop service is closed") }
    }

    private func call(_ currentHandle: UInt64, _ method: String, _ params: Any?) throws -> [String: Any] {
        let json = String(decoding: try JSONSerialization.data(withJSONObject: ["method": method, "params": params ?? [:]]), as: UTF8.self)
        return try json.withCString { try decode(GopurrDesktopCall(currentHandle, UnsafeMutablePointer(mutating: $0))) }
    }

    private func decode(_ pointer: UnsafeMutablePointer<CChar>?) throws -> [String: Any] {
        guard let pointer else { throw BridgeError("Empty desktop response") }
        defer { GopurrFree(pointer) }
        guard let value = try JSONSerialization.jsonObject(with: Data(String(cString: pointer).utf8)) as? [String: Any] else {
            throw BridgeError("Invalid desktop response")
        }
        return value
    }
}
