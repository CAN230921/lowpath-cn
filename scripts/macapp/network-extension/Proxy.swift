import CryptoKit
import Darwin
import Foundation
import Network
import NetworkExtension

private typealias Endpoint = Network.NWEndpoint

private struct Configuration: Decodable {
    let port: UInt16
    let secret: String
    let processes: [String]?
    let pids: [Int32]?
    let exclude: [String]
    let parent: Int32
    let uid: UInt32
}

private struct Message: Codable {
    let kind: String
    var token: String? = nil
    var flow: UInt64? = nil
    var pid: Int32? = nil
    var local: String? = nil
    var remote: String? = nil
    var data: Data? = nil
}

private enum ProxyError: Error {
    case configuration, bridge, flow
}

private final class Flow {
    let socket: NEAppProxyUDPFlow
    let pid: Int32
    var direct: [Endpoint: NWConnection] = [:]

    init(_ socket: NEAppProxyUDPFlow, pid: Int32) {
        self.socket = socket
        self.pid = pid
    }

    func close(_ error: Error?) {
        direct.values.forEach { $0.cancel() }
        direct.removeAll()
        socket.closeReadWithError(error)
        socket.closeWriteWithError(error)
    }
}

final class TransparentProxy: NETransparentProxyProvider, NEAppProxyUDPFlowHandling {
    private let queue = DispatchQueue(label: "lowpath.network-proxy")
    private var config: Configuration?
    private var names: Set<String> = []
    private var pids: Set<Int32> = []
    private var exclude: Set<String> = []
    private var bridge: NWConnection?
    private var startCompletion: ((Error?) -> Void)?
    private var incoming = Data()
    private var pendingBytes = 0
    private var active = false
    private var serial: UInt64 = 0
    private var flows: [UInt64: Flow] = [:]

    override func startProxy(options: [String: Any]? = nil, completionHandler: @escaping (Error?) -> Void) {
        guard let text = options?["bridge"] as? String,
              let config = try? JSONDecoder().decode(Configuration.self, from: Data(text.utf8)),
              config.secret.utf8.count == 64, config.parent > 1,
              let port = Endpoint.Port(rawValue: config.port), port.rawValue != 0 else {
            completionHandler(ProxyError.configuration)
            return
        }
        queue.async { [self] in
            self.config = config
            self.names = Set((config.processes ?? []).map { $0.lowercased() })
            self.pids = Set(config.pids ?? [])
            self.exclude = Set(config.exclude)
            self.startCompletion = completionHandler
            let bridge = NWConnection(host: .ipv4(.loopback), port: port, using: .tcp)
            self.bridge = bridge
            bridge.stateUpdateHandler = { [weak self] state in
                guard let self, self.bridge != nil else { return }
                switch state {
                case .ready:
                    self.send(Message(kind: "hello", token: self.proof("provider", config.secret)))
                    self.receive()
                case .failed, .cancelled:
                    self.end(ProxyError.bridge)
                default:
                    break
                }
            }
            bridge.start(queue: self.queue)
            self.queue.asyncAfter(deadline: .now() + 8) { [weak self] in
                guard let self, self.startCompletion != nil else { return }
                self.end(ProxyError.bridge)
            }
        }
    }

    private func installRules() {
        let ipv4 = Endpoint.hostPort(host: .ipv4(.any), port: .any)
        let settings = NETransparentProxyNetworkSettings(tunnelRemoteAddress: "127.0.0.1")
        settings.includedNetworkRules = [NENetworkRule(remoteNetworkEndpoint: ipv4, remotePrefix: 0,
            localNetworkEndpoint: nil, localPrefix: 0, protocol: .UDP, direction: .outbound)]
        settings.excludedNetworkRules = []
        setTunnelNetworkSettings(settings) { [weak self] error in
            guard let self else { return }
            self.queue.async {
                guard let completion = self.startCompletion else { return }
                if let error {
                    self.end(error)
                    return
                }
                self.startCompletion = nil
                self.active = true
                self.send(Message(kind: "ready"))
                completion(nil)
            }
        }
    }

