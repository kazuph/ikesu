import AppKit
import Foundation

let root = CommandLine.arguments[1]
let resultPath = root + "/answers.jsonl"
let input: [String: Any] = ["window_title":"ショートカット試験", "kind":"command_guard", "title":"実行確認: rm", "options":[["id":"approve","label":"承認"],["id":"reject","label":"却下"]], "allow_text":false, "multi_select":false, "result_path":resultPath]
let spec = try JSONDecoder().decode(DialogSpec.self, from: JSONSerialization.data(withJSONObject:input))
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let controller = DialogController(spec:spec, specPath:root + "/spec.json")
controller.build()
controller.show()
NSEvent.addLocalMonitorForEvents(matching:.keyDown) { controller.handleKey($0) }
let panel = controller.panel!
let mouse = NSEvent.mouseEvent(with:.leftMouseDown, location:NSPoint(x:2,y:2), modifierFlags:[], timestamp:ProcessInfo.processInfo.systemUptime, windowNumber:panel.windowNumber, context:nil, eventNumber:1, clickCount:1, pressure:1)!
panel.sendEvent(mouse)
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
precondition(panel.isKeyWindow, "click must make the actual panel key")
func key(_ character:String, _ code:UInt16) {
 let event=NSEvent.keyEvent(with:.keyDown,location:.zero,modifierFlags:[.command],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,characters:character,charactersIgnoringModifiers:character,isARepeat:false,keyCode:code)!
 app.sendEvent(event)
 RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
}
func answers() -> [[String: Any]] {
 let lines=(try? String(contentsOfFile:resultPath,encoding:.utf8))?.split(separator:"\n") ?? []
 return lines.compactMap { try? JSONSerialization.jsonObject(with:Data($0.utf8)) as? [String:Any] }.filter { $0["result"] as? String == "answer" }
}
key("1",18)
precondition(controller.checkedIds()==["approve"] && answers().isEmpty, "⌘1 selects only")
print("SHORTCUT_AFTER_CMD1 option=approve sends=0")
key("\r",36)
key("\r",36)
let sent=answers()
precondition(sent.count==1 && sent[0]["option_ids"] as? [String] == ["approve"], "⌘↩ sends the selected ID once")
print("SHORTCUT_AFTER_CMD_RETURN option=approve sends=\(sent.count)")
controller.close()
