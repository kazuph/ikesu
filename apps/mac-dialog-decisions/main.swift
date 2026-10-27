import AppKit
import Foundation

let args = CommandLine.arguments
if args.count == 3 && args[1] == "--daemon-lock" {
  let fd = open(args[2], O_CREAT | O_RDWR, S_IRUSR | S_IWUSR)
  guard fd >= 0 else { exit(1) }
  let acquired = flock(fd, LOCK_EX | LOCK_NB) == 0
  FileHandle.standardOutput.write("{\"acquired\":\(acquired)}\n".data(using: .utf8)!)
  if acquired { while !FileHandle.standardInput.availableData.isEmpty {} }
  close(fd)
  exit(acquired ? 0 : 1)
}
guard args.count >= 2 else { exit(2) }
let hostMode = args.count >= 3 && args[1] == "--host"
let app = NSApplication.shared
let host = DialogHost(directory: hostMode ? URL(fileURLWithPath: args[2]).standardizedFileURL.path : nil)
app.delegate = host
app.setActivationPolicy(.accessory)
signal(SIGTERM) { _ in exit(0) }
NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
  for controller in host.windows.values where event.window === controller.panel { return controller.handleKey(event) }
  return event
}
// Finish the accessory-app launch before showing an ordinary selectable window.
if !hostMode { DispatchQueue.main.async { host.open(args[1]) } }
DispatchQueue.global().async {
  while let line = readLine() {
    guard let data = line.data(using: .utf8), let message = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { continue }
    DispatchQueue.main.async { host.handle(message) }
  }
  DispatchQueue.main.async { host.closeAll(); NSApp.terminate(nil) }
}
app.run()
