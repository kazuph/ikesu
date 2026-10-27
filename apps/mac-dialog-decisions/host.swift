import AppKit
import Foundation

final class DialogHost: NSObject, NSApplicationDelegate {
  var windows: [String: DialogController] = [:]
  var cascadePoint = NSPoint.zero
  let directory: String?
  init(directory: String?) { self.directory = directory }

  func open(_ specPath: String) {
    let key = URL(fileURLWithPath: specPath).lastPathComponent
    if windows[key] != nil { return }
    if let directory, URL(fileURLWithPath: specPath).deletingLastPathComponent().standardizedFileURL.path != directory {
      writeOutput(["event": "closed", "key": key, "error": "spec outside owning directory"])
      return
    }
    guard let data = FileManager.default.contents(atPath: specPath),
      let spec = try? JSONDecoder().decode(DialogSpec.self, from: data) else {
      writeOutput(["event": "closed", "key": key, "error": "unreadable dialog specification"])
      return
    }
    let controller = DialogController(spec: spec, specPath: specPath)
    controller.onClose = { [weak self] window in
      self?.windows.removeValue(forKey: window.key)
      self?.refresh()
      if self?.directory == nil { NSApp.terminate(nil) }
    }
    controller.build()
    controller.panel.isReleasedWhenClosed = false
    if windows.isEmpty { cascadePoint = NSPoint(x: controller.panel.frame.minX, y: controller.panel.frame.maxY) }
    cascadePoint = controller.panel.cascadeTopLeft(from: cascadePoint)
    windows[key] = controller
    refresh()
    controller.show()
    controller.startWatchdog()
  }

  func refresh() {
    NSApp.setActivationPolicy(windows.isEmpty ? .accessory : .regular)
    NSApp.dockTile.badgeLabel = windows.isEmpty ? nil : String(windows.count)
    for controller in windows.values { controller.respond(["status": "windows", "queue_remaining": windows.count - 1]) }
    writeOutput(["event": "windows", "count": windows.count, "pid": ProcessInfo.processInfo.processIdentifier])
  }

  func closeAll() {
    for window in Array(windows.values) { window.close() }
  }

  func handle(_ message: [String: Any]) {
    let key = message["key"] as? String ?? ""
    switch message["op"] as? String {
    case "open": if let path = message["path"] as? String { open(path) }
    case "close": windows[key]?.close()
    case "stop": closeAll(); NSApp.terminate(nil)
    default: windows[key]?.respond(message)
    }
  }

  private func bringSelectedPanelForward() {
    let panels = NSApp.orderedWindows.filter { panel in
      windows.values.contains { $0.panel === panel }
    }
    let selected = panels.first(where: { $0.isKeyWindow })
      ?? panels.first(where: { $0.isMainWindow }) ?? panels.first
    for panel in panels.reversed() { panel.orderFront(nil) }
    selected?.makeKeyAndOrderFront(nil)
  }

  func applicationDidBecomeActive(_ notification: Notification) {
    bringSelectedPanelForward()
  }

  func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool {
    sender.activate(ignoringOtherApps: true)
    bringSelectedPanelForward()
    return true
  }

  func applicationDockMenu(_ sender: NSApplication) -> NSMenu? {
    let menu = NSMenu()
    for key in windows.keys.sorted() {
      guard let controller = windows[key] else { continue }
      let spec = controller.spec
      let source = [spec.machine, spec.location, spec.source].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " › ")
      let item = NSMenuItem(title: source + " — " + spec.title, action: #selector(chooseWindow(_:)), keyEquivalent: "")
      item.representedObject = key
      item.target = self
      menu.addItem(item)
    }
    return menu
  }

  @objc func chooseWindow(_ sender: NSMenuItem) {
    guard let key = sender.representedObject as? String, let controller = windows[key] else { return }
    NSApp.activate(ignoringOtherApps: true)
    controller.panel.makeKeyAndOrderFront(nil)
  }
}
