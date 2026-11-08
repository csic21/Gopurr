import Foundation

/// Owns one Go runtime on a serial worker. The UI thread never waits for core I/O.
final class EngineBridge: @unchecked Sendable {
    private let queue = DispatchQueue(label: "io.github.gopurr.native.engine", qos: .userInitiated)
    private let home: URL
    private var handle: UInt64 = 0
    private var closed = false

    init(home: URL) {
        self.home = home
    }

    func dispatch(_ request: Any?, completion: @escaping (Result<Any, Error>) -> Void) {
        queue.async { [self] in
            do {
                guard !closed else { throw BridgeError("The native engine is closed") }
                if handle == 0 { try open() }
                guard let request, JSONSerialization.isValidJSONObject(request) else {
                    throw BridgeError("An engine request must be a JSON object")
                }
                let data = try JSONSerialization.data(withJSONObject: request)
                let json = String(decoding: data, as: UTF8.self)
                let response = try json.withCString {
                    try decode(GopurrCall(handle, UnsafeMutablePointer(mutating: $0)), checkError: false)
                }
                // Keep the stable Go result/error envelope for every host.
                completion(.success(response))
            } catch {
                completion(.failure(error))
            }
        }
    }

    /// Called at process termination, after the system stopped UI work.
    func close() {
        queue.sync {
            closed = true
            if handle != 0 {
                GopurrClose(handle)
                handle = 0
            }
        }
    }

    private func open() throws {
        let options = try JSONSerialization.data(withJSONObject: ["home": home.path])
        let response = try String(decoding: options, as: UTF8.self).withCString {
            try decode(GopurrOpen(UnsafeMutablePointer(mutating: $0)))
        }
        guard let number = response["handle"] as? NSNumber, number.uint64Value != 0 else {
            throw BridgeError("The engine did not return a valid handle")
        }
        handle = number.uint64Value
    }

    private func decode(_ pointer: UnsafeMutablePointer<CChar>?, checkError: Bool = true) throws -> [String: Any] {
        guard let pointer else { throw BridgeError("The engine returned an empty response") }
        defer { GopurrFree(pointer) }
        let data = Data(String(cString: pointer).utf8)
        guard let result = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw BridgeError("The engine returned an invalid response")
        }
        if checkError, let failure = result["error"] as? [String: Any] {
            throw BridgeError(failure["message"] as? String ?? "The engine request failed")
        }
        return result
    }
}

struct BridgeError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