    override func stopProxy(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        queue.async {
            self.end(nil)
            completionHandler()
        }
    }

    private func end(_ error: Error?) {
        active = false
        let previous = bridge
        bridge = nil
        previous?.cancel()
        flows.values.forEach { $0.close(error) }
        flows.removeAll()
        incoming.removeAll()
        if let completing = startCompletion {
            startCompletion = nil
            completing(error ?? ProxyError.bridge)
        } else if let error, previous != nil {
            cancelProxyWithError(error)
        }
    }

    override func handleNewFlow(_ flow: NEAppProxyFlow) -> Bool { false }

    func handleNewUDPFlow(_ flow: NEAppProxyUDPFlow, initialRemoteFlowEndpoint endpoint: Network.NWEndpoint) -> Bool {
        queue.sync {
            guard active, let config, flows.count < 1024, let address = ipv4Endpoint(endpoint),
                  shouldTunnel(address), let token = flow.metaData.sourceAppAuditToken,
                  token.count == MemoryLayout<audit_token_t>.size else { return false }
            let audit = token.withUnsafeBytes { $0.loadUnaligned(as: audit_token_t.self) }
            let pid = audit_token_to_pid(audit)
            guard pid > 1, pid != config.parent, pid != getpid(), audit_token_to_euid(audit) == config.uid,
                  pids.contains(pid) || names.contains(flow.metaData.sourceAppSigningIdentifier.lowercased()) ||
                    names.contains(processName(pid)) else { return false }
            serial += 1
            let id = serial
            flows[id] = Flow(flow, pid: pid)
            flow.open(withLocalFlowEndpoint: nil) { [weak self] error in
                guard let self else { return }
                self.queue.async {
                    if let error { self.close(id, error) }
                    else { self.readFlow(id) }
                }
            }
            return true
        }
    }

    private func readFlow(_ id: UInt64) {
        guard let flow = flows[id] else { return }
        flow.socket.readDatagrams { [weak self] datagrams, error in
            guard let self else { return }
            self.queue.async {
                guard let current = self.flows[id] else { return }
                guard let datagrams, !datagrams.isEmpty, error == nil else {
                    self.close(id, error)
                    return
                }
                for (payload, endpoint) in datagrams {
                    guard let remote = self.ipv4Endpoint(endpoint), self.shouldTunnel(remote),
                          let localEndpoint = current.socket.localFlowEndpoint,
                          let local = self.ipv4Endpoint(localEndpoint) else {
                        self.pass(payload, endpoint, id)
                        continue
                    }
                    self.send(Message(kind: "packet", flow: id, pid: current.pid,
                        local: local.description, remote: remote.description, data: payload))
                }
                self.readFlow(id)
            }
        }
    }

    private func pass(_ data: Data, _ endpoint: Endpoint, _ id: UInt64) {
        guard let flow = flows[id] else { return }
        var connection = flow.direct[endpoint]
        if connection == nil {
            guard flow.direct.count < 32 else { return }
            let direct = NWConnection(to: endpoint, using: .udp)
            flow.direct[endpoint] = direct
            direct.stateUpdateHandler = { [weak self, weak direct] state in
                guard let self, let direct else { return }
                switch state {
                case .ready:
                    self.readDirect(direct, endpoint, id)
                case .failed, .cancelled:
                    if self.flows[id]?.direct[endpoint] === direct {
                        self.flows[id]?.direct.removeValue(forKey: endpoint)
                    }
                    direct.stateUpdateHandler = nil
                    direct.cancel()
                default:
                    break
                }
            }
            direct.start(queue: queue)
            connection = direct
        }
        connection?.send(content: data, completion: .contentProcessed { _ in })
    }

    private func readDirect(_ connection: NWConnection, _ endpoint: Endpoint, _ id: UInt64) {
        connection.receiveMessage { [weak self] data, _, _, error in
            guard let self, error == nil, let flow = self.flows[id] else { return }
            if let data {
                flow.socket.writeDatagrams([(data, endpoint)]) { _ in }
            }
            self.readDirect(connection, endpoint, id)
        }
    }

