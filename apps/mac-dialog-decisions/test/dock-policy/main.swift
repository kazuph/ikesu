import AppKit
import Foundation

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root = CommandLine.arguments[1]
let host = DialogHost(directory: root)
app.delegate = host

func check(_ condition: @autoclosure () -> Bool, _ message: String) {
  if !condition() { fputs("FAIL: \(message)\n", stderr); exit(1) }
}
func waitUntil(_ timeout: TimeInterval, _ condition: () -> Bool) -> Bool {
  let deadline = Date().addingTimeInterval(timeout)
  while !condition() && Date() < deadline {
    RunLoop.current.run(mode: .default, before: min(deadline, Date().addingTimeInterval(0.01)))
  }
  return condition()
}
func spec(_ name: String) throws -> String {
  let path = root + "/" + name + ".json"
  let object: [String: Any] = ["window_title": name, "title": name, "options": [], "multi_select": false, "allow_text": false]
  try JSONSerialization.data(withJSONObject: object).write(to: URL(fileURLWithPath: path))
  return path
}

do {
  let first = try spec("first"), second = try spec("second")
  host.open(first)
  check(app.activationPolicy() == .regular, "opening sets regular immediately")
  check(app.dockTile.badgeLabel == "1", "opening updates badge")
  host.closeAll()
  let closedAt = Date()
  check(app.activationPolicy() == .regular, "closing retains regular immediately")
  check(app.dockTile.badgeLabel == nil, "closing clears badge immediately")
  check(waitUntil(2) { app.activationPolicy() == .accessory }, "empty host becomes accessory")
  check(Date().timeIntervalSince(closedAt) >= 0.95, "accessory must wait about one second")
  print("EMPTY_DELAY_PASS")
  host.open(first)
  host.closeAll()
  let reopenedClose = Date()
  check(app.activationPolicy() == .regular, "second close retains regular")
  host.open(second)
  check(app.activationPolicy() == .regular && app.dockTile.badgeLabel == "1", "reopen is regular with badge")
  let end = reopenedClose.addingTimeInterval(1.2)
  var remainedRegular = true
  _ = waitUntil(2) {
    if app.activationPolicy() != .regular { remainedRegular = false; return true }
    return Date() >= end
  }
  check(remainedRegular && Date() >= end, "reopen cancels pending accessory throughout original deadline")
  host.closeAll()
  check(app.dockTile.badgeLabel == nil, "final close clears badge")
  check(waitUntil(2) { app.activationPolicy() == .accessory }, "final close eventually becomes accessory")
  print("REOPEN_CANCEL_PASS")
  host.open(first)
  host.closeAll()
  let firstClose = Date()
  check(waitUntil(1) { Date().timeIntervalSince(firstClose) >= 0.35 }, "time before reopen")
  host.open(second)
  check(Date().timeIntervalSince(firstClose) < 1, "reopened before first deadline")
  host.closeAll()
  let lastClose = Date()
  check(waitUntil(1) { Date().timeIntervalSince(firstClose) >= 1.1 }, "first deadline elapsed")
  check(Date().timeIntervalSince(lastClose) < 0.95, "observation precedes final deadline")
  check(app.activationPolicy() == .regular, "old timer must not remove Dock tile after second close")
  check(waitUntil(2) { app.activationPolicy() == .accessory }, "last close eventually removes Dock tile")
  check(Date().timeIntervalSince(lastClose) >= 0.95, "last close has its own delay")
  print("STALE_TIMER_PASS")
} catch { fputs("FAIL: \(error)\n", stderr); exit(1) }
