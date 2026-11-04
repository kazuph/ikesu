import AppKit
import Foundation

// Native check for where a new decision panel is ordered:
// (a) an uncovered open panel keeps the new panel behind it,
// (b) when every open panel is covered by another app's window the new panel
//     comes to the front, and (c) neither case takes the keyboard.
// `cover x y w h` is the other app: a separate accessory process that owns a
// plain window. No sound, no real decision, no Slack.

let args = CommandLine.arguments
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
signal(SIGTERM) { _ in exit(0) }

func fail(_ message: String) -> Never { FileHandle.standardError.write(Data("FAIL: \(message)\n".utf8)); exit(1) }

if args.count == 6 && args[1] == "cover" {
  let frame = NSRect(x: Double(args[2])!, y: Double(args[3])!, width: Double(args[4])!, height: Double(args[5])!)
  DispatchQueue.main.async {
    let window = NSWindow(contentRect: frame, styleMask: [.titled], backing: .buffered, defer: false)
    window.title = "panel-front cover"
    window.isReleasedWhenClosed = false
    window.setFrame(frame, display: true)
    window.orderFrontRegardless()
    print("COVER_READY \(window.windowNumber)")
    fflush(stdout)
  }
  DispatchQueue.global().async {
    while readLine() != nil {}
    DispatchQueue.main.async { exit(0) }
  }
  app.run()
}

if args.count == 2 && args[1] == "order" {
  let rows = (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? [])
    .filter { ($0[kCGWindowLayer as String] as? Int) == 0 }
    .map { ["number": $0[kCGWindowNumber as String] as? Int ?? 0, "pid": $0[kCGWindowOwnerPID as String] as? Int ?? 0] }
  print(String(decoding: try! JSONSerialization.data(withJSONObject: rows), as: UTF8.self))
  exit(0)
}

guard args.count == 2 else { fail("usage: panel-front <directory> | cover x y w h") }
let root = args[1]
let host = DialogHost(directory: URL(fileURLWithPath: root).standardizedFileURL.path)
app.delegate = host
let selfPID = Int(ProcessInfo.processInfo.processIdentifier)
let frontBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier

func screenOrder() -> [[String: Any]] {
  (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? [])
    .filter { ($0[kCGWindowLayer as String] as? Int) == 0 }
}
func index(_ number: Int, _ order: [[String: Any]]) -> Int {
  order.firstIndex { ($0[kCGWindowNumber as String] as? Int) == number } ?? -1
}
func waitFor(_ predicate: () -> Bool) -> Bool {
  let end = Date().addingTimeInterval(5)
  while Date() < end {
    if predicate() { return true }
    RunLoop.main.run(until: Date().addingTimeInterval(0.02))
  }
  return predicate()
}
func writeSpec(_ name: String) -> String {
  let path = root + "/\(name).json"
  let spec: [String: Any] = ["window_title": "panel-front \(name)", "title": "前面の試験 \(name)", "options": [["id": "yes", "label": "はい"]], "allow_text": false, "multi_select": false]
  try! JSONSerialization.data(withJSONObject: spec).write(to: URL(fileURLWithPath: path))
  return path
}
func checkKeyboard(_ label: String) {
  let front = NSWorkspace.shared.frontmostApplication?.processIdentifier
  let keyPanels = host.windows.values.filter { $0.panel.isKeyWindow }.count
  print("KEYBOARD \(label) frontBefore=\(frontBefore ?? 0) front=\(front ?? 0) app_active=\(NSApp.isActive) key_panels=\(keyPanels)")
  // Concurrent tests may change the frontmost app; only this process counts.
  if front.map(Int.init) == selfPID || NSApp.isActive || keyPanels != 0 { fail("\(label): a new panel took the keyboard") }
}

var cover: Process?
func stopCover() {
  guard let process = cover else { return }
  (process.standardInput as? Pipe)?.fileHandleForWriting.closeFile()
  process.waitUntilExit()
  cover = nil
}

