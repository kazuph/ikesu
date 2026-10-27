import AppKit
import ApplicationServices
import Foundation

let app = NSApplication.shared
let host = DialogHost(directory: CommandLine.arguments[2])
app.delegate = host
app.setActivationPolicy(.accessory)
let tracePath = ProcessInfo.processInfo.environment["TASK3_NATIVE_TRACE"]!

func attribute(_ element: AXUIElement, _ name: CFString) -> AnyObject? {
  var value: CFTypeRef?
  guard AXUIElementCopyAttributeValue(element, name, &value) == .success else { return nil }
  return value
}
var frontCounts: [String:Int] = [:]
func record(_ label: String) {
  let front = NSWorkspace.shared.frontmostApplication
  let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
  var input: [String: Any] = ["available": false, "reason": "accessibility permission unavailable"]
  if let front, AXIsProcessTrusted() {
    let target = AXUIElementCreateApplication(front.processIdentifier)
    input = ["available": true, "app": front.localizedName ?? "", "pid": front.processIdentifier]
    if let value = attribute(target, kAXFocusedWindowAttribute as CFString) {
      let window = value as! AXUIElement
      input["window_title"] = attribute(window, kAXTitleAttribute as CFString) as? String ?? ""
    }
    if let value = attribute(target, kAXFocusedUIElementAttribute as CFString) {
      let element = value as! AXUIElement
      input["role"] = attribute(element, kAXRoleAttribute as CFString) as? String ?? ""
    }
  }
  let panels = host.windows.values.map { controller -> [String:Any] in
    let panel = controller.panel!
    return ["key":controller.key,"front_count":frontCounts[controller.key] ?? 0,"window_number":panel.windowNumber,"key_window":panel.isKeyWindow,"visible":panel.isVisible,"title":panel.title]
  }
  let row: [String:Any] = ["label":label,"unix_ms":Int(Date().timeIntervalSince1970*1000),"pid":ProcessInfo.processInfo.processIdentifier,
    "frontmost_pid":front?.processIdentifier ?? 0,"app_active":app.isActive,"window_count":host.windows.count,"panels":panels,
    "input_target":input,"screen_windows":windows.map { w -> [String:Any] in
      ["number":w[kCGWindowNumber as String] ?? 0,"pid":w[kCGWindowOwnerPID as String] ?? 0,"owner":w[kCGWindowOwnerName as String] ?? "","title":w[kCGWindowName as String] ?? "","layer":w[kCGWindowLayer as String] ?? 0]
    }]
  let data = try! JSONSerialization.data(withJSONObject:row,options:[.sortedKeys]) + Data([10])
  if !FileManager.default.fileExists(atPath:tracePath) { FileManager.default.createFile(atPath:tracePath,contents:nil) }
  let file = try! FileHandle(forWritingTo:URL(fileURLWithPath:tracePath));try! file.seekToEnd();try! file.write(contentsOf:data);try! file.close()
}
signal(SIGTERM) { _ in exit(0) }
NSEvent.addLocalMonitorForEvents(matching:.keyDown) { event in
  for controller in host.windows.values where event.window === controller.panel { return controller.handleKey(event) }
  return event
}
DispatchQueue.main.async { record("started") }
DispatchQueue.global().async {
  while let line=readLine() {
    guard let data=line.data(using:.utf8),let message=try? JSONSerialization.jsonObject(with:data) as? [String:Any] else {continue}
    DispatchQueue.main.async {
      if message["op"] as? String == "inspect" { record(message["label"] as? String ?? "inspect") }
      else {
        host.handle(message)
        if message["status"] as? String == "front", let key=message["key"] as? String {
          frontCounts[key,default:0] += 1
          record("command-front")
        }
      }
    }
  }
  DispatchQueue.main.async { host.closeAll();NSApp.terminate(nil) }
}
app.run()
