import AppKit
import Foundation

let app = NSApplication.shared
let host = DialogHost(directory: nil)
app.delegate = host
app.setActivationPolicy(.accessory)
let specPath = CommandLine.arguments[1]
let testCase = CommandLine.arguments[2]
let frontBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier

DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
  host.open(specPath)
  let panel = host.windows.values.first!.panel!
  let frontAfter = NSWorkspace.shared.frontmostApplication?.processIdentifier
  print("show frontBefore=\(frontBefore ?? 0) frontAfter=\(frontAfter ?? 0) key=\(panel.isKeyWindow) level=\(panel.level.rawValue)")
  guard frontBefore == frontAfter && !panel.isKeyWindow && panel.level == .normal else {
    fputs("FAIL: showing a new panel stole focus or changed window level\n", stderr)
    exit(1)
  }
  if testCase == "activate" {
    NSApp.activate(ignoringOtherApps: true)
  } else if testCase == "reopen" {
    _ = host.applicationShouldHandleReopen(app, hasVisibleWindows: true)
  }
  DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
    let front = NSWorkspace.shared.frontmostApplication?.processIdentifier
    let pass = panel.isVisible && panel.isKeyWindow && front == ProcessInfo.processInfo.processIdentifier
    print("\(testCase) front=\(front ?? 0) self=\(ProcessInfo.processInfo.processIdentifier) visible=\(panel.isVisible) key=\(panel.isKeyWindow) pass=\(pass)")
    panel.orderOut(nil)
    exit(pass ? 0 : 1)
  }
}
app.run()