    private func close(_ id: UInt64, _ error: Error?) {
        guard let flow = flows.removeValue(forKey: id) else { return }
        flow.close(error)
        send(Message(kind: "closed", flow: id))
    }

    private func receive() {
        bridge?.receive(minimumIncompleteLength: 1, maximumLength: 32_768) { [weak self] bytes, _, complete, error in
            guard let self, self.bridge != nil else { return }
            if complete || error != nil {
                self.end(ProxyError.bridge)
                return
            }
            if let bytes {
                self.incoming.append(bytes)
                while let newline = self.incoming.firstIndex(of: 10) {
                    let line = self.incoming.prefix(upTo: newline)
                    guard line.count <= 65_536 else { self.end(ProxyError.bridge); return }
                    if let message = try? JSONDecoder().decode(Message.self, from: line) {
                        self.handle(message)
                    }
                    self.incoming.removeSubrange(...newline)
                }
                if self.incoming.count > 65_536 {
                    self.end(ProxyError.bridge)
                    return
                }
            }
            self.receive()
        }
    }

    private func proof(_ role: String, _ secret: String) -> String {
        SHA256.hash(data: Data("\(role):\(secret)".utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private func handle(_ message: Message) {
        if message.kind == "accept", let config, startCompletion != nil, !active {
            guard message.token == proof("client", config.secret) else { end(ProxyError.bridge); return }
            installRules()
            return
        }
        guard active, let id = message.flow, let flow = flows[id],
              let remote = message.remote, let address = Address(remote) else { return }
        let data = message.data ?? Data()
        switch message.kind {
        case "reply":
            flow.socket.writeDatagrams([(data, address.endpoint)]) { _ in }
        case "direct":
            pass(data, address.endpoint, id)
        default:
            break
        }
    }

    private func send(_ message: Message) {
        guard let bridge, var bytes = try? JSONEncoder().encode(message) else { return }
        bytes.append(10)
        guard pendingBytes + bytes.count <= 1_048_576 else { end(ProxyError.bridge); return }
        pendingBytes += bytes.count
        let count = bytes.count
        bridge.send(content: bytes, completion: .contentProcessed { [weak self] error in
            guard let self else { return }
            self.pendingBytes -= count
            if error != nil, self.bridge != nil { self.end(ProxyError.bridge) }
        })
    }

    private func ipv4Endpoint(_ endpoint: Endpoint) -> Address? {
        guard case .hostPort(.ipv4(let ip), let port) = endpoint, port.rawValue != 0 else { return nil }
        return Address(ip, port)
    }

    private func shouldTunnel(_ address: Address) -> Bool {
        guard address.port.rawValue != 53, !exclude.contains(address.description),
              !exclude.contains("\(address.ip):0") else { return false }
        let octets = Array(address.ip.rawValue)
        let a = octets[0], b = octets[1]
        return a > 0 && a < 224 && a != 10 && a != 127 &&
            !(a == 100 && (64..<128).contains(b)) && !(a == 169 && b == 254) &&
            !(a == 172 && (16..<32).contains(b)) && !(a == 192 && b == 168)
    }

    private func processName(_ pid: Int32) -> String {
        var path = [CChar](repeating: 0, count: 4096)
        guard proc_pidpath(pid, &path, UInt32(path.count)) > 0 else { return "" }
        return URL(fileURLWithPath: String(cString: path)).lastPathComponent.lowercased()
    }
}

private struct Address: CustomStringConvertible {
    let ip: IPv4Address
    let port: Endpoint.Port
    var endpoint: Endpoint { .hostPort(host: .ipv4(ip), port: port) }
    var description: String { "\(ip):\(port.rawValue)" }

    init(_ ip: IPv4Address, _ port: Endpoint.Port) {
        self.ip = ip
        self.port = port
    }

    init?(_ value: String) {
        let parts = value.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2, let ip = IPv4Address(String(parts[0])),
              let number = UInt16(parts[1]), number > 0, let port = Endpoint.Port(rawValue: number) else { return nil }
        self.init(ip, port)
    }
}
