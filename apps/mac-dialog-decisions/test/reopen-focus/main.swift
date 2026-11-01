import AppKit
import Foundation

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let previous = NSWorkspace.shared.frontmostApplication!
let selfPID = ProcessInfo.processInfo.processIdentifier
let root = CommandLine.arguments[1]
let host = DialogHost(directory: root)
app.delegate = host
func require(_ value: @autoclosure () -> Bool, _ message: String) {
  if !value() { fputs("FAIL: \(message)\n", stderr); _ = previous.activate(); exit(1) }
}
func wait(_ condition: () -> Bool) -> Bool {
  let deadline = Date().addingTimeInterval(5)
  while !condition() && Date() < deadline {
    if let event = app.nextEvent(matching: .any, until: Date().addingTimeInterval(0.01), inMode: .default, dequeue: true) {
      app.sendEvent(event)
    }
  }
  return condition()
}
func front() -> pid_t { NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0 }
func writeSpec(_ name: String) throws -> String {
  let path = root + "/" + name + ".json"
  try JSONSerialization.data(withJSONObject: ["window_title": name, "title": name, "options": [], "allow_text": true, "multi_select": false]).write(to: URL(fileURLWithPath:path))
  return path
}
func restorePrevious() {
  NSApp.deactivate()
  _ = previous.activate()
  require(wait { front() == previous.processIdentifier && !NSApp.isActive }, "restore previous app")
}
func clickAndType(_ panel: KeyPanel, _ controller: DialogController) {
  let text = controller.textView!
  let point = text.convert(NSPoint(x: 8, y: 8), to: nil)
  let down = NSEvent.mouseEvent(with: .leftMouseDown, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: panel.windowNumber, context: nil, eventNumber: 1, clickCount: 1, pressure: 1)!
  let up = NSEvent.mouseEvent(with: .leftMouseUp, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: panel.windowNumber, context: nil, eventNumber: 2, clickCount: 1, pressure: 0)!
  NSApp.postEvent(up, atStart: false)
  panel.sendEvent(down)
  let selected = wait { front() == selfPID && panel.isKeyWindow && panel.firstResponder === text }
  if !selected { fputs("click state front=\(front()) self=\(selfPID) key=\(panel.isKeyWindow) responder=\(String(describing: panel.firstResponder)) hit=\(String(describing: panel.contentView?.hitTest(point))) point=\(point)\n", stderr) }
  require(selected, "actual click selects text input")
  let key = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: panel.windowNumber, context: nil, characters: "a", charactersIgnoringModifiers: "a", isARepeat: false, keyCode: 0)!
  panel.sendEvent(key)
  require(wait { text.string == "a" }, "key event reaches clicked text input")
}
DispatchQueue.main.async {
do {
  let first = try writeSpec("first"), second = try writeSpec("second")
  require(previous.processIdentifier != selfPID, "another app must initially be frontmost")
  for scenario in ["external", "selected"] {
    restorePrevious()
    host.open(first)
    let original = host.windows["first.json"]!.panel!
    if scenario == "selected" {
      app.activate(ignoringOtherApps: true); original.makeKeyAndOrderFront(nil)
      require(wait { front() == selfPID && original.isKeyWindow }, "original panel selected")
    }
    host.closeAll()
    let closed = Date()
    let before = front()
    require(scenario == "selected" ? before == selfPID : before == previous.processIdentifier, "expected frontmost app before reopen")
    host.open(first)
    let panel = host.windows["first.json"]!.panel! as! KeyPanel
    require(Date().timeIntervalSince(closed) < 1, "reopen occurs within one second")
    require(front() == before && !panel.isKeyWindow, "reopen preserves frontmost PID and does not take key")
    print("REOPEN_FOCUS scenario=\(scenario) before=\(before) after=\(front()) key=\(panel.isKeyWindow)")
    host.open(second)
    let newer = host.windows["second.json"]!.panel!
    let numbers = NSWindow.windowNumbers(options: [.allSpaces]) ?? []
    let ordered = numbers.map { $0.intValue }
    require(ordered.firstIndex(of: panel.windowNumber) != nil && ordered.firstIndex(of: newer.windowNumber) != nil, "both windows appear in window order")
    require(ordered.firstIndex(of: panel.windowNumber)! < ordered.firstIndex(of: newer.windowNumber)!, "new panel is behind existing panel")
    restorePrevious()
    clickAndType(panel, host.windows["first.json"]!)
    print("REOPEN_CLICK_ORDER_PASS scenario=\(scenario) first=\(panel.windowNumber) behind=\(newer.windowNumber)")
    host.closeAll()
  }
  restorePrevious()
  print("REOPEN_FOCUS_PASS")
  exit(0)
} catch { fputs("FAIL: \(error)\n", stderr); _ = previous.activate(); exit(1) }
}
app.run()