func runTest() {
  if frontBefore.map(Int.init) == selfPID { fail("another app must be frontmost before the test") }
  // (a0) Two decisions arrive together: the second opens before the window
  // server lists the first, and still sits behind it.
  host.open(writeSpec("burst1"))
  host.open(writeSpec("burst2"))
  let burst1 = host.windows["burst1.json"]!.panel!, burst2 = host.windows["burst2.json"]!.panel!
  let burstOK = waitFor({
    let order = screenOrder()
    let one = index(burst1.windowNumber, order), two = index(burst2.windowNumber, order)
    return one == 0 && two == 1
  })
  let orderBurst = screenOrder()
  print("PANEL_FRONT_BURST first=\(index(burst1.windowNumber, orderBurst)) second=\(index(burst2.windowNumber, orderBurst))")
  if !burstOK { host.closeAll(); fail("(a0) burst: second panel was not directly behind the first") }
  checkKeyboard("a0")
  host.closeAll()
  if !waitFor({ index(burst1.windowNumber, screenOrder()) < 0 && index(burst2.windowNumber, screenOrder()) < 0 }) { fail("burst panels did not close") }
  // (a) First panel alone comes to the front; the second sits behind it.
  host.open(writeSpec("first"))
  let first = host.windows["first.json"]!.panel!
  if !waitFor({ index(first.windowNumber, screenOrder()) == 0 }) { fail("first panel was not the front window") }
  host.open(writeSpec("second"))
  let second = host.windows["second.json"]!.panel!
  if !waitFor({ index(second.windowNumber, screenOrder()) > index(first.windowNumber, screenOrder()) }) { fail("(a) uncovered panel: new panel was not behind it") }
  let orderA = screenOrder()
  let firstIndex = index(first.windowNumber, orderA), secondIndex = index(second.windowNumber, orderA)
  print("PANEL_FRONT_A first=\(firstIndex) second=\(secondIndex)")
  if secondIndex != firstIndex + 1 { fail("(a) new panel must sit directly behind the uncovered panel") }
  checkKeyboard("a")

  // (b) Another app's window covers both panels; the next panel comes forward.
  let union = first.frame.union(second.frame).insetBy(dx: -20, dy: -20)
  let process = Process()
  process.executableURL = URL(fileURLWithPath: args[0])
  process.arguments = ["cover", "\(union.minX)", "\(union.minY)", "\(union.width)", "\(union.height)"]
  let input = Pipe(), output = Pipe()
  process.standardInput = input
  process.standardOutput = output
  try! process.run()
  cover = process
  var line = ""
  while !line.contains("\n") {
    let data = output.fileHandleForReading.availableData
    if data.isEmpty { fail("cover process ended") }
    line += String(decoding: data, as: UTF8.self)
  }
  guard let coverNumber = Int(line.split(separator: " ").last!.trimmingCharacters(in: .whitespacesAndNewlines)) else { fail("cover window number") }
  if !waitFor({
    let order = screenOrder()
    let c = index(coverNumber, order)
    return c >= 0 && c < index(first.windowNumber, order) && c < index(second.windowNumber, order)
  }) { stopCover(); fail("cover window did not cover the open panels") }
  host.open(writeSpec("third"))
  let third = host.windows["third.json"]!.panel!
  let ok = waitFor({
    let order = screenOrder()
    let t = index(third.windowNumber, order)
    return t >= 0 && t < index(coverNumber, order)
  })
  let orderB = screenOrder()
  print("PANEL_FRONT_B cover=\(index(coverNumber, orderB)) first=\(index(first.windowNumber, orderB)) second=\(index(second.windowNumber, orderB)) third=\(index(third.windowNumber, orderB))")
  if !ok { stopCover(); host.closeAll(); fail("(b) covered panels: new panel was not brought in front of the other app") }
  checkKeyboard("b")
  stopCover()
  host.closeAll()
  print("PANEL_FRONT_PASS behindWhenFront=1 frontWhenCovered=1 keyboardKept=1")
  exit(0)
}
DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) { runTest() }
app.run()
