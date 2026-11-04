import AppKit
import Foundation

// `screen` is CGWindowListCopyWindowInfo's front-to-back order. `panels` are
// this process's open panels on the active space, front to back. Own windows
// that are not panels (previews) neither count as panels nor cover them.
// A panel ordered moments ago may not be in `screen` yet; it was placed in
// front or behind an uncovered panel, so it counts as uncovered.
func panelPlacement(screen: [[String: Any]], panels: [Int], peers: Set<Int>, own: Int) -> PanelPlacement {
  var others: [CGRect] = []
  var peerBehind: Int?
  var seen = 0, covered = 0
  var listed = Set<Int>(), hiddenPanels = Set<Int>()
  for window in screen {
    guard (window[kCGWindowLayer as String] as? Int) == 0,
      let number = window[kCGWindowNumber as String] as? Int,
      let rect = window[kCGWindowBounds as String] as? NSDictionary,
      let bounds = CGRect(dictionaryRepresentation: rect as CFDictionary) else { continue }
    let pid = window[kCGWindowOwnerPID as String] as? Int ?? 0
    let ownPanel = panels.contains(number)
    if ownPanel || peers.contains(pid) {
      seen += 1
      listed.insert(number)
      let hidden = others.contains { other in
        let overlap = other.intersection(bounds)
        return !overlap.isNull && overlap.width > 0 && overlap.height > 0
      }
      if hidden { covered += 1; hiddenPanels.insert(number) } else if !ownPanel { peerBehind = number }
    } else if pid != own && (window[kCGWindowAlpha as String] as? Double ?? 1) > 0 {
      others.append(bounds)
    }
  }
  let unlisted = panels.filter { !listed.contains($0) }.count
  // Prefer this process's lowest uncovered panel (its own order is current).
  if let behind = panels.last(where: { !hiddenPanels.contains($0) }) ?? peerBehind {
    return PanelPlacement(behind: behind, reason: "panel_in_front", openPanels: seen + unlisted, coveredPanels: covered)
  }
  let reason = seen > 0 ? "all_panels_covered" : panels.isEmpty && peers.isEmpty ? "no_open_panel" : "panel_not_on_screen"
  return PanelPlacement(behind: nil, reason: reason, openPanels: seen, coveredPanels: covered)
}

// Other processes running this same dialog executable (one-dialog helpers).
func peerPanelPIDs(own: Int) -> Set<Int> {
  guard let executable = Bundle.main.executableURL?.resolvingSymlinksInPath() else { return [] }
  return Set(NSWorkspace.shared.runningApplications.compactMap { app in
    Int(app.processIdentifier) != own && app.executableURL?.resolvingSymlinksInPath() == executable ? Int(app.processIdentifier) : nil
  })
}

final class DialogHost: NSObject, NSApplicationDelegate {
  // Rapid activation-policy changes can leave stale Dock tiles; allow the Dock to settle.
  private let dockRemovalDelay: TimeInterval = 1
  private var dockGeneration = 0
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
    let own = Int(ProcessInfo.processInfo.processIdentifier)
    let open = Set(windows.values.filter { $0 !== controller && $0.panel.isVisible && $0.panel.isOnActiveSpace }.map { $0.panel.windowNumber })
    let panels = (NSWindow.windowNumbers(options: []) ?? []).map { $0.intValue }.filter { open.contains($0) }
    // A one-dialog helper shares the screen with sibling helper processes.
    let peers = directory == nil && controller.spec.present_in_background == true ? peerPanelPIDs(own: own) : []
    let screen = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
    controller.show(placement: panelPlacement(screen: screen, panels: panels, peers: peers, own: own))
    controller.startWatchdog()
  }

  func refresh() {
    dockGeneration += 1
    if windows.isEmpty {
      let generation = dockGeneration
      DispatchQueue.main.asyncAfter(deadline: .now() + dockRemovalDelay) { [weak self] in
        guard let self, self.dockGeneration == generation, self.windows.isEmpty else { return }
        NSApp.setActivationPolicy(.accessory)
      }
    } else {
      NSApp.setActivationPolicy(.regular)
    }
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
      let question = spec.title.isEmpty ? (spec.body ?? "") : spec.title
      let item = NSMenuItem(title: source + " — " + question, action: #selector(chooseWindow(_:)), keyEquivalent: "")
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
