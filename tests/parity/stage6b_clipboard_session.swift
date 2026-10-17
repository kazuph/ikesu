// Run only on the explicitly authorized mini GUI pasteboard.
import AppKit
import CryptoKit
import Foundation

let args = Array(CommandLine.arguments.dropFirst())
let out = URL(fileURLWithPath: args[0], isDirectory: true)
let board = NSPasteboard.general
let manager = FileManager.default
try manager.createDirectory(at: out, withIntermediateDirectories: true)
typealias Snapshot = [[String: Data]]
func snapshot() throws -> Snapshot {
    try (board.pasteboardItems ?? []).map { item in
        var result: [String: Data] = [:]
        for type in item.types {
            guard let data = item.data(forType: type) else {
                throw NSError(domain: "clipboard type unavailable", code: 1)
            }
            result[type.rawValue] = data
        }
        return result
    }
}
func encode(_ value: Snapshot) throws -> Data {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    return try encoder.encode(value)
}
func digest(_ value: Snapshot) throws -> String {
    SHA256.hash(data: try encode(value)).map { String(format: "%02x", $0) }.joined()
}
func command(_ executable: String, _ arguments: [String], input: Data? = nil) throws -> (Int32, Data) {
    let child = Process()
    child.executableURL = URL(fileURLWithPath: executable)
    child.arguments = arguments
    let output = Pipe()
    child.standardOutput = output
    let stream = Pipe()
    if input != nil { child.standardInput = stream }
    try child.run()
    if let data = input {
        stream.fileHandleForWriting.write(data)
        try stream.fileHandleForWriting.close()
    }
    let data = output.fileHandleForReading.readDataToEndOfFile()
    child.waitUntilExit()
    return (child.terminationStatus, data)
}
let original = try snapshot()
let saved = out.appendingPathComponent("original-pasteboard.json")
try encode(original).write(to: saved, options: .atomic)
try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: saved.path)
let decoded = try JSONDecoder().decode(Snapshot.self, from: Data(contentsOf: saved))
guard decoded == original else { fatalError("clipboard backup verification failed") }
var proof: [String: Any] = ["backup_verified": true, "before_sha256": try digest(original),
    "saved_items": original.count, "saved_types": original.reduce(0) { $0 + $1.count }]
var failed = false
// Every operation after backup, including failed child commands, reaches restoration.
do {
    let payload = try Data(contentsOf: URL(fileURLWithPath: args[1]))
    let write = try command("/usr/bin/pbcopy", [], input: payload)
    let read = try command("/usr/bin/pbpaste", [])
    guard write.0 == 0 && read.0 == 0 && read.1 == payload && board.string(forType: .string)?.data(using: .utf8) == payload else {
        throw NSError(domain: "SSH pbcopy/pbpaste cannot reach GUI NSPasteboard", code: 2)
    }
    proof["ssh_gui_roundtrip"] = true
    let run = try command(args[2], Array(args.dropFirst(3)))
    FileHandle.standardOutput.write(run.1)
    proof["observation_exit"] = run.0
    if run.0 != 0 { failed = true }
    proof["payload_unchanged_after_observation"] = board.string(forType: .string)?.data(using: .utf8) == payload
    if proof["payload_unchanged_after_observation"] as? Bool != true { failed = true }
} catch {
    proof["error"] = String(describing: error)
    failed = true
}
board.clearContents()
let restoredItems = decoded.map { values -> NSPasteboardItem in
    let item = NSPasteboardItem()
    for (type, data) in values { item.setData(data, forType: NSPasteboard.PasteboardType(type)) }
    return item
}
let restored = restoredItems.isEmpty || board.writeObjects(restoredItems)
let after = try snapshot()
proof["restore_write_succeeded"] = restored
proof["after_sha256"] = try digest(after)
proof["restored_exactly"] = restored && after == original
if !restored || after != original { failed = true }
let evidence = try JSONSerialization.data(withJSONObject: proof, options: [.prettyPrinted, .sortedKeys])
try evidence.write(to: out.appendingPathComponent("proof.json"), options: .atomic)
// Original bytes stay private on mini; publish only verification and hashes.
FileHandle.standardOutput.write(evidence)
exit(failed ? 1 : 0)
